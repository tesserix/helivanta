package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
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
