package tax_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tax arithmetic is integer-only; a float anywhere in it is a rounding bug
// waiting for a large enough invoice.
func TestNoFloats(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		require.NoError(t, err)
		checked++
		ast.Inspect(parsed, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (id.Name == "float32" || id.Name == "float64") {
				t.Errorf("%s uses %s", f, id.Name)
			}
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.FLOAT {
				t.Errorf("%s has float literal %s", f, lit.Value)
			}
			return true
		})
	}
	require.Positive(t, checked)
}
