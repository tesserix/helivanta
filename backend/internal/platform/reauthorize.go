package platform

import (
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http"
)

// Reauthorize refreshes tenant membership and permission after external I/O.
func (r *Router) Reauthorize(c *gin.Context, permission authz.Permission) bool {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return false
	}
	member, err := r.membership.IsMember(authz.WithCurrentAuthorization(c.Request.Context()), p.Subject, p.TenantID)
	if err != nil {
		// Membership uncertainty denies the sensitive operation.
		respond.Error(c, http.StatusServiceUnavailable, "authz_unavailable", "authorization is temporarily unavailable")
		return false
	}
	if !member {
		respond.Forbidden(c, "not a member of this tenant")
		return false
	}
	return authz.Refresh(c, permission)
}
