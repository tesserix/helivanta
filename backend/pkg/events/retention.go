package events

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"gorm.io/gorm"
)

// streamMaxAge bounds how long a payload — patient names included — lives on
// the JetStream stream. Was 7 days; with maxDeliver=5 and ackWait=30s
// (see bus.go) a failing message reaches the DLQ in ~2.5 minutes, so
// everything past that was retained PHI rather than recoverability. 24h
// still spans an overnight outage by a wide margin. Also the outbox's own
// pruning window (see Prune): a published row's payload has no reason to
// outlive the payload's own life on the stream it was dispatched to, and
// tying the two together avoids a third independent number to keep in sync
// (spec 2026-08-14-event-transport-isolation-design.md, D3).
const streamMaxAge = 24 * time.Hour

// processedRetention MUST exceed streamMaxAge. processed_events is the
// idempotency ledger: JetStream redelivers for up to streamMaxAge, so
// pruning the ledger sooner makes a redelivered event indistinguishable
// from a new one — the consumer's ON CONFLICT DO NOTHING claim in
// runConsumerTx finds no row, runs the handler again, and inserts a SECOND
// pharmacy_dispenses row: a duplicate clinical record produced by a cleanup
// job. Derived from streamMaxAge, not written as an independent number, so
// a developer tidying retention config cannot silently invert the
// relationship — TestProcessedRetentionOutlivesRedelivery makes the
// invariant a CI failure rather than a comment.
const processedRetention = streamMaxAge + 24*time.Hour

// pruneInterval is how often RunPruner sweeps outbox_events and
// processed_events. Hourly, and its own loop — not folded into
// RunDispatcher's 500ms tick, which would put a DELETE on the publish hot
// path on every dispatch cycle for no benefit (spec D3, rejected).
const pruneInterval = time.Hour

// PruneResult reports how many rows Prune actually removed. Its existence
// is the point: a prune whose effect nobody can observe is the same blind
// spot as an unlogged exemption, and a caller that only checks the error is
// exactly the shape that would have hidden the WithSystem defect below.
type PruneResult struct {
	OutboxDeleted int64
	LedgerDeleted int64
}

// Prune deletes outbox rows that have already been dispatched and ledger
// rows old enough that JetStream can no longer redeliver the event they
// guard.
//
// Two rules make this safe rather than merely convenient:
//
//  1. Unpublished outbox rows are NEVER deleted, regardless of age. An old
//     unpublished row means the dispatcher failed to drain it — a bug to
//     investigate, not garbage to clean up. Deleting it would silently lose
//     an event that was never actually delivered.
//  2. The ledger window is processedRetention, not streamMaxAge. Pruning
//     on the shorter window reintroduces duplicate processing on
//     redelivery (see processedRetention's comment).
//
// Prune runs on WithAllTenants, not WithSystem — and this is the one thing
// in this function that is not optional. (It said WithAdmin until #894;
// see below.) outbox_events carries forced row-
// level security since 0002_events_outbox_tenant (#835 Task 1), and
// WithSystem sets no tenant GUC. A DELETE run under WithSystem would match
// zero rows in every tenant's outbox — and every tenant-less row is, by
// 0002's policy, invisible to WithSystem too — and report success. That is
// a pruner that runs forever and never prunes anything, with no error
// anywhere: exactly the trap TestPruneDeletesPublishedOutboxRows exists to
// pin, by asserting the row count actually drops rather than that the call
// returns nil. processed_events carries no RLS (it stays allowlisted, spec
// D2), so the bypass is not strictly required for that half — but using
// one transaction for both keeps this function a single privilege path
// instead of splitting it per statement for no operational benefit.
//
// #894: this called WithAdmin, which names the schema OWNER — and FORCE
// ROW LEVEL SECURITY binds the owner. So the DELETE this comment warns
// about ("matches zero rows in every tenant's outbox and reports success")
// is exactly what Prune did in production. The hazard was identified
// correctly and the mitigation chosen for it had the same defect, because
// WithAdmin's own documentation said it bypassed RLS and it did not.
func (b *Bus) Prune(ctx context.Context, db OutboxStore) (PruneResult, error) {
	var res PruneResult
	now := time.Now().UTC()
	err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		outboxCutoff := now.Add(-streamMaxAge)
		outboxResult := tx.Exec(`DELETE FROM outbox_events
			WHERE published_at IS NOT NULL AND published_at < ?`, outboxCutoff)
		if outboxResult.Error != nil {
			return fmt.Errorf("prune outbox_events: %w", outboxResult.Error)
		}
		res.OutboxDeleted = outboxResult.RowsAffected

		ledgerCutoff := now.Add(-processedRetention)
		ledgerResult := tx.Exec(`DELETE FROM processed_events WHERE processed_at < ?`, ledgerCutoff)
		if ledgerResult.Error != nil {
			return fmt.Errorf("prune processed_events: %w", ledgerResult.Error)
		}
		res.LedgerDeleted = ledgerResult.RowsAffected
		return nil
	})
	return res, err
}

// RunPruner runs Prune on a fixed pruneInterval schedule until ctx ends or
// the Bus is Close()'d. It is a separate goroutine and a separate ticker
// from RunDispatcher on purpose (spec D3) — deletion latency is decoupled
// from publish throughput.
func (b *Bus) RunPruner(ctx context.Context, db OutboxStore) {
	loopCtx, cancel := b.deriveCtx(ctx)
	defer cancel()
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-loopCtx.Done():
			return
		case <-ticker.C:
			b.pruneSafely(loopCtx, db)
		}
	}
}

// pruneSafely runs one prune pass, containing a panic to this tick. Same
// shape as drainSafely: RunPruner is started with a bare `go` in main, so a
// panic here would otherwise terminate the process.
func (b *Bus) pruneSafely(ctx context.Context, db OutboxStore) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("outbox prune panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	result, err := b.Prune(ctx, db)
	if err != nil {
		slog.Error("outbox prune", "err", err)
		return
	}
	// A prune whose effect nobody can observe is the same blind spot as an
	// unlogged exemption — log the counts even when both are zero, so an
	// operator can tell the loop is running versus silently not deleting.
	slog.Info("outbox prune", "outbox_deleted", result.OutboxDeleted, "ledger_deleted", result.LedgerDeleted)
}
