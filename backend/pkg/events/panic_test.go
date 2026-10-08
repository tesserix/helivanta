package events_test

import (
	"context"
	"encoding/json"
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

// A consumer handler panicking must dead-letter one event, not terminate
// the process. GORM's Transaction recovers and re-panics, and
// gin.Recovery() covers only HTTP handlers, so before this guard a
// malformed-but-parseable payload took down every module for every
// tenant. If the recover is missing, this test does not fail — the test
// binary crashes, which is the point.
func TestPanickingConsumerDoesNotKillTheProcess(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	// tenantdb.Migrations() first: outbox_events' policy
	// (0002_events_outbox_tenant) calls hms_tenant_visible.
	require.NoError(t, db.Migrate(context.Background(), append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const consumerName = "panic-test-consumer"
	const subject = "helivanta.in.reference.panictest.v1"

	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    consumerName,
		Subject: subject,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			panic("boom")
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	// Subscribe to the DLQ subject with a plain core-NATS subscription,
	// BEFORE publishing, so we catch the js.Publish delivery once
	// maxDeliver is exhausted (a core subscriber on a matching subject
	// receives the message the same as any JetStream-tracked publish).
	rawNC, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer rawNC.Close()
	dlqSub, err := rawNC.SubscribeSync(bus.Subject("helivanta.dlq." + consumerName))
	require.NoError(t, err)
	defer func() { _ = dlqSub.Unsubscribe() }()

	evt := events.Event{
		ID: uuid.NewString(), Type: "ReferencePanicTest", Version: 1,
		OccurredAt: time.Now().UTC(), TenantID: "t-1",
		Data: json.RawMessage(`{"ping_id":"panic-1"}`),
	}
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, evt)
	}))

	// Reaching this point at all is half the assertion: before the
	// recover, the panic would have crashed the test binary rather than
	// let execution get here.
	dlqMsg, err := dlqSub.NextMsg(15 * time.Second)
	require.NoError(t, err, "expected a message on the DLQ subject after maxDeliver exhaustion")

	var dlqEvt events.Event
	require.NoError(t, json.Unmarshal(dlqMsg.Data, &dlqEvt))
	require.Equal(t, evt.ID, dlqEvt.ID, "dead-lettered payload should be the original envelope, byte-for-byte")
}
