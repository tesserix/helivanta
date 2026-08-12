// Package logging builds the process logger: JSON to stdout, level from
// LOG_LEVEL, every attribute passed through PHI redaction on the way out.
//
// It is the one place a *slog.Logger is constructed. Handlers and modules
// take the request-scoped logger from internal/platform/requestid instead,
// which is this logger pre-bound with request_id, tenant_id and subject.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns the process logger, writing JSON to stdout.
func New(level string) *slog.Logger {
	return NewWithWriter(os.Stdout, level)
}

// NewWithWriter is New with the destination injected, so tests can capture
// output without touching os.Stdout.
func NewWithWriter(w io.Writer, level string) *slog.Logger {
	lvl, ok := ParseLevel(level)
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	l := slog.New(handler)
	if !ok && strings.TrimSpace(level) != "" {
		// Warn rather than fail: a mistyped log level cannot compromise
		// tenant isolation, and a hospital's API should not refuse to boot
		// over one. This is deliberately the opposite call from the HMS_ENV
		// guards, where a wrong value silently disables safety checks.
		l.Warn("unrecognised LOG_LEVEL; defaulting to info", "value", level)
	}
	return l
}

// ParseLevel maps a LOG_LEVEL string to a slog.Level. The bool reports
// whether the input was recognised; an unrecognised or empty value yields
// slog.LevelInfo with ok=false, and the caller decides whether that warrants
// a warning (an empty value is the ordinary unset case, not a mistake).
func ParseLevel(level string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
