package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginExchangeUsesSharedCurrentIdentityReader(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "cmd", "api", "main.go"), nil, 0)
	require.NoError(t, err)
	var registryIdentity, loginIdentity *ast.Ident
	registryCalls, loginCalls := 0, 0
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "bootstrap" && selector.Sel.Name == "NewRegistry" {
			registryCalls++
			require.Len(t, call.Args, 2)
			registryIdentity, _ = call.Args[1].(*ast.Ident)
		}
		if pkg.Name == "iam" && selector.Sel.Name == "NewLoginHandlers" {
			loginCalls++
			require.Len(t, call.Args, 1)
			deps, ok := call.Args[0].(*ast.CompositeLit)
			require.True(t, ok)
			for _, element := range deps.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if ok && name.Name == "CurrentIdentity" {
					loginIdentity, _ = field.Value.(*ast.Ident)
				}
			}
		}
		return true
	})
	require.Equal(t, 1, registryCalls)
	require.Equal(t, 1, loginCalls)
	require.NotNil(t, registryIdentity)
	require.NotNil(t, loginIdentity, "login must not omit the credential-admission dependency")
	require.Equal(t, registryIdentity.Name, loginIdentity.Name, "login exchange and protected requests must share the configured identity-owner reader")
}
