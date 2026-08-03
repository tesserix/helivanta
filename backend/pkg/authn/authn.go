package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
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
