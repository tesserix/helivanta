package logging_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/logging"
)

func TestRedactPatterns(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"aadhaar bare", "id 123456789012 end", "id [REDACTED:aadhaar] end"},
		{"aadhaar spaced", "id 1234 5678 9012 end", "id [REDACTED:aadhaar] end"},
		{"aadhaar hyphenated", "id 1234-5678-9012 end", "id [REDACTED:aadhaar] end"},
		{"abha 14 digits", "abha 12345678901234 end", "abha [REDACTED:abha] end"},
		{"abha hyphenated", "abha 12-3456-7890-1234 end", "abha [REDACTED:abha] end"},
		{"mobile plus91", "call +919876543210 now", "call [REDACTED:mobile] now"},
		{"mobile plus91 spaced", "call +91 9876543210 now", "call [REDACTED:mobile] now"},
		{"mobile bare 10 digit", "call 9876543210 now", "call [REDACTED:mobile] now"},
		{"mobile 5-5 grouping", "call 98765 43210 now", "call [REDACTED:mobile] now"},
		{"mobile plus91 5-5", "call +91 98765 43210 now", "call [REDACTED:mobile] now"},
		{"mobile starting five", "call 5876543210 now", "call [REDACTED:mobile] now"},
		{"two values in one string", "a 123456789012 b 9876543210", "a [REDACTED:aadhaar] b [REDACTED:mobile]"},
		// Two matches of the SAME pattern sharing one separator. The first
		// match consumes the space, so a single pass would leave the second
		// number in the clear — an entirely ordinary thing to log.
		{"adjacent mobiles", "9876543210 9876543211", "[REDACTED:mobile] [REDACTED:mobile]"},
		{"three adjacent mobiles", "9876543210 9876543211 9876543212",
			"[REDACTED:mobile] [REDACTED:mobile] [REDACTED:mobile]"},
		// Non-matches must survive untouched, or every log line becomes noise.
		{"short number", "count 12345", "count 12345"},
		{"mobile-length starting four", "code 4876543210", "code 4876543210"},
		{"iso timestamp untouched", "at 2026-08-13T01:02:03Z", "at 2026-08-13T01:02:03Z"},
		{"plain prose", "patient admitted", "patient admitted"},
		// The regression that would silently destroy correlation: a UUID
		// contains a boundary-delimited twelve-digit run, so a \b-anchored
		// Aadhaar pattern masks every tenant_id in every log line.
		{"uuid untouched", "t 11111111-1111-1111-1111-111111111111", "t 11111111-1111-1111-1111-111111111111"},
		{"bare uuid untouched", "11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111"},
		{"digits inside a longer token untouched", "ref_9876543210_x", "ref_9876543210_x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, n := logging.RedactString(tc.in)
			require.Equal(t, tc.want, got)
			if tc.in == tc.want {
				require.Zero(t, n, "a clean string must not increment the counter")
			} else {
				require.Positive(t, n)
			}
		})
	}
}

// A 14-digit ABHA must come out as an ABHA, not as an Aadhaar that chewed
// twelve of its digits.
func TestLongerIdentifiersWinOverShorterOnes(t *testing.T) {
	got, _ := logging.RedactString("12345678901234")
	require.Equal(t, "[REDACTED:abha]", got)
}

// +919876543210 contains a run of exactly twelve digits. If Aadhaar is tried
// before the international mobile form, the number is masked under the wrong
// label — still safe, but the marker is supposed to say what fired.
func TestInternationalMobileIsNotMistakenForAnAadhaar(t *testing.T) {
	got, _ := logging.RedactString("call +919876543210")
	require.Equal(t, "call [REDACTED:mobile]", got)
}

// through writes one line through a redacting writer and returns the output.
func through(t *testing.T, line string) string {
	t.Helper()
	var buf bytes.Buffer
	w := logging.NewRedactingWriter(&buf)
	n, err := w.Write([]byte(line))
	require.NoError(t, err)
	// io.Writer's contract: a successful Write reports len(p), whatever the
	// wrapped writer received. Returning the post-redaction length would make
	// callers believe a short write occurred.
	require.Equal(t, len(line), n, "Write must report the input length")
	return buf.String()
}

// requireValidJSON is the assertion that keeps the number-token handling
// honest. Substituting a bare marker for a JSON number produces syntactically
// invalid output, and slog would replace the whole record with !ERROR — a
// redaction control that silently deletes log records.
func requireValidJSON(t *testing.T, line string) map[string]any {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &parsed),
		"output is not valid JSON: %s", line)
	return parsed
}

func TestWriterRedactsInsideStringValues(t *testing.T) {
	out := through(t, `{"msg":"call 9876543210 now"}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "call [REDACTED:mobile] now", parsed["msg"])
}

// A bare JSON number is the case that produces invalid output if the marker
// is substituted unquoted.
func TestWriterQuotesTheMarkerWhenTheMatchIsANumberToken(t *testing.T) {
	out := through(t, `{"aadhaar":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:aadhaar]", parsed["aadhaar"],
		"a masked number must become a JSON string, not a bare token")
}

func TestWriterHandlesNumberAndStringOnTheSameLine(t *testing.T) {
	out := through(t, `{"n":9876543210,"s":"call 9876543211"}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:mobile]", parsed["n"])
	require.Equal(t, "call [REDACTED:mobile]", parsed["s"])
}

func TestWriterRedactsKeysAsWellAsValues(t *testing.T) {
	out := through(t, `{"9876543210":"v"}`+"\n")
	parsed := requireValidJSON(t, out)
	_, raw := parsed["9876543210"]
	require.False(t, raw, "the raw key survived: %s", out)
	require.Equal(t, "v", parsed["[REDACTED:mobile]"])
}

// Correlation must survive redaction. A tenant_id is a UUID and a request_id
// may be one too; masking either destroys the feature Part C exists for.
func TestWriterLeavesCorrelationFieldsAlone(t *testing.T) {
	const tenantID = "11111111-1111-1111-1111-111111111111"
	out := through(t, `{"tenant_id":"`+tenantID+`","count":42,"ok":true}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, tenantID, parsed["tenant_id"])
	require.Equal(t, float64(42), parsed["count"])
	require.Equal(t, true, parsed["ok"])
}

// An escaped quote inside a string must not be mistaken for the string's end,
// or the in-string tracking desynchronises and every following number token
// is misclassified.
func TestWriterTracksEscapedQuotes(t *testing.T) {
	out := through(t, `{"msg":"he said \"9876543210\" loudly","n":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, `he said "[REDACTED:mobile]" loudly`, parsed["msg"])
	require.Equal(t, "[REDACTED:aadhaar]", parsed["n"])
}

// A trailing backslash before the closing quote is an escaped backslash, not
// an escaped quote — the classic off-by-one in this kind of scanner.
func TestWriterTracksEscapedBackslashes(t *testing.T) {
	out := through(t, `{"msg":"path\\","n":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, `path\`, parsed["msg"])
	require.Equal(t, "[REDACTED:aadhaar]", parsed["n"])
}

func TestWriterPassesCleanLinesThroughByteForByte(t *testing.T) {
	const line = `{"time":"2026-08-13T01:02:03Z","level":"INFO","msg":"ok","ward":"ward-3"}` + "\n"
	require.Equal(t, line, through(t, line), "a line with no PHI must be untouched")
}

func TestWriterCountsRedactions(t *testing.T) {
	before := logging.RedactionCount()
	through(t, `{"a":"9876543210","b":"123456789012"}`+"\n")
	require.Equal(t, before+2, logging.RedactionCount())
}
