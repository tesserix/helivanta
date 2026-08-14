package ratelimit_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/ratelimit"
)

var defaultRule = ratelimit.Rule{Rate: 120, Burst: 20, Per: time.Minute}

// TestBucketAllowsBurstThenRefillsAtRate pins both parameters of the
// bucket. A rate-only implementation passes half of this and throttles a
// clinical screen on load, because a dashboard fires several requests at
// once; a burst-only implementation passes the other half and never
// throttles anything after the first second.
//
// The clock is injected precisely so this can assert refill exactly
// rather than sleeping. A rate-limiter test that sleeps is a flaky test
// waiting to happen, and a flaky test in a capacity control gets
// disabled rather than fixed.
func TestBucketAllowsBurstThenRefillsAtRate(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	// Burst is 20: the first 20 go through with no time passing at all.
	for i := range 20 {
		d := l.Allow("subject:alice", defaultRule, now)
		require.True(t, d.Allowed, "request %d of the burst must be allowed", i+1)
	}

	// The 21st has no token and no time has passed.
	d := l.Allow("subject:alice", defaultRule, now)
	require.False(t, d.Allowed, "burst is 20; the 21st request in the same instant must be refused")
	require.Positive(t, d.RetryAfter, "a refusal must tell the client when to come back")

	// 120/min is one token every 500ms. Advance exactly that.
	d = l.Allow("subject:alice", defaultRule, now.Add(500*time.Millisecond))
	require.True(t, d.Allowed, "at 120/min one token refills every 500ms")

	// And immediately after, empty again.
	d = l.Allow("subject:alice", defaultRule, now.Add(500*time.Millisecond))
	require.False(t, d.Allowed, "the refilled token was just consumed")
}

// TestBucketsAreIndependentPerKey is the whole point of per-tenant
// limiting: one hospital exhausting its budget must not refuse another's
// requests.
func TestBucketsAreIndependentPerKey(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	for range 20 {
		require.True(t, l.Allow("tenant:a", defaultRule, now).Allowed)
	}
	require.False(t, l.Allow("tenant:a", defaultRule, now).Allowed)

	require.True(t, l.Allow("tenant:b", defaultRule, now).Allowed,
		"tenant b must be unaffected by tenant a exhausting its bucket")
}

// TestDecisionReportsRemainingAndReset covers what the client sees. The
// mobile apps in the backlog are offline-first and back off on these
// headers; wrong numbers are worse than none, because a client trusts them.
func TestDecisionReportsRemainingAndReset(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	d := l.Allow("subject:alice", defaultRule, now)
	require.Equal(t, 120, d.Limit)
	require.Equal(t, 19, d.Remaining, "one of the 20 burst tokens was just consumed")

	for range 19 {
		l.Allow("subject:alice", defaultRule, now)
	}
	d = l.Allow("subject:alice", defaultRule, now)
	require.False(t, d.Allowed)
	require.Equal(t, 0, d.Remaining)
	require.Equal(t, 500*time.Millisecond, d.RetryAfter,
		"the next token is 500ms away at 120/min")
}

// TestKeySpaceIsBounded: keys are tenant and subject ids, so an
// unbounded map is a memory leak reachable by anyone who can obtain
// tokens for many subjects. Same bound, same reason, as the revocation
// cache in #781.
func TestKeySpaceIsBounded(t *testing.T) {
	const max = 100
	l := ratelimit.NewMemory(max)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	for i := range max * 5 {
		l.Allow(fmt.Sprintf("subject:%d", i), defaultRule, now)
	}
	require.LessOrEqual(t, l.Len(), max, "the bucket store must stay bounded")
}

// TestMemoryIsRaceFree exercises the bucket map under concurrent access.
// The map is mutated on every request, so this must be proven under
// -race, not merely assumed from the mutex being present.
func TestMemoryIsRaceFree(t *testing.T) {
	l := ratelimit.NewMemory(50)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Allow(fmt.Sprintf("subject:%d", i%20), defaultRule, now.Add(time.Duration(i)*time.Millisecond))
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, l.Len(), 50)
}
