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

func TestPasswordRecoveryUsesTheLoginNativeRequestAuthority(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "cmd", "api", "main.go"), nil, 0)
	require.NoError(t, err)
	var loginNative, recoveryNative, loginClient, recoveryClient string
	recoveryCalls := 0
	selectorText := func(expr ast.Expr) string {
		value, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		base, ok := value.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return base.Name + "." + value.Sel.Name
	}
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "NewLoginUIHandlers" && len(call.Args) > 0 {
			if native, ok := call.Args[0].(*ast.Ident); ok {
				loginNative = native.Name
			}
		}
		if selector.Sel.Name == "SetNativeWebClientID" && len(call.Args) == 1 {
			loginClient = selectorText(call.Args[0])
		}
		if selector.Sel.Name == "NewPasswordRecoveryHandlers" {
			recoveryCalls++
			require.Len(t, call.Args, 2)
			deps, ok := call.Args[1].(*ast.CompositeLit)
			require.True(t, ok)
			for _, element := range deps.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch name.Name {
				case "Requests":
					if native, ok := field.Value.(*ast.Ident); ok {
						recoveryNative = native.Name
					}
				case "ClientID":
					recoveryClient = selectorText(field.Value)
				}
			}
		}
		return true
	})
	require.Equal(t, 1, recoveryCalls)
	require.NotEmpty(t, loginNative)
	require.Equal(t, loginNative, recoveryNative)
	require.NotEmpty(t, loginClient)
	require.Equal(t, loginClient, recoveryClient, "recovery must validate this deployment's actual native web client")
}

func TestMFARecoveryAdmissionUsesConfiguredRecoveryOwner(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "cmd", "api", "main.go"), nil, 0)
	require.NoError(t, err)
	var admissionOwner, passwordOwner string
	loginCalls, passwordCalls := 0, 0
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "NewLoginHandlers":
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
				if !ok || name.Name != "RecoveryAdmission" {
					continue
				}
				value, ok := field.Value.(*ast.Ident)
				require.True(t, ok)
				admissionOwner = value.Name
			}
		case "NewPasswordRecoveryHandlers":
			passwordCalls++
			require.Len(t, call.Args, 2)
			owner, ok := call.Args[0].(*ast.Ident)
			require.True(t, ok)
			passwordOwner = owner.Name
		}
		return true
	})
	require.Equal(t, 1, loginCalls)
	require.Equal(t, 1, passwordCalls)
	require.NotEmpty(t, passwordOwner)
	require.Equal(t, passwordOwner, admissionOwner, "native MFA admission must use the configured recovery owner, including the explicit isolated owner fixture")
}
