package authn

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const SessionCookie = "hms_session"

const principalKey = "authn.principal"

type Principal struct {
	Subject  string `json:"subject"`
	TenantID string `json:"tenant_id"`
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

// Middleware authenticates via Bearer header or the session cookie.
// Failures are 401 with a JSON envelope; no handler runs unauthenticated.
func Middleware(v TokenVerifier) gin.HandlerFunc {
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
