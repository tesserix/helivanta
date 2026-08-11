package iam

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
)

type tenantMembership struct {
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}

type switchRequest struct {
	TenantID string `json:"tenant_id" binding:"required,uuid"`
}

// registerMe adds the self-service routes. They are authz.Public because
// any authenticated caller may ask what they can do and where they
// belong; the switch endpoint gates on membership itself.
//
// /me/tenants and /me/tenant resolve membership from OpenFGA via
// deps.Roles.ListRoles rather than querying iam_members directly.
// iam_members is RLS-forced and every runtime accessor (WithTenant)
// scopes to a single tenant GUC — there is no accessor that can answer
// "which tenants does this subject belong to" without already knowing
// the tenant, which is exactly the question these routes exist to
// answer. tenantdb.WithAdmin bypasses RLS and would work mechanically,
// but it is boot/ops-only (see TestWithAdminIsOnlyCalledFromTheAllowlist
// in internal/archtest) and reads every tenant's rows, not just role
// keys. OpenFGA already holds membership as tuples written by the
// iam-fga-sync consumer, so resolving from there needs no migration, no
// new RLS policy, and no privileged accessor on a request path.
func (m *Module) registerMe(g *platform.Router, deps platform.Deps) {
	g.GET("/me/permissions", authz.Public, func(c *gin.Context) {
		set, ok := authz.PermissionsFrom(c)
		if !ok {
			respond.Internal(c, "authorization not initialized")
			return
		}
		respond.OK(c, gin.H{"data": set.Sorted()})
	})

	g.GET("/me/tenants", authz.Public, func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		bindings, err := deps.Roles.ListRoles(c.Request.Context(), p.Subject)
		if err != nil {
			respondRolesUnavailable(c, err)
			return
		}
		respond.OK(c, gin.H{"data": groupByTenant(bindings)})
	})

	g.POST("/me/tenant", authz.Public, func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		var req switchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		bindings, err := deps.Roles.ListRoles(c.Request.Context(), p.Subject)
		if err != nil {
			respondRolesUnavailable(c, err)
			return
		}
		if !hasBindingForTenant(bindings, req.TenantID) {
			respond.Forbidden(c, "not a member of that tenant")
			return
		}
		// The session is re-minted by the shell, which exchanges this
		// confirmation for a token carrying the new tenant_id claim.
		respond.OK(c, gin.H{"tenant_id": req.TenantID})
	})
}

// groupByTenant turns FGA role bindings into the response shape, one
// entry per tenant listing every role key held there. bindings is
// already sorted by tenant then role (authz.Client.ListRoles's
// contract), so the grouping preserves that order and the result is
// deterministic. Returns a non-nil empty slice when bindings is empty
// so the JSON array is never null.
func groupByTenant(bindings []authz.RoleBinding) []tenantMembership {
	out := make([]tenantMembership, 0, len(bindings))
	for _, b := range bindings {
		if n := len(out); n > 0 && out[n-1].TenantID == b.TenantID {
			out[n-1].Roles = append(out[n-1].Roles, string(b.Role))
			continue
		}
		out = append(out, tenantMembership{TenantID: b.TenantID, Roles: []string{string(b.Role)}})
	}
	for i := range out {
		sort.Strings(out[i].Roles)
	}
	return out
}

func hasBindingForTenant(bindings []authz.RoleBinding, tenantID string) bool {
	for _, b := range bindings {
		if b.TenantID == tenantID {
			return true
		}
	}
	return false
}

// respondRolesUnavailable fails closed on a ListRoles error: an error
// must never be treated as "no memberships", which would silently
// deny a real member and silently admit a switch that should have been
// checked. It reuses authz.Middleware's 503 authz_unavailable shape so
// every authorization-infrastructure failure looks the same to callers,
// whether it happened in the resolve-permissions middleware or here in
// a membership check.
func respondRolesUnavailable(c *gin.Context, err error) {
	requestid.Logger(c).ErrorContext(c.Request.Context(), "list roles failed", "err", err)
	respond.Error(c, http.StatusServiceUnavailable,
		"authz_unavailable", "authorization is temporarily unavailable")
}
