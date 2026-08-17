package httpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReadyCheck reports one dependency's readiness (name → error).
type ReadyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Server struct {
	Engine *gin.Engine
}

// New builds the gin engine every route in this repository is ultimately
// registered on — through platform.Router for authenticated routes, or
// directly via bootstrap.MountUnauthenticated for the deliberate
// bypasses. trustedProxyCIDRs is config.Config.TrustedProxyCIDRs (#867
// Task 4 fix round 3, Finding C1): the CIDR blocks whose IMMEDIATE TCP
// peer is trusted to have set X-Forwarded-For/X-Real-IP honestly.
//
// gin.New()'s OWN default trusts every proxy (0.0.0.0/0) — every rate
// limiter in this codebase that keys on gin.Context.ClientIP() before a
// verified subject exists (LoginRateLimitRule, FactorRateLimitRule, and
// their sibling routes in iam.LoginUIHandlers) is bypassable under that
// default: a caller sends a fresh X-Forwarded-For value per request and
// draws a fresh token-bucket burst every time. This function OVERRIDES
// that default unconditionally, in BOTH directions:
//
//   - trustedProxyCIDRs non-empty → SetTrustedProxies(trustedProxyCIDRs),
//     so gin.Context.ClientIP() resolves the request's actual origin by
//     walking X-Forwarded-For from the right, skipping entries whose
//     address falls in a trusted CIDR (e.g. the Istio ingress gateway's
//     pod IP) until it reaches the first untrusted one — the real
//     caller, per gin's own SetTrustedProxies semantics.
//   - trustedProxyCIDRs empty (unset, or every entry failed to parse —
//     see config.getenvCIDRList) → SetTrustedProxies(nil), which
//     DISABLES the X-Forwarded-For/X-Real-IP mechanism entirely rather
//     than falling through to gin's trust-everyone default.
//     ClientIP() then always returns the raw TCP RemoteAddr, which no
//     request header can influence. This is TrustedProxyCIDRs' own
//     documented fail-closed direction (config.go): an unconfigured
//     trust boundary must mean coarser rate-limit buckets, never "trust
//     whatever header shows up".
//
// SetTrustedProxies itself can only error on a malformed CIDR string,
// and config.getenvCIDRList already validates every entry before this
// function ever sees it — but this still checks the error rather than
// discarding it, and forces SetTrustedProxies(nil) (fail closed, not
// gin's default) if it ever fires, so a future caller that builds
// trustedProxyCIDRs some other way cannot silently regress to
// trust-everyone by way of an ignored error.
func New(readyChecks []ReadyCheck, trustedProxyCIDRs []string, middlewares ...gin.HandlerFunc) *Server {
	e := gin.New()
	if len(trustedProxyCIDRs) > 0 {
		if err := e.SetTrustedProxies(trustedProxyCIDRs); err != nil {
			slog.Error("invalid trusted proxy CIDRs; failing closed to trusting no proxy",
				"cidrs", trustedProxyCIDRs, "err", err)
			_ = e.SetTrustedProxies(nil)
		}
	} else {
		_ = e.SetTrustedProxies(nil)
	}
	e.Use(gin.Recovery())
	for _, mw := range middlewares {
		e.Use(mw)
	}
	e.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	e.GET("/readyz", func(c *gin.Context) {
		for _, rc := range readyChecks {
			if err := rc.Check(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready", "failed": rc.Name})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	return &Server{Engine: e}
}
