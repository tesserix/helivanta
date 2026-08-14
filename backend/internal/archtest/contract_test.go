package archtest

import (
	"go/ast"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// contractAllowedImports is everything a contract package may import.
//
// Deliberately tiny. A contract describes a shape; it does not do
// anything. Anything beyond these two either implies behaviour (gorm,
// gin, pkg/events) or re-opens module isolation transitively (any
// internal/ path), because contract packages are importable by every
// module.
var contractAllowedImports = map[string]string{
	"time":                   "timestamps in payloads",
	"github.com/google/uuid": "identifiers in payloads",
}

// keysOfStringMap returns m's keys in sorted order, so error messages are
// deterministic across runs instead of following Go's randomized map
// iteration order.
func keysOfStringMap(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func loadContractPackages(t *testing.T) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedSyntax | packages.NeedFiles,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	var out []*packages.Package
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, "/contract") {
			out = append(out, p)
		}
	}
	require.NotEmpty(t, out, "no contract packages found — this test would pass vacuously")
	return out
}

// TestContractPackagesDeclareOnlyData is what keeps the module-isolation
// exception narrow. A func or method on a contract type is behaviour
// crossing the module boundary — exactly what the isolation rule exists
// to stop — arriving through the one door that rule leaves open.
func TestContractPackagesDeclareOnlyData(t *testing.T) {
	for _, p := range loadContractPackages(t) {
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				fn, isFunc := decl.(*ast.FuncDecl)
				if !isFunc {
					continue
				}
				t.Errorf("%s declares func %q: a contract package is data only (const, type, var). Behaviour belongs in the module, or in pkg/ if it is genuinely shared",
					p.PkgPath, fn.Name.Name)
			}
		}
	}
}

// TestContractPackagesImportAlmostNothing is the other half. A contract
// that could import its own module would make every module reachable
// from every other module transitively, since contract packages are
// importable by all of them.
func TestContractPackagesImportAlmostNothing(t *testing.T) {
	for _, p := range loadContractPackages(t) {
		for imp := range p.Imports {
			if _, ok := contractAllowedImports[imp]; ok {
				continue
			}
			t.Errorf("%s imports %q: a contract package may import only %v — anything else either implies behaviour or re-opens module isolation transitively",
				p.PkgPath, imp, keysOfStringMap(contractAllowedImports))
		}
	}
}
