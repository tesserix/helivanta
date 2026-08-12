// Package archtest enforces the architecture rules that lint alone
// cannot express precisely (same-module imports allowed, cross-module
// denied) and the registry-level invariants (unique migration IDs,
// subject/consumer naming).
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
	return []platform.Module{iam.New(), reference.New(), medicore.New(), pharmacy.New(), lab.New()}
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

func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports, Tests: true}, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		from := moduleOf(p.PkgPath)
		for imp := range p.Imports {
			to := moduleOf(imp)
			if to != "" && to != from {
				t.Errorf("module %q imports module %q (%s -> %s): cross-module data flows only via events", from, to, p.PkgPath, imp)
			}
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

func TestPublishedSubjectConstants(t *testing.T) {
	for name, s := range map[string]string{
		"reference.SubjectPinged":          reference.SubjectPinged,
		"medicore.SubjectVisitCreated":     medicore.SubjectVisitCreated,
		"pharmacy.SubjectDispenseRecorded": pharmacy.SubjectDispenseRecorded,
		"lab.SubjectResultReady":           lab.SubjectResultReady,
	} {
		if !subjectRe.MatchString(s) {
			t.Errorf("%s = %q must match %s", name, s, subjectRe)
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
	for _, m := range bootstrap.Modules() {
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

// TestEveryDeclaredPermissionIsGranted catches a module that guards a
// route with a permission it never declared in Permissions() — the route
// would be permanently unreachable for every non-admin role.
func TestEveryDeclaredPermissionIsGranted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, m := range allModules() {
		declaredByGrants := map[authz.Permission]bool{authz.Public: true}
		for _, g := range m.Permissions() {
			declaredByGrants[g.Permission] = true
		}
		e := gin.New()
		r := platform.NewRouter(e.Group("/v1"))
		m.Routes(r, platform.Deps{})
		for _, p := range r.Declared() {
			if !declaredByGrants[p] {
				t.Errorf("module %q guards a route with %q but does not declare it in Permissions()", m.Name(), p)
			}
		}
	}
}

// withAdminAllowlist is exactly the files permitted to call
// tenantdb.DB.WithAdmin — the reconciler (its one legitimate whole-system
// caller) and pkg/tenantdb itself (the method's own definition and its
// direct test). Extending this list is a real RLS-bypass decision, not a
// convenience; it belongs in code review, not a casual addition here.
var withAdminAllowlist = map[string]bool{
	"internal/platform/reconcile.go": true,
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
