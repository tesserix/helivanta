package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/logging"
)

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in    string
		want  slog.Level
		valid bool
	}{
		{"debug", slog.LevelDebug, true},
		{"DEBUG", slog.LevelDebug, true},
		{"  info  ", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"", slog.LevelInfo, false},
		{"lodebug", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := logging.ParseLevel(tc.in)
			require.Equal(t, tc.valid, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewEmitsJSONWithStandardFields(t *testing.T) {
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("hello", "k", "v")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "output must be one JSON object per line")
	require.Equal(t, "INFO", line["level"])
	require.Equal(t, "hello", line["msg"])
	require.Equal(t, "v", line["k"])
	require.NotEmpty(t, line["time"])
}

func TestNewRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "warn")
	l.Info("suppressed")
	require.Empty(t, buf.String(), "info must be below the configured warn threshold")
	l.Warn("emitted")
	require.Contains(t, buf.String(), "emitted")
}

// An unrecognised LOG_LEVEL degrades to info with a warning rather than
// refusing to boot. A hospital's API must not fail to start over a typo in a
// log level — the opposite of the HMS_ENV guards, where a wrong value would
// disable tenant isolation.
func TestUnrecognisedLevelFallsBackToInfoAndWarns(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "verbose")
	require.Contains(t, buf.String(), "unrecognised LOG_LEVEL")
	require.Contains(t, buf.String(), "verbose", "the rejected value belongs in the warning")

	buf.Reset()
	l.Info("still logging")
	require.Contains(t, buf.String(), "still logging", "must degrade to info, not to silence")
}

// An empty LOG_LEVEL is the ordinary unset case, not a mistake, so it must
// not produce a warning on every boot.
func TestEmptyLevelIsSilentlyInfo(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "")
	require.Empty(t, buf.String())
	l.Info("x")
	require.Contains(t, buf.String(), "x")
	require.False(t, strings.Contains(buf.String(), "unrecognised"))
}

// The wiring test: pkg/logging's own constructor must produce a redacting
// logger. Every unit in redact_test.go can pass while New still hands out a
// bare JSON handler, and that gap is the whole bug.
func TestNewRedacts(t *testing.T) {
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("ok", "phone", "9876543210")
	require.Contains(t, buf.String(), "[REDACTED:mobile]")
	require.NotContains(t, buf.String(), "9876543210")
}

// Part D must not eat Part C. The correlation fields pass through the same
// redaction as everything else, and a tenant_id UUID contains a
// boundary-delimited twelve-digit run — so a careless Aadhaar pattern would
// mask the very field that makes an incident traceable.
func TestCorrelationFieldsSurviveRedaction(t *testing.T) {
	const tenantID = "11111111-1111-1111-1111-111111111111"
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").
		With("request_id", "req-abc", "tenant_id", tenantID, "subject", "gip-uid-42").
		Info("handled")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "raw: %s", buf.String())
	require.Equal(t, "req-abc", line["request_id"])
	require.Equal(t, tenantID, line["tenant_id"], "redaction destroyed the tenant correlation field")
	require.Equal(t, "gip-uid-42", line["subject"])
}

// Whatever slog renders a value as, the writer sees the final bytes — which
// is the entire reason redaction lives there. These are the shapes that
// defeated the previous handler-level design; each must come out masked and
// each line must still parse as JSON.
func TestNewRedactsEveryRenderingSlogProduces(t *testing.T) {
	phone := "9876543210"
	for _, tc := range []struct {
		name string
		emit func(l *slog.Logger)
	}{
		{"pointer field in a struct", func(l *slog.Logger) {
			l.Info("m", "k", struct{ Phone *string }{&phone})
		}},
		{"slice of pointers", func(l *slog.Logger) {
			l.Info("m", "k", []*struct{ Phone string }{{Phone: phone}})
		}},
		{"map of pointers", func(l *slog.Logger) {
			l.Info("m", "k", map[string]*string{"a": &phone})
		}},
		{"float64", func(l *slog.Logger) { l.Info("m", "aadhaar", float64(123456789012)) }},
		{"int64", func(l *slog.Logger) { l.Info("m", "aadhaar", int64(123456789012)) }},
		{"wrapped error", func(l *slog.Logger) {
			l.Error("m", "err", fmt.Errorf("saving: %w", errors.New("dup mobile "+phone)))
		}},
		{"error with a divergent Stringer", func(l *slog.Logger) {
			l.Error("m", "err", errDivergentStringer{})
		}},
		{"json.Marshaler over unexported state", func(l *slog.Logger) {
			l.Info("m", "k", marshalerOverUnexported{phone: phone})
		}},
		{"encoding.TextMarshaler over unexported state", func(l *slog.Logger) {
			l.Info("m", "k", textMarshalerOverUnexported{phone: phone})
		}},
		{"numeric marshaller", func(l *slog.Logger) { l.Info("m", "k", big.NewInt(9876543210)) }},
		{"group", func(l *slog.Logger) {
			l.Info("m", slog.Group("g", slog.String("a", "123456789012")))
		}},
		{"message text", func(l *slog.Logger) { l.Info("lookup failed for " + phone) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.emit(logging.NewWithWriter(&buf, "info"))
			require.NotContains(t, buf.String(), phone, "raw: %s", buf.String())
			require.NotContains(t, buf.String(), "123456789012", "raw: %s", buf.String())
			require.Contains(t, buf.String(), "[REDACTED:", "raw: %s", buf.String())
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed),
				"redaction produced invalid JSON: %s", buf.String())
			require.NotContains(t, buf.String(), "!ERROR",
				"redaction destroyed the record: %s", buf.String())
		})
	}
}

type errDivergentStringer struct{}

func (errDivergentStringer) Error() string  { return "failed for patient 9876543210" }
func (errDivergentStringer) String() string { return "failed" }

type marshalerOverUnexported struct{ phone string }

func (m marshalerOverUnexported) MarshalJSON() ([]byte, error) {
	return []byte(`{"phone":"` + m.phone + `"}`), nil
}

type textMarshalerOverUnexported struct{ phone string }

func (m textMarshalerOverUnexported) MarshalText() ([]byte, error) {
	return []byte("p " + m.phone), nil
}

// A shared logger is used from every request goroutine at once.
func TestNewIsSafeForConcurrentUse(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	l := logging.NewWithWriter(&lockedWriter{w: &buf, mu: &mu}, "info")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Info("ok", "phone", "9876543210", "tenant_id", "11111111-1111-1111-1111-111111111111")
			}
		}()
	}
	wg.Wait()
	require.NotContains(t, buf.String(), "9876543210")
	require.Contains(t, buf.String(), "11111111-1111-1111-1111-111111111111")
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}
