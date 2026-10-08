package testinfra

import (
	"fmt"
	"sync"
	"testing"
)

// IsolationKey returns the name that isolates this test's state on a
// container shared by the whole binary: its NATS namespace, its OpenFGA
// store, and anything else keyed by name (#929).
//
// It is NOT t.Name(). Containers are shared per test binary (sync.Once,
// deliberately, for speed), so a key must be unique across every test that
// can run in one process. t.Name() is unique within one iteration, but
// `go test -count=N` runs every test N times in ONE process, against the
// same containers, under the same names. Iteration 2 then read iteration
// 1's events and store tuples, and failed in ways indistinguishable from a
// real regression.
//
// The key is t.Name() plus a sequence number that is unique in the process.
// It is stable for one *testing.T, so two buses or clients in the same test
// that are meant to share state still do. A new iteration is a new
// *testing.T, so it gets a new key. A new process starts new containers
// (the sync.Once is per process), so a sequence that restarts at 1 there
// cannot collide.
//
// internal/archtest's TestTestNameIsNeverAnIsolationKey forbids t.Name()
// anywhere else, so the rule does not depend on anyone remembering it.
func IsolationKey(t testing.TB) string {
	t.Helper()
	isolationMu.Lock()
	defer isolationMu.Unlock()
	if k, ok := isolationKeys[t]; ok {
		return k
	}
	isolationSeq++
	k := fmt.Sprintf("%s_i%d", t.Name(), isolationSeq)
	isolationKeys[t] = k
	t.Cleanup(func() {
		isolationMu.Lock()
		delete(isolationKeys, t)
		isolationMu.Unlock()
	})
	return k
}

var (
	isolationMu   sync.Mutex
	isolationSeq  uint64
	isolationKeys = map[testing.TB]string{}
)
