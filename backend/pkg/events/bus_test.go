package events_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestOutboxPublishDispatchConsume(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	// tenantdb.Migrations() must run first: outbox_events' policy
	// (0002_events_outbox_tenant) calls hms_tenant_visible, which only
	// tenantdb.Migrations() defines.
	require.NoError(t, db.Migrate(context.Background(), append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "test-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	// Fix event id/timestamp up front so the direct-republish below sends
	// byte-identical JSON with the same event_id.
	evt := events.Event{
		ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
		OccurredAt: time.Now().UTC(), TenantID: "t-1",
		Data: json.RawMessage(`{"ping_id":"p-1"}`),
	}

	// Publish inside a transaction — commits to outbox, not to NATS.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", evt)
	}))

	require.Eventually(t, func() bool { return handled.Load() == 1 },
		15*time.Second, 100*time.Millisecond, "event should be dispatched and consumed exactly once")

	// Redelivery of the same event id is a no-op (idempotency table).
	require.Never(t, func() bool { return handled.Load() > 1 }, 2*time.Second, 200*time.Millisecond)

	// Force a genuine duplicate delivery: connect directly to NATS and
	// publish the identical envelope WITHOUT a MsgId header, so JetStream's
	// server-side dedup can't intervene — only the (consumer, event_id)
	// claim in processed_events should stop the handler from re-running.
	directNC, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer directNC.Close()
	directJS, err := directNC.JetStream()
	require.NoError(t, err)

	payload, err := json.Marshal(evt)
	require.NoError(t, err)
	_, err = directJS.Publish(bus.Subject("helivanta.in.reference.pinged.v1"), payload)
	require.NoError(t, err)

	require.Never(t, func() bool { return handled.Load() > 1 }, 3*time.Second, 200*time.Millisecond,
		"duplicate delivery of the same event_id must be claimed and skipped, not re-handled")

	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM processed_events WHERE consumer = ? AND event_id = ?`,
			"test-consumer", evt.ID).Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count, "exactly one processed_events row for the consumer/event pair")
		return nil
	}))
}

func TestConsumerTenantScopedWrite(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	// tenantdb.Migrations() first: outbox_events' own policy
	// (0002_events_outbox_tenant, inside events.Migrations()) calls
	// hms_tenant_visible, which only tenantdb.Migrations() defines.
	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, tenantdb.Migration{
		ID: "0002_consumer_widgets",
		SQL: `
			CREATE TABLE consumer_widgets (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  note text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE consumer_widgets ENABLE ROW LEVEL SECURITY;
			ALTER TABLE consumer_widgets FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON consumer_widgets
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	})
	require.NoError(t, db.Migrate(context.Background(), migs))

	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA, tenantB := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"

	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "tenant-write-consumer",
		Subject: "helivanta.in.test.tenantwrite.v1",
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			// Relies on the bus having set app.tenant_id from evt.TenantID.
			return tx.Exec(`INSERT INTO consumer_widgets (tenant_id, note) VALUES (?, 'from-consumer')`,
				evt.TenantID).Error
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	// Published from inside WithTenant(tenantA, ...), not WithSystem: since
	// 0002_events_outbox_tenant, the outbox's WITH CHECK pins a
	// non-NULL tenant_id to the GUC of the transaction that inserted it
	// (design spec D1a) — this mirrors how every real tenant-scoped
	// publisher in this codebase calls Publish (see
	// docs/standards/backend.md's events section), unlike iam's
	// tenant-less CredentialRevoked broadcast, which really does publish
	// from WithSystem.
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.test.tenantwrite.v1", events.Event{
			Type: "TenantWrite", Version: 1, TenantID: tenantA,
			Data: json.RawMessage(`{}`),
		})
	}))

	// Row lands for tenant A…
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM consumer_widgets`).Scan(&n).Error
		})
		return n == 1
	}, 15*time.Second, 100*time.Millisecond)

	// …and tenant B sees nothing (RLS held inside the consumer).
	var nB int64
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM consumer_widgets`).Scan(&nB).Error
	}))
	require.Zero(t, nB)
}
