package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductionUsesCurrentIAMAuthorityForBothAdmissionGates(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	require.NoError(t, err)
	var authority string
	ast.Inspect(source, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			return true
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok || !currentAuthorityCall(call, "iam", "NewCurrentAuthorization") {
			return true
		}
		require.Empty(t, authority, "production must construct a single shared IAM admission authority")
		name, ok := assignment.Lhs[0].(*ast.Ident)
		require.True(t, ok)
		authority = name.Name
		return true
	})
	require.NotEmpty(t, authority, "production must bound graph permissions by current IAM membership")
	var permissionGates, membershipGates int
	ast.Inspect(source, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		index := -1
		if currentAuthorityCall(call, "bootstrap", "V1Chain") {
			index = 4
			permissionGates++
		}
		if currentAuthorityCall(call, "platform", "NewRouter") {
			index = 1
			membershipGates++
		}
		if index >= 0 {
			require.Greater(t, len(call.Args), index)
			name, ok := call.Args[index].(*ast.Ident)
			require.True(t, ok, "the gate must use the shared current IAM authority")
			require.Equal(t, authority, name.Name)
		}
		return true
	})
	require.Equal(t, 1, permissionGates)
	require.Equal(t, 1, membershipGates)
}

func currentAuthorityCall(call *ast.CallExpr, pkg, name string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == pkg
}
