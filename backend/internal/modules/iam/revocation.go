package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	// maxRevocationCacheEntries bounds memory at roughly a megabyte. A
	// single hospital's concurrent staff is two orders of magnitude
	// below this, so eviction should effectively never happen; when it
	// does, the evicted entry reads through and is correct, only slower.
	maxRevocationCacheEntries = 10_000

	// revocationCacheTTL is the BACKSTOP for a missed broadcast, not the
	// propagation mechanism — invalidation is (see pkg/events/broadcast.go,
	// Task 5). Five minutes bounds the abnormal case to something an
	// incident responder can accept while keeping steady-state traffic
	// off Postgres.
	//
	// Both constants are code, not configuration, on purpose: a
	// deployment that tuned the TTL upward would silently widen a
	// security window, so changing it should require a reviewer.
	revocationCacheTTL = 5 * time.Minute
)

type revocationRow struct {
	Subject   string `gorm:"primaryKey"`
	RevokedAt time.Time
	Reason    string
	Actor     string
	UpdatedAt time.Time
}

func (revocationRow) TableName() string { return "iam_credential_revocations" }

type cacheEntry struct {
	watermark time.Time // zero == never revoked
	readAt    time.Time
}

// RevocationChecker answers "has this subject's credential been
// revoked, and when" for the authentication path, and is the only
// writer of the watermark. It implements authn.RevocationChecker.
//
// The cache holds NEGATIVE entries too — "not revoked" is the
// overwhelmingly common answer and the one that must not reach
// Postgres on every request.
type RevocationChecker struct {
	db *tenantdb.DB

	mu      sync.RWMutex
	entries map[string]cacheEntry
	order   []string // insertion order, for bounded eviction

	queries atomic.Int64 // observability + the cache test's assertion
}

func NewRevocationChecker(db *tenantdb.DB) *RevocationChecker {
	return &RevocationChecker{db: db, entries: make(map[string]cacheEntry, 128)}
}

// RevokedAfter returns subject's watermark, or the zero time if it has
// never been revoked. It returns an error rather than the zero time
// when the answer cannot be read: the caller must be able to tell
// "definitely not revoked" from "cannot say".
func (r *RevocationChecker) RevokedAfter(ctx context.Context, subject string) (time.Time, error) {
	if e, ok := r.lookup(subject); ok {
		return e.watermark, nil
	}
	var row revocationRow
	r.queries.Add(1)
	// WithSystem, not WithTenant: this table is not tenant-scoped and
	// the authentication path has no tenant context yet — the tenant
	// claim has been read but nothing has authorized the caller to act
	// in it.
	err := r.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Where("subject = ?", subject).First(&row).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		r.store(subject, time.Time{})
		return time.Time{}, nil
	case err != nil:
		return time.Time{}, fmt.Errorf("read revocation watermark for %s: %w", subject, err)
	}
	r.store(subject, row.RevokedAt.UTC())
	return row.RevokedAt.UTC(), nil
}

// RevokeTx moves subject's watermark to at, never backwards.
//
// It takes a tx so the caller can publish the invalidation event
// through the outbox in the same transaction: a watermark that
// committed without its event would propagate only at TTL, and an event
// published without its watermark would be a lie.
func (r *RevocationChecker) RevokeTx(tx *gorm.DB, subject string, at time.Time, reason, actor string) error {
	return tx.Exec(`
		INSERT INTO iam_credential_revocations (subject, revoked_at, reason, actor, updated_at)
		VALUES (?, ?, ?, ?, now())
		ON CONFLICT (subject) DO UPDATE SET
		  revoked_at = GREATEST(iam_credential_revocations.revoked_at, EXCLUDED.revoked_at),
		  reason     = EXCLUDED.reason,
		  actor      = EXCLUDED.actor,
		  updated_at = now()`,
		subject, at.UTC(), reason, actor).Error
}

// Invalidate drops subject's cached entry, forcing the next lookup to
// re-read. Called by the broadcast consumer on every replica (Task 5/6).
func (r *RevocationChecker) Invalidate(subject string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, subject)
}

func (r *RevocationChecker) lookup(subject string) (cacheEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[subject]
	if !ok || time.Since(e.readAt) > revocationCacheTTL {
		return cacheEntry{}, false
	}
	return e, true
}

func (r *RevocationChecker) store(subject string, watermark time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[subject]; !exists {
		if len(r.order) >= maxRevocationCacheEntries {
			delete(r.entries, r.order[0])
			r.order = r.order[1:]
		}
		r.order = append(r.order, subject)
	}
	r.entries[subject] = cacheEntry{watermark: watermark, readAt: time.Now()}
}

// QueriesForTest reports how many times the cache missed and read
// Postgres. Exported for the cache test, which cannot otherwise
// distinguish a cache hit from a fast query.
func (r *RevocationChecker) QueriesForTest() int64 { return r.queries.Load() }

// EntriesForTest reports the current cache size. Exported for the
// eviction test, which cannot otherwise observe whether the bounded-size
// invariant held.
func (r *RevocationChecker) EntriesForTest() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
