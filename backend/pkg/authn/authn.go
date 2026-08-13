package authn

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const SessionCookie = "hms_session"

const principalKey = "authn.principal"

type Principal struct {
	Subject  string `json:"subject"`
	TenantID string `json:"tenant_id"`
	// AuthTime is when the user actually authenticated, not when this
	// token was issued. A token refresh mints a new token with a fresh
	// iat but carries the ORIGINAL auth_time, so this is the only claim
	// a revocation watermark can be compared against: comparing iat
	// would let any client holding a live refresh token walk through the
	// watermark simply by refreshing.
	AuthTime time.Time `json:"-"`
}

type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (Principal, error)
}

// TokenMinter issues a custom token the client exchanges for a fresh ID
// token. It exists so a caller who has already been authorized to act in
// a different tenant can be handed a credential carrying that tenant_id
// claim — without which a "tenant switch" changes nothing, because
// Principal.TenantID comes from the token and nowhere else.
//
// Deliberately one method wide: it is the whole of the identity
// provider that any module may reach. Nothing here can verify, look up,
// or mutate a user — only mint a token for a subject the caller has
// already gated on. The method signature matches the Firebase Admin
// SDK's auth.Client so the GIP implementation is a thin forward.
type TokenMinter interface {
	CustomTokenWithClaims(ctx context.Context, uid string, claims map[string]interface{}) (string, error)
}

// TokenRevoker revokes a subject's refresh tokens at the identity
// provider, so GIP agrees with the HMS watermark instead of quietly
// disagreeing. One method wide, for the same reason TokenMinter is: it
// is the whole of the identity provider a module may reach for this
// purpose, and nothing else.
type TokenRevoker interface {
	RevokeRefreshTokens(ctx context.Context, uid string) error
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
