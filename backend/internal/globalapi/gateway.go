// Package globalapi routes client traffic to explicitly configured product owners.
package globalapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/internal/platform/requestid"
)

type Config struct {
	Upstreams         map[string]string
	Development       bool
	Timeout           time.Duration
	MaxBodyBytes      int64
	TrustedProxyCIDRs []string
	TransportSecurity string
}

var owners = map[string]string{
	"privacy": "platform", "auth": "platform", "iam": "platform", "workspace": "platform", "patients": "platform", "reference": "platform", "drugmaster": "platform",
	"mediconnect": "mediconnect", "medicore": "medicore", "workforce": "workforce", "pharmacy": "pharmacy", "payments": "platform", "supply": "supply", "lab": "lab", "emergency": "emergency",
}

// New keeps the owner's authentication, authorization and wire response intact.
// It never derives authority from forwarded identity or tenant headers.
func New(cfg Config) (http.Handler, error) {
	if cfg.TransportSecurity != "" && cfg.TransportSecurity != "tls" && cfg.TransportSecurity != "istio" {
		return nil, errors.New("invalid Global API transport security mode")
	}
	if len(cfg.Upstreams) == 0 {
		return nil, errors.New("global API requires at least one upstream")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("global API timeout must be positive")
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = 16 << 20
	}
	if cfg.MaxBodyBytes < 0 {
		return nil, errors.New("global API body limit must be positive")
	}
	for _, cidr := range cfg.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, errors.New("invalid global API trusted proxy CIDR")
		}
	}
	proxies := make(map[string]*httputil.ReverseProxy)
	slots := make(map[string]chan struct{})
	totalSlots := make(chan struct{}, 16)
	var readyChecks []httpserver.ReadyCheck
	for product, raw := range cfg.Upstreams {
		known := false
		for _, owner := range owners {
			if owner == product {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown global API product %q", product)
		}
		target, err := url.Parse(raw)
		if err != nil || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || (target.Path != "" && target.Path != "/") {
			return nil, fmt.Errorf("invalid origin for %s", product)
		}
		localHTTP := cfg.Development && target.Scheme == "http" && (target.Hostname() == "localhost" || net.ParseIP(target.Hostname()).IsLoopback())
		meshHTTP := cfg.TransportSecurity == "istio" && target.Scheme == "http" && strings.HasSuffix(target.Hostname(), ".svc.cluster.local") && len(strings.Split(target.Hostname(), ".")) == 5
		if target.Scheme != "https" && !localHTTP && !meshHTTP {
			return nil, fmt.Errorf("HTTPS required for %s (development HTTP is loopback only)", product)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.MaxConnsPerHost = 32
		transport.MaxIdleConnsPerHost = 8
		transport.ResponseHeaderTimeout = cfg.Timeout
		if product == "platform" {
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			readyChecks = append(readyChecks, httpserver.ReadyCheck{Name: "platform", Check: func(ctx context.Context) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Scheme+"://"+target.Host+"/readyz", nil)
				if err != nil {
					return errors.New("platform readiness request unavailable")
				}
				res, err := client.Do(req)
				if err != nil {
					return errors.New("platform readiness unavailable")
				}
				if err := res.Body.Close(); err != nil {
					return errors.New("platform readiness response unavailable")
				}
				if res.StatusCode != http.StatusOK {
					return errors.New("platform not ready")
				}
				return nil
			}})
		}
		proxies[product] = &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(p *httputil.ProxyRequest) {
				p.SetURL(target)
				// Allowlisted forwarding prevents client-supplied identity and proxy headers
				// becoming authority at a subsequently extracted product API.
				headers := make(http.Header)
				for _, key := range []string{"Authorization", "Cookie", "Content-Type", "Accept", "Accept-Language", "If-Match", "If-None-Match", "Idempotency-Key", "X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject", "Origin", "Sec-Fetch-Site", "Range", "If-Range"} {
					for _, value := range p.Out.Header.Values(key) {
						headers.Add(key, value)
					}
				}
				headers.Set("X-Request-ID", p.In.Header.Get("X-Request-ID"))
				headers.Set("X-Forwarded-For", p.In.RemoteAddr)
				p.Out.Header = headers
				p.Out.GetBody = nil
			},
			ModifyResponse: func(r *http.Response) error {
				// Middleware owns the response ID; ReverseProxy appends copied headers.
				r.Header.Del("X-Request-ID")
				r.Header.Set("Cache-Control", "no-store")
				if r.StatusCode >= http.StatusInternalServerError {
					slog.WarnContext(r.Request.Context(), "global API upstream response failed", "product", product, "request_id", r.Request.Header.Get("X-Request-ID"), "status", r.StatusCode)
				}
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				status, code := http.StatusBadGateway, "upstream_unavailable"
				if errors.Is(err, context.DeadlineExceeded) {
					status, code = http.StatusGatewayTimeout, "upstream_timeout"
				}
				slog.WarnContext(r.Context(), "global API upstream failed", "product", product, "request_id", r.Header.Get("X-Request-ID"), "code", code)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, `{"error":%q,"message":"Product API unavailable; a submitted command may require reconciliation."}`, code)
			},
		}
		slots[product] = make(chan struct{}, 32)
	}
	server := httpserver.New(readyChecks, cfg.TrustedProxyCIDRs, requestid.Middleware(), func(c *gin.Context) { c.Header("Via", "1.1 helivanta-global-api"); c.Next() })
	server.Engine.NoRoute(func(c *gin.Context) {
		r := c.Request
		if r.URL.Path != path.Clean(r.URL.Path) || strings.ContainsAny(r.URL.Path, "\\\x00") || (r.URL.RawPath != "" && r.URL.RawPath != r.URL.Path) {
			c.AbortWithStatusJSON(400, gin.H{"error": "invalid_request", "message": "Noncanonical API path"})
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		product := ""
		if len(parts) >= 2 && parts[0] == "v1" {
			product = owners[parts[1]]
		}
		if r.Method == http.MethodPost && r.URL.Path == "/internal/v1/hospital-enrolments" {
			product = "platform"
		}
		if product == "" {
			c.AbortWithStatusJSON(404, gin.H{"error": "not_found", "message": "Unknown API"})
			return
		}
		proxy := proxies[product]
		if proxy == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "service_unavailable", "message": "Product API is not configured"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			site := r.Header.Get("Sec-Fetch-Site")
			if site == "cross-site" || site == "same-site" {
				c.AbortWithStatusJSON(403, gin.H{"error": "forbidden", "message": "Cross-origin mutation refused"})
				return
			}
		}
		select {
		case slots[product] <- struct{}{}:
			defer func() { <-slots[product] }()
		default:
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(503, gin.H{"error": "service_busy", "message": "Product API capacity exhausted"})
			return
		}
		select {
		case totalSlots <- struct{}{}:
			defer func() { <-totalSlots }()
		default:
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(503, gin.H{"error": "service_busy", "message": "Global API capacity exhausted"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), cfg.Timeout)
		defer cancel()
		r = r.WithContext(ctx)
		if r.Body != nil {
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, r.Body, cfg.MaxBodyBytes))
			if err != nil {
				var tooLarge *http.MaxBytesError
				status := 400
				if errors.As(err, &tooLarge) {
					status = 413
				}
				c.AbortWithStatusJSON(status, gin.H{"error": "invalid_request", "message": "Request body rejected"})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		r.Header.Set("X-Request-ID", c.GetString(requestid.Key))
		r.RemoteAddr = c.ClientIP()
		proxy.ServeHTTP(c.Writer, r)
	})
	return server.Engine, nil
}
