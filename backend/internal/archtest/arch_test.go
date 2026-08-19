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
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/lab"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/reference"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const modulesPrefix = "github.com/tesserix/helivanta/internal/modules/"

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
	subjectRe  = regexp.MustCompile(`^helivanta\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`)
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
	"POST /auth/session/activity": "extends idle_deadline for a caller who is genuinely still present; " +
		"has nothing to do with what they are a member of (#848 Task 4, iam/activity.go)",
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
//
// cmd/bootstrap/main_test.go is the one _test.go entry, and it is here for
// the opposite reason to the others: not because the code under test needs
// the bypass, but because the ASSERTIONS do. cmd/bootstrap/main.go itself
// uses WithTenant — deliberately, so its write is scoped by the RLS policy
// rather than exempt from it. Its tests then check that a rejected input
// left NO row behind, and under WithTenant a row written to the wrong
// tenant is invisible, so require.Empty would pass on exactly the bug it
// guards. Reading back on the admin pool is what lets those assertions
// fail. The _test.go suffix is not itself a reason for an entry here: a
// test that uses WithAdmin to SET UP request-path behaviour is hiding the
// same bypass this rule exists to surface, which is why test files are
// allowlisted one at a time rather than excluded as a class.
var withAdminAllowlist = map[string]bool{
	// #894 emptied this of its cross-tenant callers. reconcile.go,
	// events/bus.go and events/retention.go were all here because they
	// must read across tenants — which WithAdmin never actually did, since
	// it connects as the schema owner and FORCE RLS binds the owner. They
	// use WithAllTenants now; see withAllTenantsAllowlist below.
	//
	// Nothing remains. Every caller that reached for WithAdmin wanted to
	// cross tenants, and none of them ever could — so an empty allowlist
	// is the honest state, not an oversight. WithAdmin is still used
	// inside pkg/tenantdb (Migrate, LintRLS, its own tests), which
	// withAdminAllowedDir covers.
}

// withAllTenantsAllowlist names the files permitted to call
// tenantdb.DB.WithAllTenants — the system pool, whose role holds
// BYPASSRLS and is the only way to cross tenant boundaries.
//
// These three are the whole-system operations that have no single tenant
// to scope to: the authorization reconciler, the outbox dispatcher, and
// the outbox/ledger pruner. Each enumerates every tenant's rows in one
// pass by definition.
//
// This list is a tighter control than the one it replaces, and should stay
// that way: WithAdmin's privilege was ambient (owner rights on a pool used
// for migrations), whereas this one is a single role attribute that exists
// for these three callers alone. Adding a file here is an RLS-bypass
// decision and belongs in code review, not a casual addition.
var withAllTenantsAllowlist = map[string]bool{
	"internal/platform/reconcile.go": true,
	"pkg/events/bus.go":              true,
	"pkg/events/retention.go":        true,
	// The one _test.go entry, here because the ASSERTIONS need the bypass
	// rather than the code under test. cmd/bootstrap/main.go uses
	// WithTenant — its write is scoped by the policy, not exempt from it —
	// and its tests then check that a rejected input left NO row behind.
	// Under WithTenant a row written to the wrong tenant is invisible, so
	// require.Empty would pass on precisely the bug it guards. The
	// _test.go suffix is not itself a reason for an entry: a test using a
	// privileged pool to SET UP request-path behaviour hides the same
	// bypass this rule exists to surface, which is why test files are
	// allowlisted one at a time.
	"cmd/bootstrap/main_test.go": true,
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
func sourceReferencesMethod(src []byte, method string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			found = true
		}
		return true
	})
	return found, nil
}

// sourceReferencesWithAdmin is retained as a named wrapper because its
// own table-driven unit test below documents the two call shapes this
// must catch, and that documentation is about WithAdmin specifically.
func sourceReferencesWithAdmin(src []byte) (bool, error) {
	return sourceReferencesMethod(src, "WithAdmin")
}

// privilegedPoolRule describes one "this method may only be called from
// these files" check. Both pool bypasses are expressed through it so the
// two rules cannot drift apart in how they scan, only in what they allow.
type privilegedPoolRule struct {
	method    string
	allowlist map[string]bool
	allowDir  func(string) bool
	message   string
}

// assertPoolMethodIsAllowlisted walks the backend and fails for any file
// outside the rule's allowlist that references the method at all — as a
// call or as a bare method value.
func assertPoolMethodIsAllowlisted(t *testing.T, rule privilegedPoolRule) {
	t.Helper()
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
		// archtest's own source necessarily mentions these names (the
		// checks, the allowlists, the doc comments); excluding the package
		// avoids that self-match rather than allowlisting it, which would
		// otherwise read as "archtest may call them".
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		if rule.allowlist[rel] || (rule.allowDir != nil && rule.allowDir(rel)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceReferencesMethod(src, rule.method)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s references %s. %s", rel, rule.method, rule.message)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestWithAllTenantsIsOnlyCalledFromTheAllowlist guards the system pool,
// which is the codebase's ONLY actual RLS bypass since #894.
//
// It matters more than the WithAdmin rule it sits beside, because this
// privilege really works: a handler that reached for WithAllTenants would
// genuinely serve every tenant's rows, whereas the same mistake with
// WithAdmin merely returned nothing. The rule that was load-bearing in
// intention is now load-bearing in fact.
func TestWithAllTenantsIsOnlyCalledFromTheAllowlist(t *testing.T) {
	assertPoolMethodIsAllowlisted(t, privilegedPoolRule{
		method:    "WithAllTenants",
		allowlist: withAllTenantsAllowlist,
		allowDir:  withAdminAllowedDir, // pkg/tenantdb/ owns the method and its tests
		message: "That runs on the system pool, whose role holds BYPASSRLS, so it sees " +
			"every tenant's rows in every table. Request-path code must use WithTenant. " +
			"If you genuinely need a whole-system read, bring it to review — don't add " +
			"this file to withAllTenantsAllowlist in arch_test.go on your own.",
	})
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

// fileStringConsts collects every file-level `const`/`var` bound directly
// to a string literal, so the path can be recognised when it has been
// lifted out of the call into a named constant. Without this the detector
// keys on the literal text at the call site, and `const p =
// "/v2/oidc/auth_requests/"` followed by `http.NewRequest(http.MethodPost,
// base+p+id, …)` — an ordinary, entirely innocent-looking refactor —
// becomes invisible to it.
//
// It resolves one level, not arbitrary constant expressions: a path
// assembled from two concatenated constants would still evade this. That
// bound is deliberate (a full constant folder here would be its own source
// of bugs) and it is why unexporting finalize, not this test, is the
// primary control — see TestFinalizeCallSiteIsUnique.
func fileStringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

// stringValue returns the string e denotes, if it denotes one directly: a
// string literal, or an identifier bound to one by consts.
func stringValue(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	}
	return "", false
}

// exprMentions reports whether e, anywhere inside it, denotes a string
// containing want. It recurses because the real call site builds its path
// by concatenation — `"/v2/oidc/auth_requests/" + url.PathEscape(id)` — so
// the string is never the argument node itself.
func exprMentions(e ast.Expr, want string, consts map[string]string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if s, ok := stringValue(expr, consts); ok && strings.Contains(s, want) {
			found = true
		}
		return true
	})
	return found
}

// exprIsPOST reports whether e denotes the POST method — the
// http.MethodPost constant (matched on the selector name, which is
// unambiguous under any import alias), the bare "POST" string that
// http.NewRequest is just as happy to take, or a local constant bound to
// either.
func exprIsPOST(e ast.Expr, consts map[string]string) bool {
	if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "MethodPost" {
		return true
	}
	s, ok := stringValue(e, consts)
	return ok && s == http.MethodPost
}

// callIsPOST reports whether call issues a POST, by either route: a method
// ARGUMENT (http.NewRequest(http.MethodPost, …), c.do(ctx,
// http.MethodPost, …)) or a method-named CALLEE, where POST is in the
// helper's name and there is no method argument at all — http.Post(url,
// …), hc.Post(…), resty's .Post(…). Keying only on the argument, as the
// first version of this test did, meant http.Post reached the finalize
// endpoint completely undetected: the single most natural way to write the
// bypass was the one shape the control could not see.
func callIsPOST(call *ast.CallExpr, consts map[string]string) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "Post", "PostForm":
			return true
		}
	}
	for _, arg := range call.Args {
		if exprIsPOST(arg, consts) {
			return true
		}
	}
	return false
}

// sourcePostsToFinalizeEndpoint parses src and reports whether it contains
// a single call expression that both issues a POST and carries the
// finalize endpoint path. Requiring both in the SAME call is what keeps
// this from firing on code that merely mentions the path, while catching
// every spelling of the call the detector's own fixtures exercise
// (TestSourcePostsToFinalizeEndpointCatchesEvasions).
func sourcePostsToFinalizeEndpoint(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	consts := fileStringConsts(f)
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !callIsPOST(call, consts) {
			return true
		}
		for _, arg := range call.Args {
			if exprMentions(arg, finalizeEndpointPath, consts) {
				found = true
			}
		}
		return true
	})
	return found, nil
}

// TestSourcePostsToFinalizeEndpointCatchesEvasions is the discriminating
// test for the detector itself, and it exists because the first version of
// this control did NOT hold the line it claimed: review probed it and
// found http.Post and a hoisted path constant both passed straight
// through, while a harmless httptest.NewRequest in a _test.go was flagged.
// The mutation that was supposed to have proven the detector — a
// http.NewRequest("POST", literal+id, …) probe — differed from the real
// call only in the helper's name, not in either feature the detector keys
// on, so it proved nothing. These fixtures are the shapes that actually
// evaded it.
func TestSourcePostsToFinalizeEndpointCatchesEvasions(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"the real call shape: method argument plus a literal path", `package fixture

func f(c *C, ctx Ctx, id string) { c.do(ctx, http.MethodPost, "/v2/oidc/auth_requests/"+id, nil) }
`, true},
		{"http.Post has no method argument at all", `package fixture

func f(base, id string) { http.Post(base+"/v2/oidc/auth_requests/"+id, "application/json", nil) }
`, true},
		{"path hoisted into a file-level const", `package fixture

const finalizePath = "/v2/oidc/auth_requests/"

func f(base, id string) { http.NewRequest(http.MethodPost, base+finalizePath+id, nil) }
`, true},
		{"both evasions at once", `package fixture

const finalizePath = "/v2/oidc/auth_requests/"

func f(hc *http.Client, base, id string) { hc.Post(base+finalizePath+id, "application/json", nil) }
`, true},
		{"a bare POST string rather than the constant", `package fixture

func f(base, id string) { http.NewRequest("POST", base+"/v2/oidc/auth_requests/"+id, nil) }
`, true},
		{"reading the auth request is a GET, not a completion", `package fixture

func f(c *C, ctx Ctx, id string) { c.do(ctx, http.MethodGet, "/v2/oidc/auth_requests/"+id, nil) }
`, false},
		{"a fake Zitadel that only mentions the path is not a call site", `package fixture

func handler(w W, r *R) {
	if r.Method == http.MethodPost && r.URL.Path == "/v2/oidc/auth_requests/V2_1" {
		w.Write(nil)
	}
}
`, false},
		{"POSTing somewhere else entirely", `package fixture

func f(c *C, ctx Ctx) { c.do(ctx, http.MethodPost, "/v2/sessions", nil) }
`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourcePostsToFinalizeEndpoint([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got != tc.want {
				t.Errorf("sourcePostsToFinalizeEndpoint = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFinalizeCallSiteIsUnique is the CI half of spec D4's structural
// control; the other half is that loginclient.finalize is unexported, so
// the only way to reach it from outside the package is through one of the
// package's own sufficiency decisions — CompleteIfSufficient or (#867,
// added after this comment was first written) CompleteAfterFactor.
// finalize additionally requires a `sufficient` witness parameter that
// only those two functions construct (see
// internal/modules/iam/loginclient/sufficiency.go and
// TestSufficientWitnessConstructionIsPinned below, which is this same
// kind of control applied one level up, to witness construction rather
// than to the wire POST itself).
//
// Unexporting alone does not stop someone writing a fresh POST to the same
// endpoint in a handler, and that is the dangerous shape: the spike (§2,
// docs/superpowers/spikes/2026-08-16-zitadel-login-client.md) proved
// Zitadel returns HTTP 200 and a valid authorization code for a
// password-only session even under a forceMfa policy. A second call site
// would therefore work perfectly — while silently skipping the factor
// every user was supposed to present. Nothing observable would be wrong,
// which is exactly why the control has to be mechanical.
//
// Two boundaries this does NOT cover, stated so nobody reads it as more
// than it is:
//
//   - _test.go files are excluded. Test code does not ship, and a handler
//     test legitimately stands up a fake Zitadel that receives this exact
//     POST — flagging those would block Task 4 while protecting nothing.
//
//   - The walk roots at backend/, so this proves the finalize call is
//     unique WITHIN backend/ — not within the repository. A Next.js
//     route handler in apps/shell could POST the endpoint directly with
//     the login client PAT and this test would never see it. Nothing in
//     the frontend does today; closing that properly means keeping the
//     PAT out of the frontend's reach, which is a deployment/config
//     control, not a Go arch test.
//
//     There IS one known call site outside the walk, and it is accepted
//     rather than overlooked: scripts/lib/zitadel.mjs's
//     `passwordLoginIDToken` POSTs the same endpoint with the
//     login-client PAT to mint an ID token for LOCAL DEV VERIFICATION.
//     It is not production code, ships in no image, and is not reachable
//     from a request — but it is a real second place the finalize call
//     is spelled out, so this test's guarantee must be read as "exactly
//     one call site in backend/", never as "nothing outside
//     CompleteIfSufficient finalises an auth request anywhere in this
//     repo". Extending the walk to .mjs would flag that script on its
//     first run and teach the next person to add an allowlist entry,
//     which is a worse control than stating the boundary plainly here.
func TestFinalizeCallSiteIsUnique(t *testing.T) {
	root := "../.."
	var callSites []string
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
		"the OIDC finalize call must stay behind a sufficiency decision (CompleteIfSufficient or CompleteAfterFactor, spec D4) — "+
			"Zitadel does not enforce forceMfa for a login client")
}

// sufficientWitnessTypeName is the unexported type
// (internal/modules/iam/loginclient/sufficiency.go) finalize requires a
// parameter of, so that constructing one is a precondition the compiler
// enforces rather than a convention. See that file's own doc comment on
// `sufficient` for why the type alone is not a complete control: Go
// permits the zero-value composite literal `sufficient{}` from anywhere
// in the defining package, so nothing stops a copy-paste of
// `c.finalize(ctx, authRequestID, s, sufficient{})` into a brand-new,
// check-free function — that compiles, and TestFinalizeCallSiteIsUnique
// above does not catch it, because it matches POSTs to the finalize
// endpoint, not constructions of this witness type. This is that missing
// half: the same file-grained pinning TestFinalizeCallSiteIsUnique does
// for the wire call, applied to `sufficient{` construction instead.
const sufficientWitnessTypeName = "sufficient"

// sufficientWitnessCallSite is the one file permitted to construct a
// `sufficient` value: CompleteIfSufficient and CompleteAfterFactor both
// live here, and both must run their own classification
// (classifyEnrolledMethods, loginPolicy, SessionFactors as applicable)
// before doing so. Like finalizeCallSite, this is deliberately not a map
// — the whole control is that the set has exactly one element.
const sufficientWitnessCallSite = "internal/modules/iam/loginclient/sufficiency.go"

// sourceConstructsSufficientWitness reports whether src contains a
// composite literal of the unexported `sufficient` type — `sufficient{}`
// or `sufficient{...}`, however many fields it later grows. It resolves
// the type by NAME only, the same one-level-of-honesty
// fileStringConsts/exprMentions above operate at: a value constructed via
// a local alias (`type s = sufficient; s{}`) would evade this, but that
// is a far more conspicuous rewrite than a straight copy-paste and is not
// a shape this codebase's Go style uses anywhere today.
func sourceConstructsSufficientWitness(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if ident, ok := lit.Type.(*ast.Ident); ok && ident.Name == sufficientWitnessTypeName {
			found = true
		}
		return true
	})
	return found, nil
}

// sufficientWitnessConstructingFuncNames reports the name of every
// top-level function in src whose body constructs a `sufficient{...}`
// composite literal — the FUNCTION-granular half of
// TestSufficientWitnessConstructionIsPinned (#867 Task 4 fix round 2,
// Finding M6). sourceConstructsSufficientWitness above, and the
// file-level walk that uses it, only prove WHICH FILE constructs a
// witness — sufficientWitnessCallSite names sufficiency.go as a WHOLE
// file, so a brand-new, check-free function added INSIDE that same file
// would construct `sufficient{}` and pass that check silently, the
// identical gap TestFinalizeCallSiteIsUnique's file-level pinning has
// for the wire call one level down. This walks each FuncDecl's body
// independently (rather than the whole file in one ast.Inspect pass, the
// way sourceConstructsSufficientWitness does) so a construction can be
// attributed to the SPECIFIC enclosing function, and
// TestSufficientWitnessConstructionIsPinned can assert the exact set of
// functions rather than merely the file.
func sufficientWitnessConstructingFuncNames(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		constructs := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if ident, ok := lit.Type.(*ast.Ident); ok && ident.Name == sufficientWitnessTypeName {
				constructs = true
			}
			return true
		})
		if constructs {
			seen[fn.Name.Name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// TestSufficientWitnessConstructionIsPinned is the CI half of the control
// finalize's `sufficient` parameter starts (see that type's doc comment,
// sufficiency.go): the parameter stops an accidental omission at compile
// time, but nothing in the language stops a deliberate or copy-pasted
// `sufficient{}` from being handed to finalize without ever calling
// classifyEnrolledMethods/loginPolicy/SessionFactors first. This test is
// the mechanical backstop for exactly that gap — the same shape as
// TestFinalizeCallSiteIsUnique, aimed one level up.
//
// Same two boundaries as TestFinalizeCallSiteIsUnique, for the same
// reasons: _test.go files are excluded (client_test.go's two direct unit
// tests of finalize legitimately construct `sufficient{}` to test its
// wire behavior in isolation — see TestFinalizeReturnsCallbackURL and
// TestFinalizeEscapesAdversarialAuthRequestID), and the walk roots at
// backend/, so this proves uniqueness within backend/ only.
func TestSufficientWitnessConstructionIsPinned(t *testing.T) {
	root := "../.."
	var callSites []string
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
		// archtest's own source necessarily spells the type name out (this
		// check, its constants, its doc comments). Excluding the package
		// avoids the self-match rather than allowlisting it.
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		constructs, err := sourceConstructsSufficientWitness(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if constructs {
			callSites = append(callSites, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	// FILE-granular: no file other than sufficientWitnessCallSite may
	// construct a `sufficient{}` witness at all. This alone does NOT
	// prove the claim its message used to make ("...inside
	// sufficiency.go's OWN EVALUATION FUNCTIONS") — a check-free function
	// added INSIDE sufficiency.go would satisfy this exact assertion.
	// See the FUNCTION-granular assertion immediately below, which is
	// what actually proves that stronger claim (#867 Task 4 fix round 2,
	// Finding M6: the message must not claim more than the test enforces).
	require.ElementsMatch(t, []string{sufficientWitnessCallSite}, callSites,
		"a sufficient{} witness must only be constructed inside sufficiency.go (#867 fix round 2, Finding B) — "+
			"finalize's witness parameter alone does not stop a copy-pasted, check-free construction of one")

	// FUNCTION-granular: within that one permitted file, a witness may
	// ONLY be constructed inside CompleteIfSufficient or
	// CompleteAfterFactor — the two functions that actually run
	// classifyEnrolledMethods/loginPolicy/SessionFactors before
	// constructing one. Without this second assertion, a THIRD,
	// check-free function added to sufficiency.go (e.g. a future
	// convenience wrapper that skips the checks) would pass the
	// file-granular assertion above silently — exactly the gap Finding
	// M6 identified.
	sufficiencySrc, err := os.ReadFile(filepath.Join(root, sufficientWitnessCallSite))
	require.NoError(t, err)
	funcs, err := sufficientWitnessConstructingFuncNames(sufficiencySrc)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"CompleteIfSufficient", "CompleteAfterFactor"}, funcs,
		"a sufficient{} witness must only be constructed inside sufficiency.go's CompleteIfSufficient or "+
			"CompleteAfterFactor (#867 Task 4 fix round 2, Finding M6) — a new function added anywhere else "+
			"in this file, check-free, must not be able to construct one undetected")
}

// TestSufficiencyNeverReferencesInstanceLoginPolicyForDisplay is the CI
// half of spec D3 (#913,
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md):
// loginclient.InstanceLoginPolicyForDisplay reads the login policy
// UNSCOPED — no x-zitadel-orgid header, resolved against the login-client
// PAT's own resource owner rather than any particular org. It exists for
// exactly one legitimate caller, loginui.go's AuthRequest handler, which
// has no authenticated user yet and therefore no org to scope a read to.
// The enforcer, sufficiency.go's CompleteIfSufficient / CompleteAfterFactor,
// must always resolve the policy against the AUTHENTICATING USER'S OWN org
// via LoginPolicyForOrg (spec D1/D2) — that is the entire fix #913 makes.
// A future contributor who reaches for InstanceLoginPolicyForDisplay
// inside sufficiency.go instead (a plausible copy-paste from client.go,
// or "simpler, no org id to plumb through") would silently reintroduce
// the exact bypass this change closes: a user in an org that forces MFA
// judged by whatever org the login-client PAT happens to belong to. This
// is the same "fail CI, not review" shape as TestFinalizeCallSiteIsUnique
// and TestSufficientWitnessConstructionIsPinned immediately above, applied
// to this one method name instead of the finalize POST or the sufficient
// witness.
//
// sourceReferencesMethod (used above for WithAdmin) is reused rather than
// duplicated: InstanceLoginPolicyForDisplay is a method on *Client, so
// every way of reaching it — an ordinary call
// (c.InstanceLoginPolicyForDisplay(ctx)) or a bare method value
// (f := c.InstanceLoginPolicyForDisplay) — is always spelled through a
// selector expression on some Client value. There is no free-function
// alias or embedding shortcut in this codebase's Go style that would let
// a reference evade sourceReferencesMethod's ast.SelectorExpr match, the
// same property TestSourceReferencesWithAdminCatchesMethodValues already
// proves for that helper against the method-value evasion specifically.
//
// This test parses only sufficiency.go (sufficientWitnessCallSite), not
// the whole loginclient package: InstanceLoginPolicyForDisplay's own
// definition and doc comment necessarily spell the name out in client.go,
// and client_test.go legitimately exercises it directly
// (TestInstanceLoginPolicyForDisplaySendsNoOrgHeader) — a package-wide
// walk would have to allowlist both, which is a weaker control than
// naming the one file that must never reference it.
//
// Being scoped to sufficiency.go names the one edit shape that would slip
// past this test: extracting the policy read into a separate helper file
// that CompleteIfSufficient calls would move the reference to
// InstanceLoginPolicyForDisplay outside sufficiency.go and this test would
// not see it. (The finalize decision itself cannot escape that way,
// because finalize needs a sufficient{} witness, and
// TestSufficientWitnessConstructionIsPinned above pins that construction
// to this file repo-wide regardless of which file reads the policy.)
func TestSufficiencyNeverReferencesInstanceLoginPolicyForDisplay(t *testing.T) {
	root := "../.."
	src, err := os.ReadFile(filepath.Join(root, sufficientWitnessCallSite))
	require.NoError(t, err)
	found, err := sourceReferencesMethod(src, "InstanceLoginPolicyForDisplay")
	require.NoError(t, err)
	require.False(t, found,
		"internal/modules/iam/loginclient/sufficiency.go references InstanceLoginPolicyForDisplay (#913) — "+
			"that read is unscoped (no x-zitadel-orgid) and resolves against the login-client PAT's own "+
			"resource owner, not the authenticating user's org; it exists only for loginui.go's pre-credential "+
			"AuthRequest display hint, which has no user to scope to. The enforcer must scope its policy read "+
			"to the authenticating user's org instead: use LoginPolicyForOrg(ctx, subject.OrgID), the id "+
			"classifyEnrolledMethods already returns alongside its two booleans (design spec D1/D2)")
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
