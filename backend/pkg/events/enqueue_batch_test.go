package events_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestEnqueueBatchRejectsLaterInvalidEventBeforeWriting(t *testing.T) {
	for _, tt := range []struct {
		name    string
		invalid events.Event
		message string
	}{
		{"invalid id", events.Event{ID: "not-a-uuid", Type: "StockMovementRecorded", Version: 1}, "event id must be a uuid"},
		{"invalid payload", events.Event{Type: "StockMovementRecorded", Version: 1, Data: json.RawMessage(`{`)}, "unexpected end of JSON input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := events.EnqueueBatch(nil, "helivanta.in.pharmacy.stock_movement_recorded.v1", []events.Event{
				{Type: "StockMovementRecorded", Version: 1, Data: json.RawMessage(`{}`)},
				tt.invalid,
			})
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestEnqueueBatchEmptyAndSizeBound(t *testing.T) {
	require.NoError(t, events.EnqueueBatch(nil, "test", nil))
	require.NoError(t, events.EnqueueBatch(nil, "test", []events.Event{}))
	require.ErrorContains(t, events.EnqueueBatch(nil, "test", make([]events.Event, 10001)), "exceeds 10000")
}
