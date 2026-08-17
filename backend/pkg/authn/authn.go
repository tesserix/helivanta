package authn

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const SessionCookie = "helivanta_session"

const principalKey = "authn.principal"

// ErrNoAuthTime is returned when a verified token carries no auth_time
// claim. A token with no auth_time cannot be evaluated against a
// revocation watermark, and a credential that cannot be evaluated is not
// one that can be trusted.
var ErrNoAuthTime = errors.New("authn: token has no auth_time claim")

type Principal struct {
	Subject string `json:"subject"`
	// TenantID is HMS's own fact, not the identity provider's (spec
	// docs/superpowers/specs/2026-08-15-zitadel-auth-design.md, decision
	// D1) — Zitadel carries no claim asserting which organization a
	// token was issued for.
	//
	// TRANSITIONAL, until plan Task 4/5 land: a Principal produced by
	// verifying a raw Zitadel ID token (pkg/authn/zitadel.go) can only
	// ever assert who authenticated and when, never a tenant — so
	// TenantID is left as the empty string, not fabricated. This is
	// deliberately NOT loosened anywhere else to treat an empty TenantID
	// as "no tenant scoping needed": TenantPrincipal below still requires
	// TenantID to parse as a UUID and 401s otherwise, so an empty
	// TenantID still fails closed, exactly as a malformed one always has.
	// Once Task 4 mints an HMS session carrying a real tenant_id (D2) and
	// wires ITS verifier into authn.Middleware, every Principal reaching
	// TenantPrincipal will have a real tenant again; until then, no
	// tenant-scoped route can be reached with a bare Zitadel token, by
	// construction.
	TenantID string `json:"tenant_id"`
	// AuthTime is when the user actually authenticated, not when this
	// token was issued. A token refresh mints a new token with a fresh
	// iat but carries the ORIGINAL auth_time, so this is the only claim
	// a revocation watermark can be compared against: comparing iat
	// would let any client holding a live refresh token walk through the
	// watermark simply by refreshing.
	AuthTime time.Time `json:"-"`
	// IdleDeadline is when this session stops being usable without
	// further human interaction (spec D2, #848), carried through
	// unchanged from session.Claims.IdleDeadline. A zero value here is
	// refused by Middleware exactly like an already-past deadline, never
	// read as "no limit" — see its comment.
	IdleDeadline time.Time `json:"-"`
}

type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (Principal, error)
}

// Middleware authenticates via Bearer header or the session cookie, then
// refuses any credential whose auth_time predates rev's watermark for
// that subject. Failures are 401 (bad/missing/revoked credential) or 503
// (revocation state could not be read) with a JSON envelope; no handler
// runs unauthenticated or with an unevaluated revocation state.
func Middleware(v TokenVerifier, rev RevocationChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			raw = strings.TrimPrefix(h, "Bearer ")
		}
		if raw == "" {
			raw, _ = c.Cookie(SessionCookie)
		}
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "missing credentials"})
			return
		}
		p, err := v.Verify(c.Request.Context(), raw)
		if err != nil {
			slog.Warn("auth verification failed", "err", err, "path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid credentials"})
			return
		}
		watermark, err := rev.RevokedAfter(c.Request.Context(), p.Subject)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "revocation lookup failed",
				"err", err, "subject", p.Subject)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "authz_unavailable", "message": "authorization is temporarily unavailable"})
			return
		}
		// Not-after, deliberately: a token whose auth_time equals the
		// watermark to the second is refused. Second granularity means a
		// sign-in racing a revocation is ambiguous, and the safe reading
		// of an ambiguous credential is that it is revoked.
		if !watermark.IsZero() && !p.AuthTime.After(watermark) {
			slog.InfoContext(c.Request.Context(), "refused a revoked credential",
				"subject", p.Subject, "auth_time", p.AuthTime, "revoked_at", watermark,
				"path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthenticated", "message": "credential revoked"})
			return
		}
		// Idle timeout (#848, spec D2), enforced server-side because the
		// deadline travels inside the signed token rather than a
		// browser-owned timer — a killed tab, a suspended laptop or a
		// stolen cookie all fail closed here. No IsZero exemption: a zero
		// deadline is treated as already past, not as "no limit" (see
		// Principal.IdleDeadline). Not-before, mirroring the watermark
		// check's not-after: exactly-now is refused. Distinct error code
		// (session_idle) so the frontend can tell "went idle" from "bad
		// credentials" apart (spec D6).
		if !time.Now().Before(p.IdleDeadline) {
			slog.InfoContext(c.Request.Context(), "refused an idle session",
				"subject", p.Subject, "idle_deadline", p.IdleDeadline, "path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "session_idle", "message": "your session ended after a period of inactivity"})
			return
		}
		c.Set(principalKey, p)
		c.Next()
	}
}

func PrincipalFrom(c *gin.Context) (Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}

// TenantPrincipal extracts the authenticated principal and its tenant
// UUID. On a missing principal or malformed tenant claim it writes the
// 401 envelope, aborts, and returns ok=false — callers just return.
func TenantPrincipal(c *gin.Context) (Principal, uuid.UUID, bool) {
	p, ok := PrincipalFrom(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "missing principal"})
		return Principal{}, uuid.Nil, false
	}
	tenantID, err := uuid.Parse(p.TenantID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid tenant"})
		return Principal{}, uuid.Nil, false
	}
	return p, tenantID, true
}
