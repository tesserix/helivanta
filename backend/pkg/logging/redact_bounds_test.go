package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

// logOne emits one record through the process logger and returns its
// decoded JSON object.
func logOne(t *testing.T, args ...any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("probe", args...)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec), "line: %s", buf.String())
	return rec
}

// #840 D1: a duration is rendered as text, not as nanoseconds, so it is
// readable AND never reaches the redactor as an Aadhaar-, ABHA- or
// mobile-shaped integer.
func TestDurationsRenderAsText(t *testing.T) {
	rec := logOne(t,
		"ttl", 15*time.Minute, // 900000000000 ns: Aadhaar-shaped
		"day", 24*time.Hour, // 86400000000000 ns: ABHA-shaped
		"tick", 5*time.Second, // 5000000000 ns: mobile-shaped
		slog.Group("cfg", slog.Duration("idle", 15*time.Minute)),
	)
	require.Equal(t, "15m0s", rec["ttl"])
	require.Equal(t, "24h0m0s", rec["day"])
	require.Equal(t, "5s", rec["tick"])
	require.Equal(t, map[string]any{"idle": "15m0s"}, rec["cfg"])
}

// #840 D2: 12-digit runs that cannot be an Aadhaar are left readable. Each
// case isolates one rule, so dropping either rule fails its own case.
func TestTwelveDigitNumbersThatCannotBeAadhaarAreNotMasked(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"15 minutes in nanoseconds (fails the check digit)", "900000000000"},
		{"starts 2-9 but fails the check digit", "234567890125"},
		{"passes the check digit but starts with 1", "123456789014"},
		{"passes the check digit but starts with 0", "023456789013"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, n := logging.RedactString("bytes " + tc.in + " end")
			require.Equal(t, "bytes "+tc.in+" end", got)
			require.Zero(t, n)
		})
	}

	// And as the JSON number slog emits for an int64 attribute.
	rec := logOne(t, "rows", int64(900000000000))
	require.Equal(t, float64(900000000000), rec["rows"])
}

// #840 D2, the other direction: the loosening must not go too far. These are
// synthetic, Verhoeff-valid, 2-9-leading numbers, i.e. exactly what an
// Aadhaar looks like, and every form of them stays masked.
func TestNumbersThatCanBeAadhaarAreStillMasked(t *testing.T) {
	for _, n := range []string{"234567890124", "987654321096", "500000000006", "900000000002"} {
		spaced := n[0:4] + " " + n[4:8] + " " + n[8:12]
		hyphen := n[0:4] + "-" + n[4:8] + "-" + n[8:12]
		for _, form := range []string{n, spaced, hyphen} {
			got, k := logging.RedactString("id " + form + " end")
			require.Equal(t, "id [REDACTED:aadhaar] end", got, "form %q", form)
			require.Equal(t, 1, k)
		}
	}

	// As a JSON number, the shape an Aadhaar held in an integer field takes.
	var asInt int64 = 234567890124
	rec := logOne(t, "aadhaar", asInt)
	require.Equal(t, "[REDACTED:aadhaar]", rec["aadhaar"])
}

// #840 D3: a declined candidate must not hide the real one beside it. With
// the old replace-to-fixpoint loop, the declined 234567890125 matched on every
// pass and consumed the shared space, so the valid Aadhaar after it was never
// seen.
func TestRedactStringMasksAValidAadhaarRightAfterADeclinedOne(t *testing.T) {
	got, n := logging.RedactString("234567890125 234567890124")
	require.Equal(t, "234567890125 [REDACTED:aadhaar]", got)
	require.Equal(t, 1, n)

	got, n = logging.RedactString("a,900000000000,234567890124,123456789014,987654321096")
	require.Equal(t, "a,900000000000,[REDACTED:aadhaar],123456789014,[REDACTED:aadhaar]", got)
	require.Equal(t, 2, n)
}
