package logging_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

// untaggedDAG has NO helivantalog tag anywhere, so it never reaches the PHI
// masking path — before #943 nothing bounded it at all.
type untaggedDAG struct {
	Label string         `json:"label"`
	Kids  []*untaggedDAG `json:"kids,omitempty"`
}

func buildUntaggedDAG(levels int) *untaggedDAG {
	cur := &untaggedDAG{Label: "leaf"}
	for range levels {
		cur = &untaggedDAG{Label: "node", Kids: []*untaggedDAG{cur, cur}}
	}
	return cur
}

// TestUntaggedSharedReferenceValueIsOmittedNotMarshalled is #943's central
// claim. A depth-40 DAG renders to ~90 TB of JSON; if the bound did not
// apply to untagged values, slog's JSON handler would marshal it and this
// test would exhaust memory rather than fail an assertion. What it asserts
// is on the bytes: a bounded line, the omission marker naming the type, the
// rest of the record intact, and the omission counted.
func TestUntaggedSharedReferenceValueIsOmittedNotMarshalled(t *testing.T) {
	before := logging.LogValueOversizeCount()
	var buf bytes.Buffer
	wrapped(&buf).Info("event", "graph", buildUntaggedDAG(40), "request_id", "r-1")
	line := buf.String()

	require.Less(t, len(line), 512, "the emitted line must be bounded, not proportional to the value")
	require.Contains(t, line, `"graph":"[OMITTED:oversize:*logging_test.untaggedDAG]"`)
	require.Contains(t, line, `"request_id":"r-1"`, "the rest of the record must survive")
	require.NotContains(t, line, "[REDACTED:", "an untagged omission is not a PHI redaction")
	require.Equal(t, before+1, logging.LogValueOversizeCount())
}

// TestOversizedStringAttributeIsOmitted: a KindString attribute is rendered
// by slog directly, never through encoding/json, so it needs its own check.
func TestOversizedStringAttributeIsOmitted(t *testing.T) {
	var buf bytes.Buffer
	wrapped(&buf).Info("event", "body", strings.Repeat("x", 2<<20))
	require.Less(t, buf.Len(), 512)
	require.Contains(t, buf.String(), `"body":"[OMITTED:oversize:string]"`)
}

type hugeError struct{ text string }

func (e hugeError) Error() string { return e.text }

// TestOversizedErrorIsMeasuredByItsErrorText mirrors how slog renders an
// error that is not a json.Marshaler: as its Error() string, not its fields.
func TestOversizedErrorIsMeasuredByItsErrorText(t *testing.T) {
	var buf bytes.Buffer
	wrapped(&buf).Info("event", "err", hugeError{text: strings.Repeat("e", 2<<20)})
	require.Less(t, buf.Len(), 512)
	require.Contains(t, buf.String(), `"err":"[OMITTED:oversize:logging_test.hugeError]"`)
}

// TestValueJustUnderTheBoundIsUntouched guards the narrowness invariant at
// the edge that matters: a large-but-legal value must render byte-identically
// to an unwrapped logger, not be omitted. 900 KiB of ASCII is well within the
// 1 MiB bound once JSON quoting is counted.
func TestValueJustUnderTheBoundIsUntouched(t *testing.T) {
	big := untaggedDAG{Label: strings.Repeat("a", 900<<10)}
	for name, v := range map[string]any{"struct": big, "string": big.Label, "error": errors.New(big.Label)} {
		t.Run(name, func(t *testing.T) {
			var withTag, without bytes.Buffer
			wrapped(&withTag).Info("event", "v", v)
			plain(&without).Info("event", "v", v)
			require.Equal(t, without.String(), withTag.String())
		})
	}
}

// TestOversizeIsCaughtInsideGroupsWithAttrsAndLogValuers: every route an
// attribute can take to the handler is bounded, not just top-level Any.
func TestOversizeIsCaughtInsideGroupsWithAttrsAndLogValuers(t *testing.T) {
	dag := buildUntaggedDAG(40)
	routes := map[string]func(l *slog.Logger){
		"group":     func(l *slog.Logger) { l.Info("event", slog.Group("g", "graph", dag)) },
		"with":      func(l *slog.Logger) { l.With("graph", dag).Info("event") },
		"logvaluer": func(l *slog.Logger) { l.Info("event", "graph", dagValuer{dag}) },
	}
	for name, emit := range routes {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			emit(wrapped(&buf))
			require.Less(t, buf.Len(), 512)
			require.Contains(t, buf.String(), "[OMITTED:oversize:*logging_test.untaggedDAG]")
		})
	}
}

type dagValuer struct{ d *untaggedDAG }

func (v dagValuer) LogValue() slog.Value { return slog.AnyValue(v.d) }

// BenchmarkLogUntaggedStruct measures what every ordinary log call now pays
// for the pre-flight walk: an untagged, realistically sized struct.
func BenchmarkLogUntaggedStruct(b *testing.B) {
	v := make([]untaggedStruct, 50)
	for i := range v {
		v[i] = untaggedStruct{A: "a", B: i, C: []string{"x", "y"}, D: map[string]string{"k": "v"}}
	}
	l := wrapped(io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		l.Info("event", "v", v, "request_id", "r-1")
	}
}
