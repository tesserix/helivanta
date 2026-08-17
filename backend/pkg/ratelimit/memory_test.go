package ratelimit_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/ratelimit"
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

// TestAKeyAddedToAFullStoreIsStillLimited is a correctness test, and the
// bound test above does not imply it.
//
// Eviction runs immediately after a new key is inserted, so the new key
// is the most recently used at that instant. An implementation that
// evicted the most recently used would therefore evict the key it just
// created — leaving the store bounded, the hot keys intact, and every
// assertion above green, while **the new caller is never limited at
// all**: their bucket is recreated with a full burst on every request
// and discarded before the next one.
//
// That is a limiter bypass for every caller not already resident once
// the store is full, which on a busy system is most of them. It is
// invisible to a size assertion, which is why this test exists
// separately.
func TestAKeyAddedToAFullStoreIsStillLimited(t *testing.T) {
	const max = 10
	tiny := ratelimit.Rule{Rate: 120, Burst: 2, Per: time.Minute}
	l := ratelimit.NewMemory(max)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	// Fill the store past its bound with other callers.
	for i := range max * 3 {
		l.Allow(fmt.Sprintf("subject:filler-%d", i), tiny, now)
	}

	// A new caller arrives at a full store and spends its burst. The
	// third request must be refused: a bucket that survives between
	// requests is the only thing that can refuse it.
	newcomer := "subject:newcomer"
	require.True(t, l.Allow(newcomer, tiny, now).Allowed)
	require.True(t, l.Allow(newcomer, tiny, now).Allowed)
	require.False(t, l.Allow(newcomer, tiny, now).Allowed,
		"a caller arriving at a full store is never limited: its bucket is being evicted between requests and recreated with a full burst")
}

// TestAHotKeySurvivesEviction pins the other half of the LRU: a key that
// keeps being used must not age out like a cold one.
//
// Without MoveToFront on a cache hit, a frequently-hit key is exactly as
// evictable as one touched once — so the caller generating the most load
// is among the first to have their bucket reset, which is the opposite of
// what a limiter should do under pressure.
func TestAnExhaustedHotKeyStaysExhaustedUnderChurn(t *testing.T) {
	const max = 10
	tiny := ratelimit.Rule{Rate: 120, Burst: 2, Per: time.Minute}
	l := ratelimit.NewMemory(max)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	// Exhaust the hot key first, so any reset is immediately visible as
	// a request that should have been refused being allowed.
	hot := "subject:hot"
	require.True(t, l.Allow(hot, tiny, now).Allowed)
	require.True(t, l.Allow(hot, tiny, now).Allowed)
	require.False(t, l.Allow(hot, tiny, now).Allowed, "precondition: the hot bucket is empty")

	// Churn far past the bound while the hot key keeps requesting. Time
	// never advances, so no token can legitimately refill: every later
	// request must still be refused.
	for i := range max * 5 {
		l.Allow(fmt.Sprintf("subject:cold-%d", i), tiny, now)
		require.False(t, l.Allow(hot, tiny, now).Allowed,
			"the hot key was allowed again at churn step %d with no time elapsed: its bucket was evicted and recreated with a full burst, so the caller generating the most load is the one the limiter stops limiting", i)
	}
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
