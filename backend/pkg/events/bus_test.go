package events_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

func TestOutboxPublishDispatchConsume(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), events.Migrations()))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "test-consumer",
		Subject: "hms.in.reference.pinged.v1",
		Handle: func(ctx context.Context, evt events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	// Publish inside a transaction — commits to outbox, not to NATS.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, "hms.in.reference.pinged.v1", events.Event{
			Type: "ReferencePinged", Version: 1, TenantID: "t-1",
			Data: json.RawMessage(`{"ping_id":"p-1"}`),
		})
	}))

	require.Eventually(t, func() bool { return handled.Load() == 1 },
		15*time.Second, 100*time.Millisecond, "event should be dispatched and consumed exactly once")

	// Redelivery of the same event id is a no-op (idempotency table).
	require.Never(t, func() bool { return handled.Load() > 1 }, 2*time.Second, 200*time.Millisecond)
}
