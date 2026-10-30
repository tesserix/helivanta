package testinfra

import (
	"regexp"
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

func TestIsolationKeyFitsSharedServiceIdentifierLimits(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_]{3,64}$`)
	for _, name := range []string{strings.Repeat("LongTestName", 20), "Test/subtest with spaces/スタッフ", "", "TestIdentityCaseDatabasePreservesPrivateHistoryAndDraftState"} {
		scope := &sameNameTB{TB: t, name: name}
		first := IsolationKey(scope)
		if !pattern.MatchString(first) {
			t.Fatalf("key %q is not a bounded shared-service identifier", first)
		}
		if again := IsolationKey(scope); first != again {
			t.Fatalf("one test changed key from %q to %q", first, again)
		}
		if second := IsolationKey(&sameNameTB{TB: t, name: name}); first == second {
			t.Fatalf("different test iterations share %q", first)
		}
	}
}
