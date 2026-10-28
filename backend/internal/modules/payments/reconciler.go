package payments

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// reconcileBatch bounds the intents one pass checks; each costs a
// provider round trip.
const reconcileBatch = 50

// RunReconciler converges intents whose webhook never came (one cannot
// reach a laptop, and production loses some) until ctx ends.
func RunReconciler(ctx context.Context, deps platform.Deps) {
	svc := serviceFor(deps)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := reconcileOnce(ctx, deps.DB, svc); err != nil {
				slog.WarnContext(ctx, "payments reconcile pass failed", "error", err)
			}
		}
	}
}

// ReconcileOnce runs one pass; tests drive it instead of the ticker.
func ReconcileOnce(ctx context.Context, deps platform.Deps) error {
	return reconcileOnce(ctx, deps.DB, serviceFor(deps))
}

// reconcileWorkers bounds the checks a pass runs at once: a slow provider
// then delays the pass by its slowest few calls, not by fifty in a row,
// and no check holds a database connection while it waits.
const reconcileWorkers = 8

func reconcileOnce(ctx context.Context, db *tenantdb.DB, svc *service) error {
	var due []struct {
		TenantID uuid.UUID
		ID       uuid.UUID
	}
	// The one cross-tenant read: ids of due intents, nothing else (the
	// system role holds SELECT on just these columns). The check itself
	// runs under the intent's own tenant.
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id,id FROM payment_intents WHERE next_check_at <= now() ORDER BY next_check_at LIMIT ?`, reconcileBatch).Scan(&due).Error
	}); err != nil {
		return err
	}
	ctx = tenantdb.WithActor(ctx, "system:payments-reconciler")
	slots := make(chan struct{}, reconcileWorkers)
	var wg sync.WaitGroup
	for _, d := range due {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			// check reschedules a failed intent itself (30 s, bounded for
			// closed intents); the pass only reports it.
			if err := svc.check(ctx, d.TenantID.String(), d.ID, "poll", false); err != nil {
				slog.WarnContext(ctx, "payments check failed; retrying later", "tenant_id", d.TenantID, "intent_id", d.ID, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}
