package events_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// TestDirectedEventOnUndeclaredSubjectIsRefused is the fail-closed
// default: a destination the bus was never told to allow must not reach
// the handler at all. This is what makes a wiring regression — the
// AllowDirected call being dropped from main.go — safe rather than
// silently permissive.
func TestDirectedEventOnUndeclaredSubjectIsRefused(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(),
		append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Deliberately no AllowDirected call.
	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "refusal-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(context.Context, *gorm.DB, events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	origin, destination := uuid.NewString(), uuid.NewString()
	evt := events.Event{
		ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
		OccurredAt: time.Now().UTC(),
		TenantID:   origin, DestinationTenantID: destination,
		Data: json.RawMessage(`{"ping_id":"p-1"}`),
	}
	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", evt)
	}))

	// Give the dispatcher and consumer ample time to have done the wrong
	// thing, then assert they did not.
	require.Never(t, func() bool { return handled.Load() > 0 },
		5*time.Second, 100*time.Millisecond,
		"a directed event on an undeclared subject reached the handler")
}

// TestUndirectedEventIsUnaffected pins the regression: adding the field
// must not change any existing event's path.
func TestUndirectedEventIsUnaffected(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(),
		append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "undirected-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(context.Context, *gorm.DB, events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	origin := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", events.Event{
			ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
			OccurredAt: time.Now().UTC(), TenantID: origin,
			Data: json.RawMessage(`{"ping_id":"p-1"}`),
		})
	}))

	require.Eventually(t, func() bool { return handled.Load() == 1 },
		20*time.Second, 100*time.Millisecond)
}

// TestDirectedEnvelopeOmitsFieldWhenEmpty proves the wire format of every
// existing event is byte-identical. omitempty is load-bearing: a new
// always-present key would change every stored outbox payload.
func TestDirectedEnvelopeOmitsFieldWhenEmpty(t *testing.T) {
	b, err := json.Marshal(events.Event{ID: "e-1", TenantID: "t-1"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "destination_tenant_id")
}
