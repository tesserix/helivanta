package platform

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/pkg/authz"
)

// Router wraps a gin route group so that every route must declare the
// permission it requires. This replaces the boot-time check the
// repo-setup spec proposed: an undeclared route is not expressible, so
// the guarantee holds at compile time rather than at startup.
//
// Use authz.Public for deliberately unguarded routes — it is an explicit,
// greppable opt-out rather than an omission. Public no longer means the
// route skips tenant membership, only that it declares no permission
// (#781) — see authz.NoTenantMembership for the narrower opt-out.
type Router struct {
	group            *gin.RouterGroup
	declared         *[]DeclaredRoute
	membership       authz.MembershipChecker
	scopedAuthorizer authz.ScopedAuthorizer
}

// DeclaredRoute is one route's method, path and permission, as recorded
// by handle. Declared() returns these rather than bare permissions so a
// caller that needs to know WHICH route carries a permission — the
// NoTenantMembership allowlist arch test — doesn't have to re-register
// every module's routes a second time to find out.
type DeclaredRoute struct {
	Method         string
	Path           string
	Permission     authz.Permission
	ScopeKind      string
	ScopeParameter string
	// Paginated is true for routes registered through ListRoute. The
	// arch test uses it to fail any collection GET registered the old
	// way — the one hole ListRoute's type signature cannot close.
	Paginated bool
	// Counted is true for routes registered through CountedListRoute,
	// whose page carries a total (#1295 D1).
	Counted bool
}

// NewRouter builds the root router. membership is required, not
// optional: a nil checker would mean every route silently skips the
// membership gate, which is the defect this parameter exists to close
// (#781) — so a nil argument is a programming error caught immediately
// at construction, not a silent bypass discovered later in production.
func NewRouter(g *gin.RouterGroup, membership authz.MembershipChecker) *Router {
	if membership == nil {
		panic("platform: NewRouter requires a non-nil MembershipChecker")
	}
	return &Router{group: g, declared: &[]DeclaredRoute{}, membership: membership}
}

// Group returns a nested router that shares the parent's declaration
// list and membership checker, so Declared() sees every route regardless
// of nesting.
func (r *Router) Group(prefix string) *Router {
	return &Router{group: r.group.Group(prefix), declared: r.declared, membership: r.membership, scopedAuthorizer: r.scopedAuthorizer}
}

// WithScopedAuthorizer configures scoped authority before route registration.
func (r *Router) WithScopedAuthorizer(authority authz.ScopedAuthorizer) *Router {
	r.scopedAuthorizer = authority
	return r
}

// POSTScopedHospital declares a command with legacy or hospital-scoped authority.
func (r *Router) POSTScopedHospital(path string, permission authz.Permission, hospitalParam string, handlers ...gin.HandlerFunc) {
	r.handleHospitalScope(http.MethodPost, path, permission, hospitalParam, false, handlers...)
}

// GETScopedHospital declares a purpose-bound hospital-scoped read.
func (r *Router) GETScopedHospital(path string, permission authz.Permission, hospitalParam string, handlers ...gin.HandlerFunc) {
	r.handleHospitalScope(http.MethodGet, path, permission, hospitalParam, false, handlers...)
}

// GETScopedHospitalProjection preserves legacy admission until an explicit scoped projection is selected.
func (r *Router) GETScopedHospitalProjection(path string, permission authz.Permission, hospitalParam, queryKey, version string, handlers ...gin.HandlerFunc) {
	r.handleHospitalScopeUsing(http.MethodGet, path, permission, hospitalParam, false, authz.RequireHospitalScopeProjection(permission, hospitalParam, r.scopedAuthorizer, queryKey, version), handlers...)
}

// PUTScopedHospitalProjection declares an idempotent command with an explicit scoped projection.
func (r *Router) PUTScopedHospitalProjection(path string, permission authz.Permission, hospitalParam, queryKey, version string, handlers ...gin.HandlerFunc) {
	r.handleHospitalScopeUsing(http.MethodPut, path, permission, hospitalParam, false, authz.RequireHospitalScopeProjection(permission, hospitalParam, r.scopedAuthorizer, queryKey, version), handlers...)
}

// POSTScopedHospitalProjection declares a command with an explicit scoped projection.
func (r *Router) POSTScopedHospitalProjection(path string, permission authz.Permission, hospitalParam, queryKey, version string, handlers ...gin.HandlerFunc) {
	r.handleHospitalScopeUsing(http.MethodPost, path, permission, hospitalParam, false, authz.RequireHospitalScopeProjection(permission, hospitalParam, r.scopedAuthorizer, queryKey, version), handlers...)
}

func (r *Router) handleHospitalScope(method, path string, permission authz.Permission, hospitalParam string, paginated bool, handlers ...gin.HandlerFunc) {
	r.handleHospitalScopeUsing(method, path, permission, hospitalParam, paginated, authz.RequireHospitalScope(permission, hospitalParam, r.scopedAuthorizer), handlers...)
}

func (r *Router) handleHospitalScopeUsing(method, path string, permission authz.Permission, hospitalParam string, paginated bool, admission gin.HandlerFunc, handlers ...gin.HandlerFunc) {
	fullPath := r.group.BasePath() + path
	found := false
	for _, segment := range strings.Split(fullPath, "/") {
		if hospitalParam != "" && segment == ":"+hospitalParam {
			found = true
			break
		}
	}
	if !found || permission == "" || permission == authz.Public || permission == authz.NoTenantMembership {
		panic("platform: scoped route requires a permission and declared hospital parameter")
	}
	*r.declared = append(*r.declared, DeclaredRoute{Method: method, Paginated: paginated, Path: fullPath, Permission: permission, ScopeKind: "hospital", ScopeParameter: hospitalParam})
	chain := []gin.HandlerFunc{authz.RequireMembership(r.membership), admission}
	r.group.Handle(method, path, append(chain, handlers...)...)
}

func (r *Router) GET(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodGet, path, perm, false, h)
}

func (r *Router) POST(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPost, path, perm, false, h)
}

func (r *Router) PUT(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPut, path, perm, false, h)
}

func (r *Router) PATCH(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPatch, path, perm, false, h)
}

func (r *Router) DELETE(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodDelete, path, perm, false, h)
}

func (r *Router) handle(method, path string, perm authz.Permission, paginated bool, h []gin.HandlerFunc) {
	r.declare(path, DeclaredRoute{Method: method, Path: r.group.BasePath() + path, Permission: perm, Paginated: paginated}, h)
}

// declare records d and mounts h at path (relative to this group) behind
// the membership and permission gates.
func (r *Router) declare(path string, d DeclaredRoute, h []gin.HandlerFunc) {
	method, perm := d.Method, d.Permission
	*r.declared = append(*r.declared, d)
	chain := make([]gin.HandlerFunc, 0, len(h)+2)
	// Membership first: a non-member gets the same answer on every route
	// regardless of what permission it declares, and a route marked
	// NoTenantMembership never pays for the call at all — see that
	// marker's doc comment for why that matters (sign-out during an
	// OpenFGA outage).
	if perm != authz.NoTenantMembership {
		chain = append(chain, authz.RequireMembership(r.membership))
	}
	chain = append(chain, authz.Require(perm))
	chain = append(chain, h...)
	r.group.Handle(method, path, chain...)
}

// Declared lists every route declared through this router and its nested
// groups, including authz.Public and authz.NoTenantMembership routes.
// The architecture test and the adversarial matrix suite both build on
// it.
func (r *Router) Declared() []DeclaredRoute {
	return append([]DeclaredRoute(nil), *r.declared...)
}
