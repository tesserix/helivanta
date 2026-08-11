package iam

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
		// Normalize before comparing and before echoing back: UUIDs are
		// routinely rendered in either case by different clients, and
		// binding:"required,uuid" validates the shape without
		// normalizing it. Comparing raw strings would let a request
		// that differs only in casing from the FGA-derived binding
		// (or from what a previous grant happened to write) be refused
		// with a 403 that looks like a deliberate denial of a real
		// member.
		target := normalizeTenantID(req.TenantID)
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
// normalized before grouping, not compared raw: two bindings for the
// same tenant can carry differently-cased ids if they were granted at
// different times, and without normalization those would split into
// two separate (and non-adjacent, since sorting is on the raw string)
// entries for what is really one hospital. Grouping by first-appearance
// order over the (already tenant-then-role sorted) input keeps the
// result deterministic. Returns a non-nil empty slice when bindings is
// empty so the JSON array is never null.
func groupByTenant(bindings []authz.RoleBinding) []tenantMembership {
	byTenant := map[string][]string{}
	var order []string
	for _, b := range bindings {
		id := normalizeTenantID(b.TenantID)
		if _, seen := byTenant[id]; !seen {
			order = append(order, id)
		}
		byTenant[id] = append(byTenant[id], string(b.Role))
	}
	out := make([]tenantMembership, 0, len(order))
	for _, id := range order {
		roles := byTenant[id]
		sort.Strings(roles)
		out = append(out, tenantMembership{TenantID: id, Roles: roles})
	}
	return out
}

// hasBindingForTenant reports whether any binding is for tenantID.
// tenantID must already be normalized (normalizeTenantID); each
// binding's tenant id is normalized before the comparison so casing
// differences never cause a real member to be treated as a stranger to
// their own tenant.
func hasBindingForTenant(bindings []authz.RoleBinding, tenantID string) bool {
	for _, b := range bindings {
		if normalizeTenantID(b.TenantID) == tenantID {
			return true
		}
	}
	return false
}

// normalizeTenantID canonicalizes a tenant id to uuid.UUID's lowercase
// string form, so casing differences between what a caller sends, what
// a previous grant wrote to OpenFGA, and what gets echoed back in a
// response never diverge. Falls back to a lowercased copy of the input
// when it doesn't parse as a UUID, so a malformed value still degrades
// to a case-insensitive comparison rather than being silently dropped
// from consideration.
func normalizeTenantID(id string) string {
	if parsed, err := uuid.Parse(id); err == nil {
		return parsed.String()
	}
	return strings.ToLower(id)
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
