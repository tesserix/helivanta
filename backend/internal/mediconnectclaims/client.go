// Package mediconnectclaims observes the canonical owner predicate; it does not grant clinical access.
package mediconnectclaims

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
)

// ErrUnavailable covers missing configuration, malformed observations and dependency failures.
var ErrUnavailable = errors.New("current claim owner unavailable")
var projectedToken = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Client holds an explicit owner origin and credential file, never a cached predicate or token.
type Client struct {
	origin, tokenFile string
	http              *http.Client
}

// New requires authenticated TLS, loopback, or an explicitly approved cluster mTLS origin.
func New(origin, tokenFile, transportMode string, provided *http.Client) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || !filepath.IsAbs(tokenFile) {
		return nil, ErrUnavailable
	}
	valid := false
	switch u.Scheme {
	case "https":
		valid = transportMode == "" || transportMode == "tls"
	case "http":
		host := u.Hostname()
		loopback := host == "127.0.0.1" || host == "::1" || host == "localhost"
		cluster := strings.HasSuffix(host, ".svc.cluster.local") && !strings.ContainsAny(host, " /\\\t\r\n")
		valid = (loopback && (transportMode == "" || transportMode == "loopback")) || (cluster && transportMode == "istio-mtls")
	}
	if !valid {
		return nil, ErrUnavailable
	}
	client := http.Client{}
	if provided != nil {
		client = *provided
	}
	var transport *http.Transport
	if client.Transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		original, ok := client.Transport.(*http.Transport)
		if !ok {
			return nil, ErrUnavailable
		}
		transport = original.Clone()
	}
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return nil, ErrUnavailable
	}
	transport.Proxy = nil
	client.Transport = transport
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{origin: strings.TrimRight(u.String(), "/"), tokenFile: tokenFile, http: &client}, nil
}

// Current makes one bounded fresh observation, preserving false separately from unavailability.
func (c *Client) Current(ctx context.Context, in mediconnectcontract.ClaimCheckRequest) (bool, error) {
	if c == nil || ctx.Err() != nil {
		return false, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > 4096 {
		return false, ErrUnavailable
	}
	info, err := os.Stat(c.tokenFile)
	if err != nil || !info.Mode().IsRegular() {
		return false, ErrUnavailable
	}
	file, err := os.Open(c.tokenFile)
	if err != nil {
		return false, ErrUnavailable
	}
	token, readErr := io.ReadAll(io.LimitReader(file, 8193))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(token) > 8192 {
		return false, ErrUnavailable
	}
	credential := strings.TrimSpace(string(token))
	if !projectedToken.MatchString(credential) || len(credential)+len("Bearer ") > 8192 || ctx.Err() != nil {
		return false, ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+mediconnectcontract.CurrentClaimPath, bytes.NewReader(raw))
	if err != nil {
		return false, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set(mediconnectcontract.ClaimWorkloadHeader, "Bearer "+credential)
	response, err := c.http.Do(request)
	if err != nil {
		return false, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return false, ErrUnavailable
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return false, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if err != nil || len(body) > 1024 || !utf8.Valid(body) || ctx.Err() != nil {
		return false, ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return false, ErrUnavailable
	}
	var out mediconnectcontract.ClaimCheckResponse
	count := 0
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil || key != "current" || count != 0 {
			return false, ErrUnavailable
		}
		var value any
		if decoder.Decode(&value) != nil {
			return false, ErrUnavailable
		}
		current, ok := value.(bool)
		if !ok {
			return false, ErrUnavailable
		}
		out.Current = current
		count++
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || count != 1 || decoder.Decode(new(any)) != io.EOF {
		return false, ErrUnavailable
	}
	return out.Current, nil
}
