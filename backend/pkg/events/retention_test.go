package events

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// package events (white-box, not events_test): TestPruneCannotResurrectADuplicate
// calls runConsumerTx directly to simulate a JetStream redelivery without
// waiting on real timers, and the backdating helpers below reach past
// Publish/drainOnce into direct SQL to age rows without sleeping for real
// retention windows (24h/48h) — see each test's comment.

// TestProcessedRetentionOutlivesRedelivery pins the coupling this whole
// design rests on (spec D3): the ledger window must strictly exceed the
// stream's redelivery window, or a cleanup job reintroduces duplicate
// processing. This is a plain constant comparison — no database needed —
// so a developer tidying retention config gets a CI failure, not a
// silent inversion.
func TestProcessedRetentionOutlivesRedelivery(t *testing.T) {
	require.Greater(t, processedRetention, streamMaxAge,
		"the idempotency ledger must outlive JetStream's redelivery window, or pruning it "+
			"makes a redelivered event indistinguishable from a new one")
}

// backdateOutboxRow rewrites created_at/published_at on an outbox row
// directly via SQL, bypassing Publish/drainOnce, so retention tests can
// exercise the streamMaxAge boundary without sleeping for real hours. Runs
// under the row's own tenant (WithTenant), exactly like every other access
// to a tenant-scoped outbox row in this package's tests — outbox_events
// carries forced RLS, and going through WithTenant here (rather than
// reaching for WithAdmin, which internal/archtest's
// TestWithAdminIsOnlyCalledFromTheAllowlist reserves for the dispatcher,
// the pruner and the reconciler) proves the backdate itself respects the
// same isolation the code under test is required to respect.
func backdateOutboxRow(t *testing.T, db *tenantdb.DB, tenantID string, id uuid.UUID, createdAt time.Time, publishedAt *time.Time) {
	t.Helper()
	require.NoError(t, db.WithTenant(context.Background(), tenantID, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE outbox_events SET created_at = ?, published_at = ? WHERE id = ?`,
			createdAt, publishedAt, id).Error
	}))
}

// backdateLedgerRow rewrites processed_at on a processed_events row
// directly via SQL. processed_events carries no RLS (spec D2, it stays
// allowlisted), so this runs on WithSystem, matching every other access to
// it in this package's tests (see dlq_test.go).
func backdateLedgerRow(t *testing.T, db *tenantdb.DB, consumer string, eventID uuid.UUID, processedAt time.Time) {
	t.Helper()
	require.NoError(t, db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE processed_events SET processed_at = ? WHERE consumer = ? AND event_id = ?`,
			processedAt, consumer, eventID).Error
	}))
}

func countOutboxRows(t *testing.T, db *tenantdb.DB, tenantID string, id uuid.UUID) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.WithTenant(context.Background(), tenantID, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE id = ?`, id).Scan(&n).Error
	}))
	return n
}

func countLedgerRows(t *testing.T, db *tenantdb.DB, consumer string, eventID uuid.UUID) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM processed_events WHERE consumer = ? AND event_id = ?`,
			consumer, eventID).Scan(&n).Error
	}))
	return n
}

// TestPruneDeletesPublishedOutboxRows asserts on the row actually
// disappearing, not on Prune returning nil — a DELETE run under the wrong
// pool (trap 3, WithSystem) matches zero rows and reports success, so an
// assertion on the error alone would pass with that defect present.
func TestPruneDeletesPublishedOutboxRows(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA := uuid.NewString()
	const subject = "helivanta.in.test.prunepublished.v1"
	var id uuid.UUID
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		evt := Event{ID: uuid.NewString(), Type: "PrunePublishedTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)}
		id = uuid.MustParse(evt.ID)
		return bus.Publish(tx, subject, evt)
	}))
	require.NoError(t, bus.drainOnce(ctx, db))
	require.Equal(t, int64(1), countOutboxRows(t, db, tenantA, id), "sanity: the published row exists before pruning")

	// Past the outbox's pruning window (streamMaxAge).
	old := time.Now().Add(-streamMaxAge - time.Hour)
	backdateOutboxRow(t, db, tenantA, id, old, &old)

	result, err := bus.Prune(ctx, db)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.OutboxDeleted, "Prune's own reported count must reflect the deletion")
	require.Equal(t, int64(0), countOutboxRows(t, db, tenantA, id), "the published row must actually be gone, not merely reported gone")
}

// TestPruneKeepsUnpublishedRows: an old unpublished row is a dispatcher
// failure to investigate, not garbage — Prune must never delete it
// regardless of age (spec D3).
func TestPruneKeepsUnpublishedRows(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA := uuid.NewString()
	const subject = "helivanta.in.test.pruneunpublished.v1"
	var id uuid.UUID
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		evt := Event{ID: uuid.NewString(), Type: "PruneUnpublishedTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)}
		id = uuid.MustParse(evt.ID)
		return bus.Publish(tx, subject, evt)
	}))
	// Deliberately never drained — published_at stays NULL. Age it far past
	// every retention window under test.
	backdateOutboxRow(t, db, tenantA, id, time.Now().Add(-30*24*time.Hour), nil)

	result, err := bus.Prune(ctx, db)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.OutboxDeleted, "an unpublished row must not be counted as deleted")
	require.Equal(t, int64(1), countOutboxRows(t, db, tenantA, id), "an unpublished row must survive pruning no matter its age")
}

// TestPruneKeepsLedgerWithinRedeliveryWindow: a ledger row younger than
// processedRetention must survive, even though it is already older than
// streamMaxAge — that gap between the two windows is exactly what
// processedRetention exists to cover.
func TestPruneKeepsLedgerWithinRedeliveryWindow(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const consumer = "prune-ledger-test-consumer"
	eventID := uuid.New()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO processed_events (consumer, event_id) VALUES (?, ?)`, consumer, eventID).Error
	}))
	// Older than streamMaxAge, but still within processedRetention.
	backdateLedgerRow(t, db, consumer, eventID, time.Now().Add(-(streamMaxAge + time.Hour)))

	result, err := bus.Prune(ctx, db)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.LedgerDeleted)
	require.Equal(t, int64(1), countLedgerRows(t, db, consumer, eventID),
		"a ledger row within processedRetention must survive even though it is past streamMaxAge")
}

// TestPruneCannotResurrectADuplicate is the load-bearing test (spec D3):
// process an event, age its ledger row into the gap between streamMaxAge
// and processedRetention (still a live redelivery window, since JetStream
// redelivers for up to streamMaxAge... actually just past it here to prove
// the row surviving is what stops the duplicate), prune, then simulate the
// redelivery runConsumerTx would see and assert the handler does NOT run a
// second time. It calls runConsumerTx directly rather than driving a real
// JetStream redelivery, because forcing an actual redelivery would mean
// waiting out streamMaxAge for real — the point under test is entirely
// about what the ledger row does when the same event arrives twice, which
// runConsumerTx's ON CONFLICT DO NOTHING claim owns regardless of how the
// second delivery arrived.
func TestPruneCannotResurrectADuplicate(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const consumerName = "prune-duplicate-test-consumer"
	var attempts atomic.Int32
	consumer := Consumer{
		Name:    consumerName,
		Subject: "helivanta.in.test.pruneduplicate.v1",
		Handle: func(ctx context.Context, tx *gorm.DB, evt Event) error {
			attempts.Add(1)
			return nil
		},
	}

	evt := Event{
		ID: uuid.NewString(), Type: "PruneDuplicateTest", Version: 1,
		OccurredAt: time.Now().UTC(), TenantID: uuid.NewString(),
		Data: json.RawMessage(`{}`),
	}

	// First delivery: claims the ledger row and runs the handler once.
	require.NoError(t, bus.runConsumerTx(ctx, db, consumer, evt))
	require.Equal(t, int32(1), attempts.Load())
	require.Equal(t, int64(1), countLedgerRows(t, db, consumerName, uuid.MustParse(evt.ID)))

	// Age the ledger row into the window a redelivery could still land in
	// (past streamMaxAge, still within processedRetention) — the gap
	// processedRetention exists to cover.
	backdateLedgerRow(t, db, consumerName, uuid.MustParse(evt.ID), time.Now().Add(-(streamMaxAge + time.Hour)))

	result, err := bus.Prune(ctx, db)
	require.NoError(t, err)
	require.Equal(t, int64(0), result.LedgerDeleted, "the ledger row must still be within processedRetention")

	// Simulate the redelivery JetStream could still legitimately send.
	require.NoError(t, bus.runConsumerTx(ctx, db, consumer, evt))
	require.Equal(t, int32(1), attempts.Load(),
		"a redelivered event whose ledger row survived pruning must not be processed a second time")
	require.Equal(t, int64(1), countLedgerRows(t, db, consumerName, uuid.MustParse(evt.ID)),
		"exactly one ledger row — no duplicate claim")
}
