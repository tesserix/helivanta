package ratelimit

import (
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

// Config is the whole limiting policy applied by Middleware.
//
// Tight and Exempt are keyed by "METHOD /path" using gin's REGISTERED
// route pattern (c.FullPath()), not the request URI: the pattern is
// stable and finite, where a URI carries path parameters and would make
// every /iam/subjects/<uid>/revoke a distinct key.
type Config struct {
	Tenant    Rule
	Principal Rule
	// Tight overrides Principal for specific routes that consume a shared
	// external resource (e.g. GIP token minting, whose quota is
	// project-wide rather than per-tenant).
	Tight map[string]Rule
	// Exempt maps a route to the reason it must never be throttled. The
	// reason is stored, not just the key, so the arch-test allowlist (Task
	// 4) can require one and a reader can see why without archaeology.
	Exempt map[string]string
}

// denials counts every request this process has refused for exceeding a
// rate budget, mirroring logging.RedactionCount()'s pattern: an
// in-process counter with an accessor rather than a metric, because no
// metrics system exists yet — #679 is unbuilt, and wiring a fake sink here
// would be worse than leaving an honest seam for the real one.
var denials atomic.Uint64

// DenialCount reports how many requests this process has rate limited.
func DenialCount() uint64 { return denials.Load() }

// Middleware refuses requests over budget.
//
// Placement is load-bearing: it must run AFTER authn (the tenant comes
// from the verified token) and BEFORE authz (which calls OpenFGA on every
// request — the most expensive step in the chain and itself a shared
// resource). Limiting after authz would let a flood exhaust OpenFGA before
// anything was refused. Task 4's arch test proves this ordering against
// the real chain; the tests in this package cannot, because they do not
// wire authz at all.
//
// Two buckets, both must allow: the tenant bucket protects other
// hospitals from a noisy one, the principal bucket protects a hospital
// from one of its own users or from a compromised credential looping.
func Middleware(l Limiter, cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			// No principal means authn did not run or did not admit the
			// caller. That is not this middleware's decision to make.
			respond.Unauthenticated(c, "missing principal")
			return
		}

		route := c.Request.Method + " " + c.FullPath()
		now := time.Now()

		if reason, exempt := cfg.Exempt[route]; exempt {
			// Exempt from the LIMIT, not from the record: a route being
			// hammered while exempt must be visible rather than
			// invisible, or the exemption becomes a blind spot.
			slog.DebugContext(c.Request.Context(), "rate limit exempt route",
				"route", route, "reason", reason, "tenant_id", p.TenantID, "subject", p.Subject)
			c.Next()
			return
		}

		if d := l.Allow("tenant:"+p.TenantID, cfg.Tenant, now); !d.Allowed {
			deny(c, "tenant", p, route, d)
			return
		}

		// A tight rule gets its OWN bucket, keyed by route as well as
		// subject. Sharing the plain "subject:<subject>" bucket between
		// the tight and default rules would let hitting the tight route
		// drain the budget every other route reads from, and vice versa —
		// two independently-configured budgets bleeding into one counter
		// is not "two buckets", it just looks like one until tested.
		rule := cfg.Principal
		key := "subject:" + p.Subject
		if tight, ok := cfg.Tight[route]; ok {
			rule = tight
			key = "subject:" + p.Subject + ":" + route
		}
		d := l.Allow(key, rule, now)
		if !d.Allowed {
			deny(c, "principal", p, route, d)
			return
		}

		c.Header("RateLimit-Limit", strconv.Itoa(d.Limit))
		c.Header("RateLimit-Remaining", strconv.Itoa(d.Remaining))
		c.Next()
	}
}

// deny refuses and names which bucket ran out. "You are rate limited"
// without saying which bucket is a support ticket rather than an answer:
// the caller cannot tell whether they are the problem or their whole
// hospital is.
func deny(c *gin.Context, bucket string, p authn.Principal, route string, d Decision) {
	slog.WarnContext(c.Request.Context(), "rate limited",
		"bucket", bucket, "route", route,
		"tenant_id", p.TenantID, "subject", p.Subject,
		"retry_after_ms", d.RetryAfter.Milliseconds())
	denials.Add(1)
	respond.TooManyRequests(c,
		"too many requests for this "+bucket+"; retry in "+d.RetryAfter.Round(time.Second).String(),
		d.RetryAfter, d.Limit, d.Remaining)
}
