package events_test

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
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestConsumerRetriesExternalStartAfterCommittedClaim(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(t.Context(), append(tenantdb.Migrations(), events.Migrations()...)))
	bus, err := events.NewBusInNamespace(testinfra.StartNATS(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	var writes, starts atomic.Int32
	consumer := events.Consumer{
		Name: "test-external-start", Subject: "helivanta.in.test.external_start.v1",
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			writes.Add(1)
			return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", evt.ID).Error
		},
		AfterCommit: func(ctx context.Context, evt events.Event) error {
			err := db.WithSystem(ctx, func(tx *gorm.DB) error {
				var claim int64
				if err := tx.Raw("SELECT count(*) FROM processed_events WHERE consumer=? AND event_id=?", "test-external-start", evt.ID).Scan(&claim).Error; err != nil {
					return err
				}
				if claim != 1 {
					return errors.New("external work ran before database commit")
				}
				var unlocked bool
				if err := tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended(?,0))", evt.ID).Scan(&unlocked).Error; err != nil {
					return err
				}
				if !unlocked {
					return errors.New("external work retained database locks")
				}
				return nil
			})
			if err != nil {
				return err
			}
			if starts.Add(1) < 3 {
				return errors.New("synthetic external dependency unavailable")
			}
			return nil
		},
	}
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{consumer}))
	go bus.RunDispatcher(ctx, db)
	evt := events.Event{ID: uuid.NewString(), Type: "ExternalStart", Version: 1, OccurredAt: time.Now().UTC(), Data: json.RawMessage(`{}`)}
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return bus.Publish(tx, consumer.Subject, evt) }))
	require.Eventually(t, func() bool { return starts.Load() == 3 }, 15*time.Second, 25*time.Millisecond, "external start must retry despite an already committed claim")
	require.EqualValues(t, 1, writes.Load(), "transactional effects remain deduplicated")
	direct, err := nats.Connect(testinfra.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(direct.Close)
	dlq, err := direct.SubscribeSync(bus.Subject("helivanta.dlq." + consumer.Name))
	require.NoError(t, err)
	require.NoError(t, direct.Flush())
	js, err := direct.JetStream()
	require.NoError(t, err)
	evt.Data = json.RawMessage(`{"file_id":"different_reference"}`)
	payload, err := json.Marshal(evt)
	require.NoError(t, err)
	_, err = js.Publish(bus.Subject(consumer.Subject), payload)
	require.NoError(t, err)
	_, err = dlq.NextMsg(5 * time.Second)
	require.NoError(t, err, "an event ID reused with another payload must be rejected, not acknowledged")
	require.EqualValues(t, 3, starts.Load(), "mismatched duplicate must never start external work")
}
