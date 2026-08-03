package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// TestHandleMsgDeadLettersAfterMaxDeliver is a white-box test (package
// events, not events_test) so it can shrink the unexported ackWait var —
// otherwise exhausting maxDeliver redeliveries would take the default
// 30s AckWait * 5 attempts.
func TestHandleMsgDeadLettersAfterMaxDeliver(t *testing.T) {
	prevAckWait := ackWait
	ackWait = 200 * time.Millisecond
	defer func() { ackWait = prevAckWait }()

	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), Migrations()))

	natsURL := testutil.StartNATS(t)
	bus, err := NewBus(natsURL)
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const consumerName = "dlq-test-consumer"
	const subject = "hms.in.reference.dlqtest.v1"

	var attempts atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []Consumer{{
		Name:    consumerName,
		Subject: subject,
		Handle: func(ctx context.Context, evt Event) error {
			attempts.Add(1)
			return errors.New("boom: handler always fails")
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
	dlqSub, err := rawNC.SubscribeSync("hms.dlq." + consumerName)
	require.NoError(t, err)
	defer func() { _ = dlqSub.Unsubscribe() }()

	evt := Event{
		ID: uuid.NewString(), Type: "ReferenceDLQTest", Version: 1,
		OccurredAt: time.Now().UTC(), TenantID: "t-1",
		Data: json.RawMessage(`{"ping_id":"dlq-1"}`),
	}
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, evt)
	}))

	dlqMsg, err := dlqSub.NextMsg(15 * time.Second)
	require.NoError(t, err, "expected a message on the DLQ subject after maxDeliver exhaustion")

	var dlqEvt Event
	require.NoError(t, json.Unmarshal(dlqMsg.Data, &dlqEvt))
	require.Equal(t, evt.ID, dlqEvt.ID, "dead-lettered payload should be the original envelope, byte-for-byte")

	// Term() should have stopped redelivery entirely: attempts should
	// already be pinned at maxDeliver and stay there.
	require.Equal(t, int32(maxDeliver), attempts.Load(), "handler should have been tried exactly maxDeliver times")
	require.Never(t, func() bool { return attempts.Load() > maxDeliver }, 2*time.Second, 200*time.Millisecond,
		"Term() should stop further redelivery once dead-lettered")

	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM processed_events WHERE consumer = ? AND event_id = ?`,
			consumerName, evt.ID).Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(0), count,
			"handler always errors — the idempotency claim tx always rolls back, so no processed_events row")
		return nil
	}))
}
