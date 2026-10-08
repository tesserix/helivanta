package testinfra

import (
	"strings"
	"testing"
)

// sameNameTB stands in for a second `-count` iteration of one test: a
// different *testing.T that reports the same Name().
type sameNameTB struct {
	testing.TB
	name string
}

func (s sameNameTB) Name() string { return s.name }

func TestIsolationKeyIsStableWithinATest(t *testing.T) {
	if a, b := IsolationKey(t), IsolationKey(t); a != b {
		t.Fatalf("two calls in one test gave %q and %q; buses or clients meant to share state would not", a, b)
	}
}

// The #929 property: two iterations with the same name get different keys.
func TestIsolationKeyDiffersAcrossIterationsWithTheSameName(t *testing.T) {
	first := IsolationKey(&sameNameTB{TB: t, name: "TestRepeated"})
	second := IsolationKey(&sameNameTB{TB: t, name: "TestRepeated"})
	if first == second {
		t.Fatalf("two iterations of TestRepeated both got %q; the second would read the first's state", first)
	}
	for _, k := range []string{first, second} {
		if !strings.HasPrefix(k, "TestRepeated_i") {
			t.Fatalf("key %q does not start with the test name, which keeps it readable in NATS and OpenFGA", k)
		}
	}
}
