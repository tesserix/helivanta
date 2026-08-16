// Package archtest enforces the architecture rules that lint alone
// cannot express precisely (same-module imports allowed, cross-module
// denied) and the registry-level invariants (unique migration IDs,
// subject/consumer naming).
package archtest

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/modules/reference"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const modulesPrefix = "github.com/tesserix/hms/internal/modules/"

// allModules must list every registered module. bootstrap.Modules()
// (backend/internal/bootstrap/modules.go) — shared by cmd/api and
// cmd/migrate — is the runtime source of truth; keep them in sync (the
// generator prints a reminder, and TestMainRegistersExactlyAllModules
// below fails CI on drift).
func allModules() []platform.Module {
	return []platform.Module{iam.New(nil), reference.New(), medicore.New(), pharmacy.New(), lab.New()}
}

// moduleOf maps a package path to its owning module name. Under
// packages.Config{Tests: true}, a module's own external test package and
// synthetic test-binary package get distinct PkgPaths built from the
// module's directory name: "<module>_test" (the external "_test" package)
// and "<module>.test" (the generated test-main package). Both belong to
// the same module as far as the cross-module-import rule is concerned, so
// strip those suffixes before comparing.
func moduleOf(pkgPath string) string {
	rest := strings.TrimPrefix(pkgPath, modulesPrefix)
	if rest == pkgPath {
		return ""
	}
	name := strings.SplitN(rest, "/", 2)[0]
	name = strings.TrimSuffix(name, ".test")
	name = strings.TrimSuffix(name, "_test")
	return name
}

// isContractImport reports whether imp is a module's published contract
// package — the one cross-module import permitted.
//
// Suffix match on "/contract" rather than an allowlist of paths: the
// point is the *shape* of the exception, not which modules currently use
// it, and a new module's contract should be legal to import the day it
// exists. What keeps this from being a hole is contract_test.go, which
// enforces that anything living behind this name is data only.
func isContractImport(imp string) bool {
	return strings.HasSuffix(imp, "/contract")
}

func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports, Tests: true}, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		from := moduleOf(p.PkgPath)
		for imp := range p.Imports {
			to := moduleOf(imp)
			if to == "" || to == from {
				continue
			}
			if isContractImport(imp) {
				continue
			}
			t.Errorf("module %q imports module %q (%s -> %s): cross-module data flows only via events, except a module's /contract package", from, to, p.PkgPath, imp)
		}
	}
}

func TestMigrationIDsAreGloballyUnique(t *testing.T) {
	seen := map[string]string{}
	record := func(owner, id string) {
		if prev, dup := seen[id]; dup {
			t.Errorf("migration id %q used by both %s and %s", id, prev, owner)
		}
		seen[id] = owner
	}
	for _, m := range tenantdb.Migrations() {
		record("tenantdb", m.ID)
	}
	for _, m := range events.Migrations() {
		record("events", m.ID)
	}
	for _, mod := range allModules() {
		for _, m := range mod.Migrations() {
			record(mod.Name(), m.ID)
		}
	}
}

var (
	subjectRe  = regexp.MustCompile(`^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`)
	consumerRe = regexp.MustCompile(`^[a-z]+-[a-z-]+$`)
)

func TestConsumerContracts(t *testing.T) {
	deps := platform.Deps{} // consumers are declared statically; nil deps fine for inspection
	for _, mod := range allModules() {
		for _, c := range mod.Consumers(deps) {
			if !consumerRe.MatchString(c.Name) {
				t.Errorf("module %s consumer name %q must match %s", mod.Name(), c.Name, consumerRe)
			}
			if !subjectRe.MatchString(c.Subject) {
				t.Errorf("module %s consumer subject %q must match %s", mod.Name(), c.Subject, subjectRe)
			}
		}
	}
}

// TestPublishedSubjectConstants derives its input from ⋃ Publishes()
// instead of a hand-maintained map. The map this replaced listed four
// subjects by hand and silently checked nothing when a fifth (iam's
// three) was never added to it — deriving from Publishes() means every
// module's declared subjects are covered automatically, including a
// module added after this test was written.
func TestPublishedSubjectConstants(t *testing.T) {
	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			if !subjectRe.MatchString(s) {
				t.Errorf("module %q publishes %q, which must match %s", m.Name(), s, subjectRe)
			}
		}
	}
}

// TestMainRegistersExactlyAllModules guards registry parity: every module
// constructor in bootstrap.Modules() — the single shared constructor
// cmd/api and cmd/migrate both call — must also appear in allModules(),
// and vice versa. Without this, a module added to one but forgotten in
// the other silently skips either production registration or the
// arch/coverage checks that walk allModules().
//
// This used to source-parse cmd/api/main.go's `[]platform.Module{...}`
// literal directly. That broke the moment the module list moved into
// bootstrap.Modules() (Task 14) so cmd/api and cmd/migrate could share
// it instead of each hand-writing a copy — comparing against the shared
// constructor is both the fix and the simpler check.
func TestMainRegistersExactlyAllModules(t *testing.T) {
	registered := map[string]bool{}
	for _, m := range bootstrap.Modules(nil) {
		registered[m.Name()] = true
	}
	if len(registered) == 0 {
		t.Fatalf("bootstrap.Modules() returned no modules")
	}

	expected := map[string]bool{}
	for _, mod := range allModules() {
		expected[mod.Name()] = true
	}

	for name := range registered {
		if !expected[name] {
			t.Errorf("bootstrap.Modules() registers module %q but allModules() in arch_test.go does not include it", name)
		}
	}
	for name := range expected {
		if !registered[name] {
			t.Errorf("allModules() in arch_test.go includes module %q but bootstrap.Modules() does not register it", name)
		}
	}
}

// TestModulesDoNotUseRawGinGroups keeps the compile-time guarantee that
// every route declares a permission: a module that reached for
// *gin.RouterGroup directly could register an unguarded route.
func TestModulesDoNotUseRawGinGroups(t *testing.T) {
	root := "../modules"
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "gin.RouterGroup") {
			t.Errorf("%s references gin.RouterGroup; modules must use *platform.Router", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk modules: %v", err)
	}
}

// dryRouteRegistrationChecker is the MembershipChecker used everywhere
// arch tests register a module's routes only to inspect
// platform.Router.Declared() — no request is ever dispatched through
// these routers, so what IsMember answers is irrelevant. It exists
// purely because platform.NewRouter panics on a nil checker (#781): an
// arch test that built its router with a nil checker would itself be
// masking the exact defect that panic exists to catch.
type dryRouteRegistrationChecker struct{}

func (dryRouteRegistrationChecker) IsMember(context.Context, string, string) (bool, error) {
	return true, nil
}

// TestEveryDeclaredPermissionIsGranted catches a module that guards a
// route with a permission it never declared in Permissions() — the route
// would be permanently unreachable for every non-admin role.
func TestEveryDeclaredPermissionIsGranted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, m := range allModules() {
		// Public and NoTenantMembership both declare no permission — see
		// their doc comments in pkg/authz/authz.go.
		declaredByGrants := map[authz.Permission]bool{authz.Public: true, authz.NoTenantMembership: true}
		for _, g := range m.Permissions() {
			declaredByGrants[g.Permission] = true
		}
		e := gin.New()
		r := platform.NewRouter(e.Group("/v1"), dryRouteRegistrationChecker{})
		m.Routes(r, platform.Deps{})
		for _, dr := range r.Declared() {
			if !declaredByGrants[dr.Permission] {
				t.Errorf("module %q guards a route with %q but does not declare it in Permissions()", m.Name(), dr.Permission)
			}
		}
	}
}

// routesDeclaring walks every registered module's routes (registration
// only — no request dispatched, see dryRouteRegistrationChecker) and
// returns "METHOD /path/without/the/v1/prefix" for every route declaring
// perm. The /v1 prefix is stripped by building the router directly at
// the root rather than under a /v1 group, so the strings this returns
// match noTenantMembershipAllowlist's keys exactly.
func routesDeclaring(t *testing.T, perm authz.Permission) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var found []string
	for _, m := range allModules() {
		e := gin.New()
		r := platform.NewRouter(e.Group(""), dryRouteRegistrationChecker{})
		m.Routes(r, platform.Deps{})
		for _, dr := range r.Declared() {
			if dr.Permission == perm {
				found = append(found, dr.Method+" "+dr.Path)
			}
		}
	}
	return found
}

// allDeclaredRoutes walks every registered module's routes (registration
// only — no request dispatched, see dryRouteRegistrationChecker) and
// returns every platform.DeclaredRoute with its path stripped of the /v1
// prefix, matching unpaginatedGETAllowlist's keys exactly. It shares the
// same registration walk as routesDeclaring rather than duplicating it;
// routesDeclaring filters to one permission, this returns everything so
// callers can filter on other fields (here, Method and Paginated).
func allDeclaredRoutes(t *testing.T) []platform.DeclaredRoute {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var found []platform.DeclaredRoute
	for _, m := range allModules() {
		e := gin.New()
		r := platform.NewRouter(e.Group(""), dryRouteRegistrationChecker{})
		m.Routes(r, platform.Deps{})
		found = append(found, r.Declared()...)
	}
	return found
}

// noTenantMembershipAllowlist is every route permitted to skip the
// tenant-membership check. Adding an entry is a security decision: such
// a route serves a caller who is not a member of the tenant their token
// names, so it must gate itself.
var noTenantMembershipAllowlist = map[string]string{
	"GET /iam/me/permissions": "reports what the caller resolved to; reveals nothing they did not already hold",
	"GET /iam/me/tenants":     "answers 'where do I belong'; unusable if it required belonging",
	"POST /iam/me/tenant":     "gates on membership itself before minting (iam/me.go switchTenant)",
	"POST /iam/me/sign-out":   "a revoked member must still be able to end their own session (iam/signout.go signOut)",
}

// TestNoTenantMembershipAllowlist pins the set of routes permitted to
// skip RequireMembership to an explicit, reviewed list (#781). The
// safety of every other route depends on this set staying small, so
// growing it must be a deliberate edit here, not an incidental one at
// the call site.
func TestNoTenantMembershipAllowlist(t *testing.T) {
	found := routesDeclaring(t, authz.NoTenantMembership)
	var want []string
	for k := range noTenantMembershipAllowlist {
		want = append(want, k)
	}
	require.ElementsMatch(t, want, found,
		"a route skipping the membership check must be added to noTenantMembershipAllowlist with a reason")
}

// unpaginatedGETAllowlist is every GET route permitted to return a
// collection-shaped body without a cursor. Two kinds qualify:
//
//   - single-item reads, which return one object
//   - collections bounded by construction rather than by tenant data:
//     a fixed role set, a person's memberships, a resolved permission set
//
// Anything else is a collection that will truncate silently once a
// tenant has enough rows, which is #816. Adding an entry here is a
// decision a reviewer sees.
var unpaginatedGETAllowlist = map[string]string{
	"GET /reference/pings/:id": "single item, not a collection",
	"GET /iam/roles":           "bounded: the fixed system role set",
	"GET /iam/me/tenants":      "bounded: one person's memberships",
	"GET /iam/me/permissions":  "bounded: one resolved permission set",
}

// TestEveryCollectionGETIsPaginated is the one hole platform.ListRoute's
// type signature cannot close (D4): nothing stops a handler registering
// a collection through plain g.GET instead. This test is the CI
// backstop — every GET route must either be registered through
// platform.ListRoute or be named in unpaginatedGETAllowlist with a
// reason.
func TestEveryCollectionGETIsPaginated(t *testing.T) {
	var offenders []string
	for _, rt := range allDeclaredRoutes(t) {
		if rt.Method != http.MethodGet || rt.Paginated {
			continue
		}
		key := rt.Method + " " + rt.Path
		if _, allowed := unpaginatedGETAllowlist[key]; allowed {
			continue
		}
		offenders = append(offenders, key)
	}
	require.Empty(t, offenders,
		"these GET routes return a collection without a cursor and will truncate silently (#816): "+
			"register them through platform.ListRoute, or add them to unpaginatedGETAllowlist with a reason")
}

// withAdminAllowlist is exactly the files permitted to call
// tenantdb.DB.WithAdmin — the reconciler (its one legitimate whole-system
// caller), the outbox dispatcher (#835 Task 1 — draining outbox_events
// across every tenant is its one job, and RLS on that table since
// 0002_events_outbox_tenant would otherwise blind it, see drainOnce's
// comment in pkg/events/bus.go), the outbox/ledger pruner (#835 Task 2 —
// Prune must run on the admin pool or a DELETE under WithSystem matches
// zero rows across every tenant and reports success, see Prune's comment
// in pkg/events/retention.go), and pkg/tenantdb itself (the method's own
// definition and its direct test). Extending this list is a real
// RLS-bypass decision, not a convenience; it belongs in code review, not a
// casual addition here.
var withAdminAllowlist = map[string]bool{
	"internal/platform/reconcile.go": true,
	"pkg/events/bus.go":              true,
	"pkg/events/retention.go":        true,
}

// withAdminAllowedDir reports whether path sits under a directory that's
// wholesale allowed to call WithAdmin.
func withAdminAllowedDir(path string) bool {
	return strings.HasPrefix(path, "pkg/tenantdb/")
}

// sourceReferencesWithAdmin parses src as Go source and reports whether
// it contains any selector expression whose field/method name is
// "WithAdmin" — whether invoked as a call (db.WithAdmin(ctx, fn)) or
// referenced bare as a method value (f := db.WithAdmin). A plain text
// scan for the literal ".WithAdmin(" call substring — this function's
// predecessor — misses the second form entirely: assigning the method
// value to a variable and calling that variable instead leaves no
// ".WithAdmin(" substring anywhere in the source, even though the
// resulting call still runs on the RLS-bypassing admin pool. Matching
// via go/ast instead of go/parser's token stream means both forms
// resolve to the same *ast.SelectorExpr node regardless of how the call
// is eventually invoked.
func sourceReferencesWithAdmin(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithAdmin" {
			found = true
		}
		return true
	})
	return found, nil
}

// TestWithAdminIsOnlyCalledFromTheAllowlist guards the one RLS bypass in
// the codebase the same way module isolation and route permissions are
// guarded: mechanically, not by convention. tenantdb.DB.WithAdmin runs on
// the admin pool, which is not subject to RLS at all — a request handler
// that reached for it, even by copy-pasting the reconciler's call, would
// see every tenant's rows. WithTenant is the only path request handlers
// may use; a genuine cross-tenant read belongs in review, not in an
// expanded allowlist.
func TestWithAdminIsOnlyCalledFromTheAllowlist(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// archtest's own source necessarily mentions "WithAdmin" (this
		// very check, its allowlist, its doc comments); excluding the
		// package avoids that self-match rather than allowlisting it,
		// which would otherwise read as "archtest may call WithAdmin".
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		if withAdminAllowlist[rel] || withAdminAllowedDir(rel) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceReferencesWithAdmin(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s references WithAdmin (as a call or a bare method value), which bypasses "+
				"RLS entirely and sees every tenant's rows. Request-path code must use WithTenant. "+
				"If you genuinely need a cross-tenant read, bring it to review — don't add this "+
				"file to withAdminAllowlist in arch_test.go on your own.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

// TestSourceReferencesWithAdminCatchesMethodValues is the discriminating
// test for the hardening itself: a fixture using db.WithAdmin as a bare
// method value (f := db.WithAdmin), with no ".WithAdmin(" substring
// anywhere in the source, is exactly the shape the old
// strings.Contains(src, ".WithAdmin(") scan would have let through
// silently. It must be caught alongside the ordinary call-syntax case,
// and ordinary source with no WithAdmin reference at all must still pass
// clean.
func TestSourceReferencesWithAdminCatchesMethodValues(t *testing.T) {
	const methodValueFixture = `package fixture

func evade(db *DB) func(func()) {
	// The call happens through f below, never spelled out as a direct
	// method call on db.
	f := db.WithAdmin
	return func(fn func()) { f(nil, fn) }
}
`
	const callSyntaxFixture = `package fixture

func direct(db *DB) {
	db.WithAdmin(nil, func() {})
}
`
	const cleanFixture = `package fixture

func direct(db *DB) {
	db.WithTenant(nil, "t", func() {})
}
`
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"method value evades text scan but not AST", methodValueFixture, true},
		{"ordinary call syntax", callSyntaxFixture, true},
		{"no WithAdmin reference at all", cleanFixture, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The old check this replaces: a literal substring scan for
			// the call-syntax pattern. Asserting it disagrees with the
			// method-value case is what proves that fixture would have
			// evaded the pre-hardening test.
			oldCheckWouldCatch := strings.Contains(tt.src, ".WithAdmin(")

			got, err := sourceReferencesWithAdmin([]byte(tt.src))
			if err != nil {
				t.Fatalf("sourceReferencesWithAdmin: %v", err)
			}
			if got != tt.want {
				t.Errorf("sourceReferencesWithAdmin() = %v, want %v", got, tt.want)
			}
			if tt.name == "method value evades text scan but not AST" && oldCheckWouldCatch {
				t.Fatalf("fixture no longer proves the hardening: the old substring scan would already catch it")
			}
		})
	}
}

// NewBusInNamespace exists so many tests can share one NATS server; it is
// not a deployment knob. A non-empty namespace renames the stream and
// prefixes every subject, so production code calling it would quietly
// publish into a subject space nothing consumes — a silent outage rather
// than a failure. Tests may call it freely; shipped code may not.
func TestNewBusInNamespaceIsTestOnly(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// The declaration itself, and this check's own description of it.
		if rel == "pkg/events/bus.go" || strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		// Test-support packages are not _test.go files but never ship:
		// nothing under cmd/ imports them, so they are the intended
		// callers. Narrowing the rule to shipped code is the point —
		// this guards deployment, not the word itself.
		if strings.HasPrefix(rel, "internal/testinfra/") || strings.HasPrefix(rel, "internal/testutil/") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "NewBusInNamespace") {
			t.Errorf("%s calls events.NewBusInNamespace, which is test-only. Production code "+
				"must use events.NewBus: a namespace renames the stream and prefixes every "+
				"subject, so consumers would silently stop seeing events.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

// TestSourceCallsGormOpenDetectsBothSpellings is the discriminating test for
// the detector itself. A plain text scan for "gorm.Open(" misses the aliased
// import form, which compiles and runs identically. Both must be caught, and
// source that merely mentions a different Open must pass clean.
func TestSourceCallsGormOpenDetectsBothSpellings(t *testing.T) {
	const plainFixture = `package fixture

import "gorm.io/gorm"

func f(d gorm.Dialector) { gorm.Open(d) }
`
	const aliasedFixture = `package fixture

import g "gorm.io/gorm"

func f(d g.Dialector) { g.Open(d) }
`
	const cleanFixture = `package fixture

import "os"

func f() { os.Open("x") }
`
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"plain gorm.Open", plainFixture, true},
		{"aliased import still resolves to the gorm package", aliasedFixture, true},
		{"an unrelated Open is not a match", cleanFixture, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceCallsGormOpen([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got != tc.want {
				t.Errorf("sourceCallsGormOpen = %v, want %v", got, tc.want)
			}
		})
	}
}

// gormOpenAllowlist is exactly the files permitted to call gorm.Open.
// Every call site decides, in its gorm.Config, whether GORM's logger
// echoes executed SQL — and GORM inlines parameter values into that SQL,
// so a non-silent pool is a full patient-data dump on the error and
// slow-query paths. Keeping the set of call sites to two means the
// decision is reviewable; adding a third is a PHI decision, not a
// convenience, and belongs in review rather than in this map.
var gormOpenAllowlist = map[string]bool{
	"pkg/tenantdb/db.go":                    true,
	"internal/testinfra/containers_test.go": true,
}

// sourceCallsGormOpen parses src and reports whether it calls Open on the
// gorm package, under whatever local name the file imports it as. Matching
// on the import path rather than the literal identifier "gorm" is what
// makes an aliased import (`import g "gorm.io/gorm"`, then `g.Open(...)`)
// resolve the same as the ordinary spelling; a text scan for "gorm.Open("
// would see nothing there at all.
func sourceCallsGormOpen(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	names := map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "gorm.io/gorm" {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
		} else {
			names["gorm"] = true
		}
	}
	if len(names) == 0 {
		return false, nil
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Open" {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && names[ident.Name] {
			found = true
		}
		return true
	})
	return found, nil
}

// finalizeEndpointPath is the Zitadel OIDC endpoint that turns a session
// into an authorization code. A POST here IS the completion of a login.
const finalizeEndpointPath = "/v2/oidc/auth_requests/"

// finalizeCallSite is the one file permitted to POST to that endpoint.
// Not a map like the other allowlists in this file: the whole control is
// that the set has exactly one element, so there is nothing here for a
// contributor to append to.
const finalizeCallSite = "internal/modules/iam/loginclient/client.go"

// exprMentions reports whether e, anywhere inside it, is a string literal
// containing want. It recurses through binary expressions and calls
// because the real call site builds its path by concatenation —
// `"/v2/oidc/auth_requests/" + url.PathEscape(id)` — so the literal is
// never the argument node itself.
func exprMentions(e ast.Expr, want string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, want) {
			found = true
		}
		return true
	})
	return found
}

// exprIsPOST reports whether e denotes the POST method — either the
// http.MethodPost constant (under any import alias, matched on the
// selector name, which is unambiguous) or the bare "POST" string that
// http.NewRequest is just as happy to take.
func exprIsPOST(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name == "MethodPost"
	case *ast.BasicLit:
		s, err := strconv.Unquote(v.Value)
		return err == nil && s == http.MethodPost
	}
	return false
}

// sourcePostsToFinalizeEndpoint parses src and reports whether it contains
// a single call expression that both names POST and carries the finalize
// endpoint path. Requiring both in the SAME call is what keeps this from
// firing on a fake Zitadel in a test handler, which mentions the path and
// http.MethodPost in separate expressions, while still catching every real
// spelling of the call: c.do(ctx, http.MethodPost, path+id, …) and
// http.NewRequest("POST", base+path+id, …) have the same shape here.
func sourcePostsToFinalizeEndpoint(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var hasPOST, hasPath bool
		for _, arg := range call.Args {
			if exprIsPOST(arg) {
				hasPOST = true
			}
			if exprMentions(arg, finalizeEndpointPath) {
				hasPath = true
			}
		}
		if hasPOST && hasPath {
			found = true
		}
		return true
	})
	return found, nil
}

// TestFinalizeCallSiteIsUnique is the CI half of spec D4's structural
// control; the other half is that loginclient.finalize is unexported, so
// the only way to reach it from outside the package is
// CompleteIfSufficient.
//
// Unexporting alone does not stop someone writing a fresh POST to the same
// endpoint in a handler, and that is the dangerous shape: the spike (§2,
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md) proved
// Zitadel returns HTTP 200 and a valid authorization code for a
// password-only session even under a forceMfa policy. A second call site
// would therefore work perfectly — while silently skipping the factor
// every user was supposed to present. Nothing observable would be wrong,
// which is exactly why the control has to be mechanical.
func TestFinalizeCallSiteIsUnique(t *testing.T) {
	root := "../.."
	var callSites []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// archtest's own source necessarily spells the endpoint out (this
		// check, its constants, its doc comments). Excluding the package
		// avoids the self-match rather than allowlisting it, which would
		// otherwise read as "archtest may finalize logins".
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		posts, err := sourcePostsToFinalizeEndpoint(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if posts {
			callSites = append(callSites, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	require.ElementsMatch(t, []string{finalizeCallSite}, callSites,
		"the OIDC finalize call must stay behind CompleteIfSufficient (spec D4) — "+
			"Zitadel does not enforce forceMfa for a login client")
}

// TestGormOpenIsOnlyCalledFromTheAllowlist protects a PHI control that is
// otherwise invisible. GORM's logger renders executed SQL with parameter
// values inlined — `INSERT INTO patients VALUES (2,'HQ-OPD-0001427','Suresh
// Kumar')` — on its error and slow-query paths. The only thing keeping that
// out of the logs is logger.Silent in each gorm.Config, and a new pool
// opened anywhere else would default to logger.Warn and start emitting.
// tenantdb.Open cannot defend against a call site it does not own, so the
// defence has to be here.
func TestGormOpenIsOnlyCalledFromTheAllowlist(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// archtest's own source necessarily mentions gorm.Open (this check,
		// its allowlist, its fixtures). Excluding the package avoids the
		// self-match rather than allowlisting it, which would otherwise read
		// as "archtest may open pools".
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		if gormOpenAllowlist[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceCallsGormOpen(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s calls gorm.Open. GORM's logger inlines parameter values into the "+
				"SQL it echoes, so a pool opened without logger.Silent writes patient data "+
				"to the logs on every failing or slow query. Use tenantdb.Open. If you "+
				"genuinely need another pool, bring it to review — don't add this file to "+
				"gormOpenAllowlist in arch_test.go on your own.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}
