package iam

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/session"
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

// meHandlers backs the self-service routes. They are authz.NoTenantMembership
// because any authenticated caller may ask what they can do and where
// they belong — including a caller whose membership in the tenant their
// token names has just been revoked (#781); the switch endpoint gates on
// membership itself, and permissions/tenants report only what the
// caller actually resolves to.
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
	signer *session.Signer
	// ttl and secureCookie mirror iam.LoginHandlers' fields exactly (see
	// login.go): a re-minted session cookie must carry the SAME lifetime
	// and the SAME `secure` flag semantics a freshly logged-in session
	// gets, or a switch could silently outlive login's bound or downgrade
	// the cookie's transport requirement.
	ttl          time.Duration
	secureCookie bool
}

func (m *Module) registerMe(g *platform.Router, deps platform.Deps) {
	me := &meHandlers{
		roles:        deps.Roles,
		signer:       deps.SessionSigner,
		ttl:          deps.SessionTTL,
		secureCookie: deps.SessionSecureCookie,
	}

	// NoTenantMembership, not Public: a caller whose membership in the
	// tenant their token names has just been revoked must still be able
	// to discover the other tenants they belong to and switch to one.
	// Requiring membership here would lock them out of the endpoint whose
	// whole job is answering that question (#781). Each of these gates
	// itself — switchTenant checks membership before minting, and
	// permissions reports only what the caller actually resolved to.
	g.GET("/me/permissions", authz.NoTenantMembership, permissions)
	g.GET("/me/tenants", authz.NoTenantMembership, me.tenants)
	g.POST("/me/tenant", authz.NoTenantMembership, me.switchTenant)
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

// switchTenant re-mints the caller's OWN Helivanta session for another tenant
// they are a member of (#838, spec D3). Everything before the mint is
// the gate; only past it does anything get issued, and no IdP round
// trip happens at all — this is the difference from the old GIP-backed
// design, which had to mint a custom token the browser then exchanged
// with Firebase for a fresh ID token.
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
	// by session.Verifier's parse of the Helivanta session (pkg/session), so
	// every FGA role tuple written by the iam-fga-sync consumer — and
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
	// 404, not 403: docs/standards/backend.md's cross-tenant rule
	// ("404, never 403, for cross-tenant access ... 403 would confirm
	// the subject exists somewhere") applies here exactly as it does to
	// any other resource lookup. target is a tenant identifier the
	// caller named; a 403 would confirm that tenant exists and only
	// withholds it, which is precisely the disclosure the rule exists
	// to prevent. This ALSO reconciles the endpoint with the rest of
	// the codebase: it used to answer 403 here (a hold-over from before
	// this rule was applied uniformly), the one remaining inconsistency
	// docs/superpowers/plans/2026-08-15-zitadel-auth.md Task 5 calls out
	// by name. login.go's equivalent refusal is brought in line with
	// this same reasoning in the same change — see its doc comment.
	if !hasBindingForTenant(bindings, target) {
		respond.NotFound(c, "tenant")
		return
	}
	// Everything above is the gate; only past it does anything get
	// re-minted. A session token is a credential for the target
	// tenant, so issuing one before the membership check — or issuing
	// one on any path where the check did not conclusively pass —
	// would hand out exactly the access the check exists to withhold.
	if h.signer == nil {
		respondMintUnavailable(c, errors.New("no session signer configured"))
		return
	}
	// p.AuthTime is carried through UNMODIFIED — never time.Now(). This
	// is the load-bearing line in this handler: p.AuthTime is when the
	// human last authenticated against Zitadel, and the #781 revocation
	// watermark compares a session's auth_time against a per-subject
	// revoked-after mark. Resetting it here would let a tenant switch
	// launder an old authentication into a fresh one and silently walk
	// straight through a revocation made between the original sign-in
	// and this switch (spec D2, D3).
	//
	// p.IdleDeadline is carried through UNMODIFIED for the same shape of
	// reason, on the #848 clock (spec D3): switching hospitals is not
	// human activity on a timer. A clinician who worked at ward A for
	// fourteen minutes and then switches to ward B has not touched
	// anything since — the switch is one click, and one click is not
	// evidence the session should get another full idle window. Only the
	// explicit activity endpoint (spec D4) moves this deadline. Resetting
	// it here would also hand any client a way to extend indefinitely
	// without a single keystroke: switch to the tenant you are already
	// working in, every fourteen minutes, forever.
	token, err := h.signer.Mint(p.Subject, target, p.AuthTime, p.IdleDeadline)
	if err != nil {
		respondMintUnavailable(c, err)
		return
	}
	// Same cookie flags iam.LoginHandlers.Login sets (login.go): httpOnly
	// and sameSite=Lax fixed, secure from config. This IS the switch —
	// there is no separate token for the client to exchange the way the
	// old custom_token was; the new session cookie itself is the
	// credential from here on.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authn.SessionCookie, token, int(h.ttl.Seconds()), "/", "", h.secureCookie, true)
	respond.OK(c, gin.H{"tenant_id": target})
}

// groupByTenant turns FGA role bindings into the response shape, one
// entry per tenant listing every role key held there. Tenant ids are
// compared raw, not normalized: Principal.TenantID is canonicalized at
// the Helivanta session parse boundary (pkg/session/verifier.go), so every grant the
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

// respondMintUnavailable fails closed when Helivanta itself cannot re-mint the
// session (no signer configured, or Signer.Mint refused): the caller
// keeps the tenant they had. It is a distinct code from authz_unavailable
// because the membership decision itself succeeded — only the credential
// could not be issued — and because a client that retries an
// authz_unavailable and a client that retries this are reacting to
// outages in two different systems. It is never a 200: a success with no
// new session is precisely the bug this endpoint had, a switch that
// reports success and changes nothing.
func respondMintUnavailable(c *gin.Context, err error) {
	requestid.Logger(c).ErrorContext(c.Request.Context(), "mint tenant session failed", "err", err)
	respond.Error(c, http.StatusServiceUnavailable,
		"session_unavailable", "could not issue a session for that hospital")
}
