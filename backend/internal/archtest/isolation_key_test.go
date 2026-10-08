package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testNameReceivers are the identifiers this repo binds a *testing.T,
// testing.TB or *testing.B to. t.Name() on any of them is what #929 forbids.
var testNameReceivers = map[string]bool{"t": true, "tb": true, "b": true}

// sourceCallsTestName reports whether src calls Name() with no arguments on a
// test-handle identifier.
func sourceCallsTestName(src []byte) (bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Name" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && testNameReceivers[id.Name] {
			found = true
		}
		return true
	})
	return found, nil
}

// TestTestNameIsNeverAnIsolationKey: t.Name() repeats across
// `go test -count=N` iterations, which run in one process against the same
// shared NATS and OpenFGA containers, so a namespace or store named by it
// leaks state from one iteration into the next (#929). Every such key comes
// from testinfra.IsolationKey(t), which is the one place allowed to call
// t.Name(). Today t.Name() has no other use in the backend; one that is
// genuinely not an isolation key can use the key too.
func TestTestNameIsNeverAnIsolationKey(t *testing.T) {
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
		if rel == "internal/testinfra/isolation.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceCallsTestName(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s calls t.Name(). It repeats across `go test -count=N` iterations that share one NATS/OpenFGA container, so as a namespace or store name it leaks one iteration's state into the next (#929). Use testinfra.IsolationKey(t).", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// The detector must fire on the shapes that caused #929, and must not fire on
// an unrelated Name() method, or the rule above proves nothing.
func TestSourceCallsTestNameDetectsTheForbiddenShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"namespace argument", `package p; func f() { NewBusInNamespace(url, t.Name()) }`, true},
		{"concatenated store name", `package p; func f() { s := "upgrade-" + tb.Name(); _ = s }`, true},
		{"benchmark handle", `package p; func f() { _ = b.Name() }`, true},
		{"a module's Name() method", `package p; func f() { _ = m.Name() }`, false},
		{"a field named Name", `package p; func f() { _ = t.Name }`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceCallsTestName([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("sourceCallsTestName = %v, want %v", got, tc.want)
			}
		})
	}
}
