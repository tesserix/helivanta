package events

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
)

const pingSubject = "hms.in.reference.pinged.v1"

// Production must be untouched by the namespacing: an empty namespace has
// to give the stream and subjects the deployed system has always used, or
// this test-only isolation would quietly rename production's stream.
func TestEmptyNamespaceKeepsProductionSubjects(t *testing.T) {
	b := &Bus{}

	require.Equal(t, StreamName, b.streamName())
	require.Equal(t, pingSubject, b.Subject(pingSubject))
	require.Equal(t, "hms.dlq.some-consumer", b.Subject("hms.dlq.some-consumer"))
}

// A namespace becomes part of a stream name and a subject token, so it
// cannot carry the characters those forbid. Go subtest names contain
// slashes, and a dot would silently split into an extra subject token
// rather than fail — the failure mode this guards is a wrong subject
// space, not an error.
func TestSanitizeNamespace(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"TestFoo", "TestFoo"},
		{"TestFoo/sub_case", "TestFoo_sub_case"},
		{"has.dots", "has_dots"},
		{"star*and>gt", "star_and_gt"},
		{"spaces here", "spaces_here"},
	} {
		require.Equal(t, tc.want, sanitizeNamespace(tc.in), tc.in)
	}
}

// The point of the namespace: two buses on one NATS server do not see each
// other's events. Without it they would share the single HMS stream over
// `hms.>`, and every test's consumers would receive every other test's
// events — the reason each test used to need its own container.
func TestNamespacedBusesDoNotShareSubjects(t *testing.T) {
	url := testinfra.StartNATS(t)

	a, err := NewBusInNamespace(url, t.Name()+"_a")
	require.NoError(t, err)
	defer a.Close()
	b, err := NewBusInNamespace(url, t.Name()+"_b")
	require.NoError(t, err)
	defer b.Close()

	require.NotEqual(t, a.streamName(), b.streamName(), "namespaces must not share a stream")
	require.NotEqual(t, a.Subject(pingSubject), b.Subject(pingSubject))

	nc, err := nats.Connect(url)
	require.NoError(t, err)
	defer nc.Close()
	js, err := nc.JetStream()
	require.NoError(t, err)

	_, err = js.Publish(a.Subject(pingSubject), []byte(`{"ping_id":"ns-1"}`))
	require.NoError(t, err)

	aInfo, err := a.js.StreamInfo(a.streamName())
	require.NoError(t, err)
	require.EqualValues(t, 1, aInfo.State.Msgs, "the publishing namespace should hold the event")

	bInfo, err := b.js.StreamInfo(b.streamName())
	require.NoError(t, err)
	require.EqualValues(t, 0, bInfo.State.Msgs, "an event published in one namespace reached another")
}
