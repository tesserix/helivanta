package authn

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExpectedTenantPrincipal additionally checks an optional workspace precondition;
// the header never selects the tenant or grants authority.
func ExpectedTenantPrincipal(c *gin.Context) (Principal, uuid.UUID, bool) {
	p, tenant, ok := TenantPrincipal(c)
	if !ok {
		return Principal{}, uuid.Nil, false
	}
	expected := c.Request.Header.Values("X-Helivanta-Expected-Tenant")
	if len(expected) == 0 {
		return p, tenant, true
	}
	if len(expected) == 1 {
		requested, err := uuid.Parse(expected[0])
		if err == nil && requested != uuid.Nil && requested == tenant {
			return p, tenant, true
		}
	}
	// A stale workspace assertion must fail before any owner reads or mutations.
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "session_changed", "message": "Your workspace changed. Refresh the page before continuing."})
	return Principal{}, uuid.Nil, false
}
