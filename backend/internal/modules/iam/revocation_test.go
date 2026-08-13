package iam

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// revocationHarness boots a real Postgres with only the
// iam_credential_revocations table migrated — the revocation checker has
// no dependency on iam_roles/iam_members or on hms_tenant_visible (the
// table carries no tenant_id at all), so pulling in the rest of the iam
// module's migrations here would only be noise.
func revocationHarness(t *testing.T) (*tenantdb.DB, context.Context) {
	t.Helper()
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	migs := New().Migrations()
	revocationMig := migs[len(migs)-1]
	require.Equal(t, "0003_iam", revocationMig.ID, "precondition: the last iam migration is the revocation table")
	require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{revocationMig}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

// revokeNow is the test's wrapper around RevokeTx, which takes a tx
// because production callers publish the invalidation in the same
// transaction. Tests that only care about the watermark open their own.
func revokeNow(t *testing.T, db *tenantdb.DB, c *RevocationChecker, subject string, at time.Time, reason, actor string) {
	t.Helper()
	require.NoError(t, db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return c.RevokeTx(tx, subject, at, reason, actor)
	}))
}

func TestWatermarkOnlyMovesForward(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	late := time.Now().Add(-time.Hour)
	current := time.Now()

	revokeNow(t, db, c, "uid-nurse", current, "sign_out", "uid-nurse")
	revokeNow(t, db, c, "uid-nurse", late, "sign_out", "uid-nurse")
	c.Invalidate("uid-nurse") // drop the cached read so the assertion sees Postgres

	got, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.WithinDuration(t, current, got, time.Second,
		"a late or replayed revoke must not move the watermark backwards and resurrect a credential")
}

func TestUnknownSubjectIsNotRevoked(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	got, err := c.RevokedAfter(ctx, "uid-never-revoked")
	require.NoError(t, err)
	require.True(t, got.IsZero(), "a subject with no row has never been revoked")
}

func TestCacheServesRepeatLookupsWithoutQuerying(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	_, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	before := c.QueriesForTest()
	for range 100 {
		_, err := c.RevokedAfter(ctx, "uid-nurse")
		require.NoError(t, err)
	}
	require.Equal(t, before, c.QueriesForTest(),
		"the negative case is the hot path and must not reach Postgres on every request")
}

func TestInvalidateForcesAReread(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	_, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)

	// Write directly, bypassing this checker's own Invalidate, exactly
	// as another replica would.
	other := NewRevocationChecker(db)
	revokeNow(t, db, other, "uid-nurse", time.Now(), "sign_out", "uid-nurse")

	got, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.True(t, got.IsZero(), "precondition: the stale negative entry is still cached")

	c.Invalidate("uid-nurse")

	got, err = c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.False(t, got.IsZero(), "invalidation must force a re-read")
}

// TestCacheIsRaceFreeUnderConcurrentReadsAndInvalidations is spec T11:
// the cache is a mutable map read on every request and written by both
// the read path and (once Task 5/6 land) the broadcast handler. -race
// needs something real to examine, not just a single-goroutine test.
func TestCacheIsRaceFreeUnderConcurrentReadsAndInvalidations(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	subjects := []string{"uid-a", "uid-b", "uid-c", "uid-d"}
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(3)
		s := subjects[i%len(subjects)]
		go func() { defer wg.Done(); _, _ = c.RevokedAfter(ctx, s) }()
		go func() { defer wg.Done(); c.Invalidate(s) }()
		go func() {
			defer wg.Done()
			revokeNow(t, db, c, s, time.Now(), "sign_out", s)
		}()
	}
	wg.Wait()

	// Eviction must also be exercised: the bounded-order slice is
	// mutated on every insert of an unseen subject.
	for i := range maxRevocationCacheEntries + 100 {
		_, err := c.RevokedAfter(ctx, fmt.Sprintf("uid-filler-%d", i))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, c.EntriesForTest(), maxRevocationCacheEntries,
		"the cache must stay bounded; an unbounded map keyed by subject is a memory leak")
}

// TestCacheEvictsOldestEntryFirst pins WHICH entry eviction drops, not
// just that the cache stays bounded. Staying within
// maxRevocationCacheEntries is necessary but not sufficient: an
// implementation that evicted the most-recently-inserted entry instead
// of the oldest would also stay bounded, yet would evict the subject
// most likely to be looked up again next (recency), defeating the point
// of a cache. QueriesForTest is used as the observable, since it is the
// only way to distinguish "still cached" from "evicted and re-read"
// from outside the type.
func TestCacheEvictsOldestEntryFirst(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	// Fill the cache to exactly capacity, oldest first.
	for i := range maxRevocationCacheEntries {
		_, err := c.RevokedAfter(ctx, fmt.Sprintf("uid-fifo-%d", i))
		require.NoError(t, err)
	}
	before := c.QueriesForTest()

	// One more, unseen subject forces exactly one eviction.
	_, err := c.RevokedAfter(ctx, "uid-fifo-new")
	require.NoError(t, err)
	require.Equal(t, before+1, c.QueriesForTest())

	// The oldest entry (index 0) must have been the one evicted: looking
	// it up again is a cache miss, so it costs a query.
	beforeOldest := c.QueriesForTest()
	_, err = c.RevokedAfter(ctx, "uid-fifo-0")
	require.NoError(t, err)
	require.Equal(t, beforeOldest+1, c.QueriesForTest(),
		"the oldest entry must have been evicted to make room for the new one")

	// The most recently inserted entry before the eviction (the last one
	// written by the fill loop) must still be cached: looking it up
	// again costs no query.
	beforeNewest := c.QueriesForTest()
	_, err = c.RevokedAfter(ctx, fmt.Sprintf("uid-fifo-%d", maxRevocationCacheEntries-1))
	require.NoError(t, err)
	require.Equal(t, beforeNewest, c.QueriesForTest(),
		"the entry inserted immediately before the eviction must still be cached")
}
