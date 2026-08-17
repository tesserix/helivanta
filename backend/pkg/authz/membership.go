package authz

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

// MembershipChecker answers whether a subject belongs to a tenant.
// *Client implements it (pkg/authz/tenant.go); tests substitute fakes.
type MembershipChecker interface {
	IsMember(ctx context.Context, subject, tenantID string) (bool, error)
}

// RequireMembership refuses a caller who holds no role in the tenant
// their token names.
//
// It runs per route rather than once per request group, because whether
// membership is required is a property of the route's declared marker —
// and because resolving it lazily means a NoTenantMembership route never
// makes the call at all. That matters: sign-out is a NoTenantMembership
// route, and it must keep working during an OpenFGA outage, which it
// would not if membership were resolved for every request up front.
//
// 403, not 404: the caller is authenticated and the route exists. 404 is
// this codebase's answer for a cross-tenant *resource*, which is a
// different question from a cross-tenant *caller*.
//
// Fails closed without exception: a membership check that cannot be
// answered denies with 503, never admits. A false here and a false from
// a genuine non-member are indistinguishable to this middleware — see
// MembershipChecker's implementation for why that is by design — so an
// error must never be read as "not a member".
func RequireMembership(m MembershipChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		member, err := m.IsMember(c.Request.Context(), p.Subject, p.TenantID)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "membership check failed",
				"err", err, "subject", p.Subject, "tenant_id", p.TenantID)
			respond.Error(c, http.StatusServiceUnavailable,
				"authz_unavailable", "authorization is temporarily unavailable")
			return
		}
		if !member {
			respond.Forbidden(c, "not a member of this tenant")
			return
		}
		c.Next()
	}
}
