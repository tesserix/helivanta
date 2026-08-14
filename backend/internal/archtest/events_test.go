package archtest

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
