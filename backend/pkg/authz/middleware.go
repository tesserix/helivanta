package authz

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
)

const permissionsKey = "authz.permissions"

// Resolver returns a subject's permissions within one tenant. *Client
// implements it; tests substitute fakes.
type Resolver interface {
	Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error)
}

// Middleware resolves the caller's whole permission set for the token's
// tenant in one call and parks it on the context, so Require is a pure
// in-memory lookup no matter how many permissions a route declares.
//
// It fails closed without exception: any resolver error denies with 503,
// including on Public routes. There is no code path on which a handler
// runs without a resolved set.
func Middleware(r Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		set, err := r.Resolve(c.Request.Context(), p.Subject, p.TenantID)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "authz resolve failed",
				"err", err, "subject", p.Subject, "tenant_id", p.TenantID)
			respond.Error(c, http.StatusServiceUnavailable,
				"authz_unavailable", "authorization is temporarily unavailable")
			return
		}
		c.Set(permissionsKey, set)
		c.Next()
	}
}

// Require denies with 403 unless the resolved set carries p. A caller
// who is not a member of the tenant resolves to an empty set, and is
// additionally refused by RequireMembership before reaching here.
//
// The two markers declare no permission, so there is nothing to check:
// Public still passes through RequireMembership, NoTenantMembership does
// not. Skipping explicitly here — rather than by having Has lie about
// what the set contains (#781) — keeps PermissionSet an honest set.
func Require(p Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == Public || p == NoTenantMembership {
			c.Next()
			return
		}
		set, ok := PermissionsFrom(c)
		if !ok {
			// Router construction guarantees Middleware runs first; this
			// is a programming error, not a client error.
			respond.InternalErr(c, errors.New("permission set missing from context"), "authorization not initialized")
			return
		}
		if !set.Has(p) {
			respond.Forbidden(c, "missing permission "+string(p))
			return
		}
		c.Next()
	}
}

func PermissionsFrom(c *gin.Context) (PermissionSet, bool) {
	v, ok := c.Get(permissionsKey)
	if !ok {
		return nil, false
	}
	set, ok := v.(PermissionSet)
	return set, ok
}
