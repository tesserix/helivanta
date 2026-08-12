package iam

import (
	"errors"
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
	// Current is true for the one membership matching the caller's
	// tenant_id token claim (authn.Principal.TenantID) — the tenant the
	// request actually ran in, not whichever entry happens to sort
	// first. The picker in apps/shell needs this to select and display
	// the real current tenant instead of guessing from array order,
	// which is what let the control silently drift out of sync with the
	// session after a switch (see groupByTenant).
	Current bool `json:"current"`
}

// switchRequest.TenantID's `uuid` binding tag is the actual enforcement
// point for tenant-id casing on this endpoint: go-playground/validator's
// uuid rule matches its uuidRegexString, which is lowercase-only
// ([0-9a-f], not [0-9a-fA-F]). An upper- or mixed-cased tenant id is
// rejected with 400 by Gin's binding validation before the handler body
// runs at all, so the raw (non-normalizing) comparison against bindings
// below never sees a non-canonical value from a well-formed request.
type switchRequest struct {
	TenantID string `json:"tenant_id" binding:"required,uuid"`
}

// meHandlers backs the self-service routes. They are authz.Public because
// any authenticated caller may ask what they can do and where they
// belong; the switch endpoint gates on membership itself.
//
// tenants and switchTenant resolve membership from OpenFGA via
// roles.ListRoles rather than querying iam_members directly. iam_members
// is RLS-forced and every runtime accessor (WithTenant) scopes to a
// single tenant GUC — there is no accessor that can answer "which
// tenants does this subject belong to" without already knowing the
// tenant, which is exactly the question these routes exist to answer.
// tenantdb.WithAdmin bypasses RLS and would work mechanically, but it is
// boot/ops-only (see TestWithAdminIsOnlyCalledFromTheAllowlist in
// internal/archtest) and reads every tenant's rows, not just role keys.
// OpenFGA already holds membership as tuples written by the
// iam-fga-sync consumer, so resolving from there needs no migration, no
// new RLS policy, and no privileged accessor on a request path.
type meHandlers struct {
	roles  platform.RoleLister
	tokens authn.TokenMinter
}

func (m *Module) registerMe(g *platform.Router, deps platform.Deps) {
	me := &meHandlers{roles: deps.Roles, tokens: deps.Tokens}

	g.GET("/me/permissions", authz.Public, permissions)
	g.GET("/me/tenants", authz.Public, me.tenants)
	g.POST("/me/tenant", authz.Public, me.switchTenant)
}

// permissions reports the caller's resolved permission set. It takes no
// handler-struct dependencies: everything it needs already travels on
// the request context via authz.PermissionsFrom / authn.PrincipalFrom.
func permissions(c *gin.Context) {
	set, ok := authz.PermissionsFrom(c)
	if !ok {
		respond.InternalErr(c, errors.New("permission set missing from context"), "authorization not initialized")
		return
	}
	// subject and tenant_id travel with the permission set so the
	// client-side cache in @hms/api can stamp its entry with whose
	// permissions it holds. The session cookie is httpOnly, so this
	// response is the only place the browser can learn that identity.
	// The stamp makes the entry self-describing — useful for
	// debugging and for the cache's own shape validation — but it is
	// not what keeps one user's nav from being shown to another:
	// nothing compares it against the session before painting, and a
	// pre-response client-side check is impossible with an httpOnly
	// cookie. That protection comes from clearing the cache on login,
	// logout and tenant switch, plus the fresh response overwriting
	// the entry.
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	respond.OK(c, gin.H{
		"data":      set.Sorted(),
		"subject":   p.Subject,
		"tenant_id": p.TenantID,
	})
}

// tenants lists every tenant the caller belongs to, with their roles.
func (h *meHandlers) tenants(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	bindings, err := h.roles.ListRoles(c.Request.Context(), p.Subject)
	if err != nil {
		respondRolesUnavailable(c, err)
		return
	}
	respond.OK(c, gin.H{"data": groupByTenant(bindings, p.TenantID)})
}

// switchTenant mints a custom token for another tenant the caller is a
// member of. Everything before the mint is the gate; only past it does
// anything get issued.
func (h *meHandlers) switchTenant(c *gin.Context) {
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
	// the same canonical form back here. The actual enforcement
	// backstop, though, is switchRequest.TenantID's `uuid` binding
	// tag (see its doc comment): a non-canonical casing never even
	// reaches this comparison, because binding validation rejects it
	// with 400 first.
	target := req.TenantID
	bindings, err := h.roles.ListRoles(c.Request.Context(), p.Subject)
	if err != nil {
		respondRolesUnavailable(c, err)
		return
	}
	if !hasBindingForTenant(bindings, target) {
		respond.Forbidden(c, "not a member of that tenant")
		return
	}
	// Everything above is the gate; only past it does anything get
	// minted. A custom token is a credential for the target tenant,
	// so issuing one before the membership check — or issuing one on
	// any path where the check did not conclusively pass — would hand
	// out exactly the access the check exists to withhold.
	//
	// target is minted as sent (already validated as a UUID by the
	// binding tag). Casing does not survive the round trip anyway:
	// principalFromToken canonicalizes the claim to lowercase
	// uuid.String() when the re-minted token comes back (see
	// pkg/authn/gip.go).
	if h.tokens == nil {
		respondMintUnavailable(c, errors.New("no token minter configured"))
		return
	}
	token, err := h.tokens.CustomTokenWithClaims(c.Request.Context(), p.Subject,
		map[string]interface{}{"tenant_id": target})
	if err != nil {
		respondMintUnavailable(c, err)
		return
	}
	// custom_token is what actually performs the switch: the client
	// exchanges it for a fresh ID token carrying the new tenant_id
	// claim and replaces its session with it. tenant_id is kept
	// alongside so callers can confirm which tenant the token is for
	// without decoding it, and so the response shape stays additive.
	respond.OK(c, gin.H{"tenant_id": target, "custom_token": token})
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
//
// currentTenantID is the caller's tenant_id token claim (already
// canonical, same as every binding's TenantID — see above), compared
// raw for the same reason. It marks exactly one entry Current: true, the
// tenant the request actually ran in. Without this the shell picker had
// no way to know which membership was current and fell back to guessing
// from array order, which drifted from reality after every switch.
func groupByTenant(bindings []authz.RoleBinding, currentTenantID string) []tenantMembership {
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
		out = append(out, tenantMembership{TenantID: id, Roles: roles, Current: id == currentTenantID})
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

// respondMintUnavailable fails closed when the identity provider cannot
// issue the new session: the caller keeps the tenant they had. It is a
// distinct code from authz_unavailable because the membership decision
// itself succeeded — only the credential could not be issued — and
// because a client that retries an authz_unavailable and a client that
// retries this are reacting to outages in two different systems. It is
// never a 200: a success with no token is precisely the bug this
// endpoint had, a switch that reports success and changes nothing.
func respondMintUnavailable(c *gin.Context, err error) {
	requestid.Logger(c).ErrorContext(c.Request.Context(), "mint tenant token failed", "err", err)
	respond.Error(c, http.StatusServiceUnavailable,
		"session_unavailable", "could not issue a session for that hospital")
}
