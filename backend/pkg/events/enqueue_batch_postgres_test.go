package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEnqueueBatchCommitAndTenantIsolation(t *testing.T) {
	db, _, _ := setUpOutboxHarness(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	const subject = "helivanta.in.test.outbox_batch.v1"
	when := time.Date(2026, 11, 20, 1, 2, 3, 0, time.UTC)
	id := uuid.Must(uuid.NewV7())
	batch := make([]Event, 501)
	for i := range batch {
		batch[i] = Event{Type: "BatchTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{"units":3}`)}
	}
	batch[0].ID, batch[0].OccurredAt = id.String(), when
	require.NoError(t, db.WithTenant(t.Context(), tenantA, func(tx *gorm.DB) error {
		return EnqueueBatch(tx, subject, batch)
	}))
	require.Empty(t, batch[1].ID, "enqueue must not mutate caller-owned retry input")
	require.True(t, batch[1].OccurredAt.IsZero())
	require.NoError(t, db.WithTenant(t.Context(), tenantA, func(tx *gorm.DB) error {
		var rows []outboxRow
		if err := tx.Select("id,tenant_id,payload,subject").Where("subject=?", subject).Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 501)
		for _, row := range rows {
			require.Equal(t, uuid.MustParse(tenantA), *row.TenantID)
			require.Equal(t, subject, row.Subject)
			var evt Event
			require.NoError(t, json.Unmarshal(row.Payload, &evt))
			require.Equal(t, row.ID.String(), evt.ID)
			require.Equal(t, tenantA, evt.TenantID)
			require.Equal(t, "BatchTest", evt.Type)
			require.JSONEq(t, `{"units":3}`, string(evt.Data))
			require.False(t, evt.OccurredAt.IsZero())
			if row.ID == id {
				require.Equal(t, when, evt.OccurredAt)
			}
		}
		return nil
	}))
	require.NoError(t, db.WithTenant(t.Context(), tenantB, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", subject).Count(&n).Error
		require.Zero(t, n)
		return err
	}))
}

func TestEnqueueBatchLaterRLSFailureRollsBackEarlierBatches(t *testing.T) {
	db, _, _ := setUpOutboxHarness(t)
	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	const subject = "helivanta.in.test.outbox_batch_rollback.v1"
	sentinel := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(t.Context(), tenantA, func(tx *gorm.DB) error {
		return Enqueue(tx, subject, Event{ID: sentinel.String(), Type: "BeforeBatch", Version: 1, TenantID: tenantA})
	}))
	batch := make([]Event, 501)
	for i := range batch {
		batch[i] = Event{Type: "BatchTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)}
	}
	batch[500].TenantID = tenantB
	err := db.WithTenant(t.Context(), tenantA, func(tx *gorm.DB) error {
		return EnqueueBatch(tx, subject, batch)
	})
	require.ErrorContains(t, err, "row-level security")
	require.NoError(t, db.WithTenant(t.Context(), tenantA, func(tx *gorm.DB) error {
		var ids []uuid.UUID
		err := tx.Table("outbox_events").Where("subject=?", subject).Pluck("id", &ids).Error
		require.Equal(t, []uuid.UUID{sentinel}, ids)
		return err
	}))
}
