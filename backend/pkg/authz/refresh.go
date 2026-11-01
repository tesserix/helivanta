package authz

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"net/http"
)

// Refresh rechecks a concrete permission using the middleware's original resolver.
func Refresh(c *gin.Context, permission Permission) bool {
	set, ok := RefreshPermissions(c)
	if !ok {
		return false
	}
	if !set.Has(permission) {
		respond.Forbidden(c, "missing permission "+string(permission))
		return false
	}
	return true
}

// RefreshPermissions resolves current permissions without conferring a route permission.
func RefreshPermissions(c *gin.Context) (PermissionSet, bool) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return nil, false
	}
	value, exists := c.Get(resolverKey)
	resolver, valid := value.(Resolver)
	if !exists || !valid || resolver == nil {
		respond.InternalErr(c, errors.New("authorization resolver missing"), "authorization not initialized")
		return nil, false
	}
	set, err := resolver.Resolve(WithCurrentAuthorization(c.Request.Context()), p.Subject, p.TenantID)
	if err != nil {
		// Uncertain current authority must never release private bytes or commit a receipt.
		respond.Error(c, http.StatusServiceUnavailable, "authz_unavailable", "authorization is temporarily unavailable")
		return nil, false
	}
	c.Set(permissionsKey, set)
	return set, true
}
