package workforce

import "testing"

// LowerClashLimit shrinks the freeze clash bound for one test so the refusal is reachable with small fixtures.
func LowerClashLimit(t *testing.T, n int) {
	prev := maxAcknowledgedClashes
	maxAcknowledgedClashes = n
	t.Cleanup(func() { maxAcknowledgedClashes = prev })
}
