package pharmacy

import (
	"testing"
	"time"
)

// SetClock pins the module clock for a test.
func SetClock(t testing.TB, now func() time.Time) {
	t.Helper()
	prev := clock
	clock = now
	t.Cleanup(func() { clock = prev })
}
