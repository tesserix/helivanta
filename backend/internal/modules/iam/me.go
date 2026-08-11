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
		// req.TenantID is compared raw, with no casing normalization:
		// Principal.TenantID is canonicalized (lowercase uuid.String())
		// at the GIP token parse boundary (pkg/authn/gip.go), so every
		// FGA role tuple written by the iam-fga-sync consumer — and
		// therefore every binding ListRoles returns — is already
		// canonical. A client round-tripping the value it received from
		// /me/tenants (itself sourced from these same bindings) submits
		// the same canonical form back here.
		target := req.TenantID
		bindings, err := deps.Roles.ListRoles(c.Request.Context(), p.Subject)
		if err != nil {
			respondRolesUnavailable(c, err)
			return
		}
		if !hasBindingForTenant(bindings, target) {
			respond.Forbidden(c, "not a member of that tenant")
			return
		}
		// The session is re-minted by the shell, which exchanges this
		// confirmation for a token carrying the new tenant_id claim.
		respond.OK(c, gin.H{"tenant_id": target})
	})
}

// groupByTenant turns FGA role bindings into the response shape, one
// entry per tenant listing every role key held there. Tenant ids are
// compared raw, not normalized: Principal.TenantID is canonicalized at
// the GIP token parse boundary (pkg/authn/gip.go), so every grant the
// iam-fga-sync consumer applies — and therefore every binding this
// resolves to — already carries the same lowercase uuid.String() form,
// regardless of when it was granted. Grouping by first-appearance order
// over the (already tenant-then-role sorted) input keeps the result
// deterministic. Returns a non-nil empty slice when bindings is empty so
// the JSON array is never null.
func groupByTenant(bindings []authz.RoleBinding) []tenantMembership {
	byTenant := map[string][]string{}
	var order []string
	for _, b := range bindings {
		if _, seen := byTenant[b.TenantID]; !seen {
			order = append(order, b.TenantID)
		}
		byTenant[b.TenantID] = append(byTenant[b.TenantID], string(b.Role))
	}
	out := make([]tenantMembership, 0, len(order))
	for _, id := range order {
		roles := byTenant[id]
		sort.Strings(roles)
		out = append(out, tenantMembership{TenantID: id, Roles: roles})
	}
	return out
}

// hasBindingForTenant reports whether any binding is for tenantID, a raw
// string comparison — see groupByTenant's doc comment for why every
// binding's tenant id is already canonical.
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
