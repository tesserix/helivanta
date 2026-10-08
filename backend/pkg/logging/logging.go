// Package logging builds the process logger: JSON to stdout, level from
// LOG_LEVEL, every attribute passed through PHI redaction on the way out.
//
// WHAT IS SCREENED, AND WHAT IS NOT (#840). Two layers screen PHI: fields
// tagged `phi:` on the way in, and Aadhaar, ABHA and mobile SHAPES in the
// bytes on the way out. Neither detects SECRETS. Private keys, seeds, tokens
// and passwords are not PHI shapes, so they pass through untouched. Keeping
// them out of logs is the caller's job and a separate control, not something
// this package does.
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
	// Two redaction layers, in the only order that works. The tag handler runs
	// first, while attribute values are still Go values and a struct tag is
	// still visible; the JSON handler then serialises what it produced, and the
	// redacting writer screens the resulting bytes for PHI *shapes* (Aadhaar,
	// ABHA, mobile) that no tag could have declared. Reversing them would put
	// the tag layer behind the serialisation, where the tags no longer exist.
	handler := NewPHITagHandler(slog.NewJSONHandler(NewRedactingWriter(w), &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: durationsAsText,
	}))
	l := slog.New(handler)
	if !ok && strings.TrimSpace(level) != "" {
		// Warn rather than fail: a mistyped log level cannot compromise
		// tenant isolation, and a hospital's API should not refuse to boot
		// over one. This is deliberately the opposite call from the HELIVANTA_ENV
		// guards, where a wrong value silently disables safety checks.
		l.Warn("unrecognised LOG_LEVEL; defaulting to info", "value", level)
	}
	return l
}

// durationsAsText renders every time.Duration attribute as its String() form
// ("15m0s") instead of slog's default nanosecond integer (#840).
//
// The integer was unreadable, and it was also the redactor's commonest false
// positive: 15m is 900000000000, the shape of an Aadhaar; 24h is
// 86400000000000, the shape of an ABHA; 5s is 5000000000, the shape of a
// mobile. Call sites used to have to remember `.String()`, and #838 forgot it
// twice. Doing it here, at the only handler this process builds, means no call
// site is involved. slog calls ReplaceAttr after resolving LogValuers and for
// attributes nested in groups, so every duration passed as an attribute is
// covered. A duration inside a struct that slog marshals as JSON is not; that
// residual is stated in the #840 spec.
func durationsAsText(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindDuration {
		a.Value = slog.StringValue(a.Value.Duration().String())
	}
	return a
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
