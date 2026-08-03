package events_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/events"
)

// TestPublishInvalidEventID verifies that a malformed event ID returns an
// error instead of panicking (uuid.MustParse would panic on bad input).
// No containers needed: Publish returns before touching gorm or NATS.
func TestPublishInvalidEventID(t *testing.T) {
	var bus events.Bus

	require.NotPanics(t, func() {
		err := bus.Publish(nil, "hms.in.reference.pinged.v1", events.Event{
			ID: "not-a-uuid",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "event id must be a uuid")
	})
}
