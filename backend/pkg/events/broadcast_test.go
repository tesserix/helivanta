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

// publishRaw publishes bytes that are not necessarily a valid Event, so
// the decode-failure path can be exercised with something json.Unmarshal
// actually rejects.
func publishRaw(t *testing.T, bus *events.Bus, subject string, payload []byte) {
	t.Helper()
	nc, err := nats.Connect(testinfra.StartNATS(t))
	require.NoError(t, err)
	defer nc.Close()
	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.Publish(bus.Subject(subject), payload)
	require.NoError(t, err)
}

// TestBroadcastSurvivesAPanickingHandler asserts the subscription is
// still live AFTER a handler panics — not merely that the process did
// not crash.
//
// The distinction is the whole point. A recover() that contains the
// panic but leaves the subscription dead would pass a "did it crash"
// test while silently ending cache invalidation for that replica: the
// revoked credential would then be honoured only at the 5-minute TTL,
// on a replica that looks perfectly healthy. So the assertion is that a
// SECOND message still arrives.
func TestBroadcastSurvivesAPanickingHandler(t *testing.T) {
	bus := newTestBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	const subject = "hms.in.iam.credential_revoked.v1"
	delivered := make(chan string, 2)
	require.NoError(t, bus.StartBroadcasts(ctx, []events.Broadcast{{
		Subject: subject,
		Handle: func(_ context.Context, evt events.Event) {
			if string(evt.Data) == `{"subject":"uid-panic"}` {
				panic("handler exploded")
			}
			delivered <- string(evt.Data)
		},
	}}))

	publishDirectly(t, bus, subject, events.Event{
		Type: "CredentialRevoked", Version: 1, Data: json.RawMessage(`{"subject":"uid-panic"}`),
	})
	publishDirectly(t, bus, subject, events.Event{
		Type: "CredentialRevoked", Version: 1, Data: json.RawMessage(`{"subject":"uid-after"}`),
	})

	select {
	case got := <-delivered:
		require.Equal(t, `{"subject":"uid-after"}`, got)
	case <-time.After(10 * time.Second):
		t.Fatal("no message delivered after a handler panicked; the panic killed the subscription, so this replica silently stopped invalidating its cache")
	}
}

// TestBroadcastSurvivesAnUndecodableMessage asserts the same liveness
// property for a payload json.Unmarshal rejects: the handler must not be
// invoked with a zero Event, and the subscription must still deliver the
// next message.
//
// Invoking the handler on a decode failure would be worse than dropping
// the message: the revocation broadcast carries the subject to
// invalidate, and a zero Event would invalidate the empty-string
// subject — a no-op that looks like success.
func TestBroadcastSurvivesAnUndecodableMessage(t *testing.T) {
	bus := newTestBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	const subject = "hms.in.iam.credential_revoked.v1"
	delivered := make(chan string, 2)
	require.NoError(t, bus.StartBroadcasts(ctx, []events.Broadcast{{
		Subject: subject,
		Handle:  func(_ context.Context, evt events.Event) { delivered <- string(evt.Data) },
	}}))

	publishRaw(t, bus, subject, []byte("{not json at all"))
	publishDirectly(t, bus, subject, events.Event{
		Type: "CredentialRevoked", Version: 1, Data: json.RawMessage(`{"subject":"uid-after"}`),
	})

	select {
	case got := <-delivered:
		require.Equal(t, `{"subject":"uid-after"}`, got,
			"the handler must never be invoked for an undecodable message; a zero Event would invalidate the empty subject and look like success")
	case <-time.After(10 * time.Second):
		t.Fatal("no message delivered after an undecodable one; the decode failure killed the subscription")
	}
}
