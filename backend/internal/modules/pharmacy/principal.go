package pharmacy

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func pharmacyPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, tenant, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	if expected := c.Request.Header.Values("X-Helivanta-Expected-Tenant"); len(expected) > 0 {
		if len(expected) == 1 {
			requested, err := uuid.Parse(expected[0])
			if err == nil && requested != uuid.Nil && requested == tenant {
				return p, tenant, true
			}
		}
		respond.Error(c, http.StatusConflict, "session_changed", "Your workspace changed. Refresh the page before continuing.")
		return p, uuid.Nil, false
	}
	return p, tenant, true
}

func requirePharmacyTenant(c *gin.Context) {
	if _, _, ok := pharmacyPrincipal(c); !ok {
		c.Abort()
	}
}
