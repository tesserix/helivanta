package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/events"
)

// newTestBus gives a Bus namespaced to this test, sharing the one NATS
// server test containers spin up — mirrors the pattern already used
// directly in bus_test.go, wrapped for reuse here.
func newTestBus(t *testing.T) *events.Bus {
	t.Helper()
	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	return bus
}

// publishDirectly connects to NATS directly and publishes into
// JetStream, bypassing the outbox entirely — broadcasts don't go
// through Postgres, so there is nothing for the outbox/dispatcher to do.
// testinfra.StartNATS is a shared-server singleton (sync.Once), so this
// resolves to the exact same NATS instance the bus already talks to.
func publishDirectly(t *testing.T, bus *events.Bus, subject string, evt events.Event) {
	t.Helper()
	nc, err := nats.Connect(testinfra.StartNATS(t))
	require.NoError(t, err)
	defer nc.Close()
	js, err := nc.JetStream()
	require.NoError(t, err)
	payload, err := json.Marshal(evt)
	require.NoError(t, err)
	_, err = js.Publish(bus.Subject(subject), payload)
	require.NoError(t, err)
}

func TestBroadcastReachesEverySubscriber(t *testing.T) {
	bus := newTestBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	const subscribers = 3
	received := make(chan string, subscribers)
	for range subscribers {
		require.NoError(t, bus.StartBroadcasts(ctx, []events.Broadcast{{
			Subject: "hms.in.iam.credential_revoked.v1",
			Handle:  func(_ context.Context, evt events.Event) { received <- string(evt.Data) },
		}}))
	}

	publishDirectly(t, bus, "hms.in.iam.credential_revoked.v1", events.Event{
		Type: "CredentialRevoked", Version: 1, Data: json.RawMessage(`{"subject":"uid-nurse"}`),
	})

	for i := range subscribers {
		select {
		case <-received:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d subscribers received the broadcast; a durable consumer would give exactly 1", i, subscribers)
		}
	}
}
