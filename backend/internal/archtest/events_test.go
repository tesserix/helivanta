package archtest

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/tesserix/hms/internal/platform"
)

// TestEveryConsumedSubjectIsPublished closes the quiet half of #827. A
// publisher bumping .v1 to .v2, or deleting an event, leaves its
// consumers subscribed to a subject nobody sends. There is no wrong
// data to notice — pharmacy and lab intake simply stops.
func TestEveryConsumedSubjectIsPublished(t *testing.T) {
	published := map[string]string{} // subject -> publishing module
	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			published[s] = m.Name()
		}
	}

	for _, m := range allModules() {
		for _, c := range m.Consumers(platform.Deps{}) {
			_, ok := published[c.Subject]
			require.True(t, ok,
				"consumer %q in module %q subscribes to %q, which no module publishes — it will receive nothing, silently",
				c.Name, m.Name(), c.Subject)
		}
		for _, b := range m.Broadcasts(platform.Deps{}) {
			_, ok := published[b.Subject]
			require.True(t, ok,
				"broadcast in module %q subscribes to %q, which no module publishes",
				m.Name(), b.Subject)
		}
	}
}

// TestNoSubjectIsPublishedByTwoModules keeps ownership unambiguous: a
// subject names one publisher's contract, and two modules publishing it
// means a consumer cannot know whose payload shape it is getting.
func TestNoSubjectIsPublishedByTwoModules(t *testing.T) {
	owner := map[string]string{}
	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			if prev, dup := owner[s]; dup {
				t.Errorf("subject %q is published by both %q and %q: one subject, one publisher, one contract", s, prev, m.Name())
				continue
			}
			owner[s] = m.Name()
		}
	}
}

// TestPublishesUsesContractConstants stops Publishes() drifting from the
// contract. A bare string here would satisfy the two checks above while
// no longer matching what the publisher actually sends.
//
// What this proves and does not prove: it compares the *value* of each
// Publishes() entry against the set of contract constants, not whether
// the entry is a reference to one. A bare string literal equal to a
// contract constant's value passes exactly like the constant itself
// would — see Task 4 Step 5.3 in the implementation plan, which proves
// this by mutation rather than asserting it in prose.
func TestPublishesUsesContractConstants(t *testing.T) {
	contractSubjects := map[string]bool{}
	for _, p := range loadContractPackages(t) {
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, v := range vs.Values {
						lit, ok := v.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						contractSubjects[strings.Trim(lit.Value, `"`)] = true
					}
				}
			}
		}
	}
	require.NotEmpty(t, contractSubjects, "no contract constants found — this test would pass vacuously")

	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			require.True(t, contractSubjects[s],
				"module %q publishes %q, which is not a constant in any contract package — declare it in %s/contract so publisher and consumers share one definition",
				m.Name(), s, m.Name())
		}
	}
}

// TestConsumersUnmarshalIntoContractTypes is the standing guard against
// a local payload copy coming back. Tasks 1 and 3 deleted the copies;
// nothing yet stops a new consumer declaring its own struct and
// unmarshalling into that, which is exactly the state #827 fixed.
//
// This resolves the TYPE of the second argument to
// json.Unmarshal(evt.Data, &d) using go/packages type information —
// which the type checker has already computed. That is why it is
// acceptable where walking bus.Publish call sites was not: that check
// had to resolve a constant VALUE through arbitrary indirection and
// fails opaquely. This fails loudly: a type it cannot resolve is a
// failure, not a skip.
func TestConsumersUnmarshalIntoContractTypes(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	checked := 0
	for _, p := range pkgs {
		// Contract packages declare the payload types; they do not consume
		// events, so there is nothing here to check and matching one would
		// be circular.
		//
		// Test files are not inspected at all, and that is deliberate
		// rather than incidental: packages.Config above sets no
		// Tests: true, so go/packages never loads them. A test may
		// legitimately unmarshal into an ad-hoc struct to assert on the
		// wire shape itself — pinning the JSON a publisher emits is a
		// reasonable thing for a test to do, and forcing it through a
		// contract type would defeat the point. The defect #827 fixes is
		// a *production* consumer drifting from its publisher.
		//
		// An earlier version also filtered on a ".test" suffix. That was
		// dead: without Tests: true, go/packages never produces such a
		// package, so the branch read as a deliberate exclusion while
		// excluding nothing.
		if strings.HasSuffix(p.PkgPath, "/contract") {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Unmarshal" {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "json" {
					return true
				}
				// Only calls unmarshalling an event payload.
				argSel, ok := call.Args[0].(*ast.SelectorExpr)
				if !ok || argSel.Sel.Name != "Data" {
					return true
				}

				checked++
				typ := p.TypesInfo.TypeOf(call.Args[1])
				require.NotNil(t, typ, "%s: cannot resolve the type unmarshalled from evt.Data", p.PkgPath)

				name := typ.String() // e.g. *github.com/.../medicore/contract.VisitCreatedData
				require.Contains(t, name, "/contract.",
					"%s unmarshals an event payload into %s, which is not a contract type — declare the payload in the publishing module's contract package so a renamed field breaks this build instead of writing a zero value (#827)",
					p.PkgPath, name)
				return true
			})
		}
	}
	require.Positive(t, checked, "no json.Unmarshal(evt.Data, …) call sites found — this test would pass vacuously")
}
