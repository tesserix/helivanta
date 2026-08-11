// Package archtest enforces the architecture rules that lint alone
// cannot express precisely (same-module imports allowed, cross-module
// denied) and the registry-level invariants (unique migration IDs,
// subject/consumer naming).
package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/tools/go/packages"

	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/modules/reference"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
)

const modulesPrefix = "github.com/tesserix/hms/internal/modules/"

// allModules must list every registered module. cmd/api/main.go is the
// runtime source of truth; keep them in sync (the generator prints a
// reminder).
func allModules() []platform.Module {
	return []platform.Module{reference.New(), medicore.New(), pharmacy.New(), lab.New()}
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

// moduleLiteralRe finds the `[]platform.Module{...}` literal in main.go's
// registry loop.
var moduleLiteralRe = regexp.MustCompile(`\[\]platform\.Module\{([^}]*)\}`)

// moduleCtorRe pulls out `<pkg>.New()` constructor calls from inside the
// literal.
var moduleCtorRe = regexp.MustCompile(`(\w+)\.New\(\)`)

// TestMainRegistersExactlyAllModules guards registry parity: every module
// constructor registered in cmd/api/main.go's `[]platform.Module{...}`
// literal must also appear in allModules(), and vice versa. Without this,
// a module added to one but forgotten in the other silently skips either
// production registration or the arch/coverage checks that walk
// allModules().
func TestMainRegistersExactlyAllModules(t *testing.T) {
	src, err := os.ReadFile("../../cmd/api/main.go")
	if err != nil {
		t.Fatalf("read cmd/api/main.go: %v", err)
	}

	lit := moduleLiteralRe.FindSubmatch(src)
	if lit == nil {
		t.Fatalf("could not find []platform.Module{...} literal in cmd/api/main.go")
	}

	registered := map[string]bool{}
	for _, m := range moduleCtorRe.FindAllSubmatch(lit[1], -1) {
		registered[string(m[1])] = true
	}
	if len(registered) == 0 {
		t.Fatalf("found []platform.Module{...} literal but no <pkg>.New() constructors inside it")
	}

	expected := map[string]bool{}
	for _, mod := range allModules() {
		expected[mod.Name()] = true
	}

	for name := range registered {
		if !expected[name] {
			t.Errorf("cmd/api/main.go registers module %q but allModules() in arch_test.go does not include it", name)
		}
	}
	for name := range expected {
		if !registered[name] {
			t.Errorf("allModules() in arch_test.go includes module %q but cmd/api/main.go does not register it", name)
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
		// archtest's own source necessarily mentions the ".WithAdmin("
		// pattern it scans for; excluding the package avoids that
		// self-match rather than allowlisting it, which would otherwise
		// read as "archtest may call WithAdmin".
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
		if strings.Contains(string(src), ".WithAdmin(") {
			t.Errorf("%s calls .WithAdmin(), which bypasses RLS entirely and sees every "+
				"tenant's rows. Request-path code must use WithTenant. If you genuinely need "+
				"a cross-tenant read, bring it to review — don't add this file to "+
				"withAdminAllowlist in arch_test.go on your own.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}
