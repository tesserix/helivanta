package payments

import (
	"context"
	"log/slog"
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
	for _, d := range due {
		err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			in, found, err := lockIntent(tx, false, "id=? AND next_check_at <= now()", d.ID)
			if err != nil || !found {
				return err
			}
			closeAs := ""
			if in.Status == "awaiting_payment" && !svc.now().Before(in.ExpiresAt) {
				closeAs = "expired"
			}
			return svc.check(ctx, tx, in, "poll", closeAs)
		})
		if err == nil {
			continue
		}
		// A provider outage must not stall the queue on one intent: push it
		// back and try again later. The intent keeps its status, so the
		// expiry decision still happens, just late.
		slog.WarnContext(ctx, "payments check failed; retrying in 30s", "tenant_id", d.TenantID, "intent_id", d.ID, "error", err)
		if err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE payment_intents SET next_check_at=now()+interval '30 seconds' WHERE id=? AND next_check_at IS NOT NULL`, d.ID).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
