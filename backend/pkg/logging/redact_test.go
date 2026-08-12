package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
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
// invalid output, and nothing downstream of this writer validates that: the
// malformed line reaches stdout as-written and an ingest pipeline expecting
// one JSON object per line drops it — a redaction control that silently
// deletes log records.
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

// Round 1 fix: C1 — PHI immediately after a JSON escape sequence leaked. A
// byte-scanning writer sees the raw bytes `\` `n` before a newline, and `n`
// is a word character, so the boundary check reads it as "part of a longer
// token" and declines to match. errors.Join — the standard library's own
// multi-error type — joins with `\n`, so this is ordinary Go, not a
// contrived input. Decoding the string first turns `\n` into an actual
// newline before the boundary check ever runs, and a newline is not a word
// character, so it is a valid boundary. Tabs and `\uXXXX` escapes fail the
// same way on a scanner and are fixed the same way by decoding first.
func TestWriterRedactsPHIAfterJSONEscapeSequences(t *testing.T) {
	// The exact shape of the defect: errors.Join joins with "\n".
	joined := errors.Join(errors.New("bad record"), errors.New("9876543210 rejected"))
	line, err := json.Marshal(map[string]string{"err": joined.Error()})
	require.NoError(t, err)
	out := through(t, string(line)+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "bad record\n[REDACTED:mobile] rejected", parsed["err"])

	newlineCase := "{\"msg\":\"bad record\\n9876543210 rejected\"}\n"
	tabCase := "{\"msg\":\"bad record\\t9876543210 rejected\"}\n"
	//   decodes to an ordinary space, but a byte scanner sees the raw
	// hex digit '0' from the escape sequence immediately before the PHI
	// digits and reads them as one long, boundary-free run.
	unicodeCase := "{\"msg\":\"bad record\\u00209876543210 rejected\"}\n"

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"newline before PHI", newlineCase, "bad record\n[REDACTED:mobile] rejected"},
		{"tab before PHI", tabCase, "bad record\t[REDACTED:mobile] rejected"},
		{"unicode escape before PHI", unicodeCase, "bad record [REDACTED:mobile] rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := through(t, tc.in)
			parsed := requireValidJSON(t, out)
			require.Equal(t, tc.want, parsed["msg"])
		})
	}
}

// Round 1 fix: C2 — a match only ever covers a contiguous digit run, so
// substituting a marker for part of a fractional or signed number produces
// invalid JSON: `{"amount":9876543210.75}` became
// `{"amount":"[REDACTED:mobile]".75}`. Decoding hands the whole number
// literal — sign, integer, fraction, exponent — to RedactString as one
// token, and the result is re-encoded as a single JSON value, so there is no
// partial substitution left to corrupt the document.
func TestWriterHandlesFractionalAndSignedNumberTokens(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		key  string
	}{
		{"fractional with PHI integer part", `{"amount":9876543210.75}` + "\n", "amount"},
		{"small integer part, PHI fraction", `{"v":12.9876543210}` + "\n", "v"},
		{"zero integer part, PHI fraction", `{"v":0.9876543210}` + "\n", "v"},
		{"negative PHI-shaped number", `{"a":-9876543210}` + "\n", "a"},
		{"PHI-shaped fraction longer than any pattern", `{"v":9876543210.123456789012}` + "\n", "v"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := through(t, tc.in)
			parsed := requireValidJSON(t, out)
			got, ok := parsed[tc.key].(string)
			require.True(t, ok, "a redacted number must become a JSON string: %s", out)
			require.NotContains(t, got, "9876543210", "raw PHI digits must not survive")
			require.Contains(t, got, "[REDACTED:", "the field must show a redaction marker")
		})
	}
}

// The negative case specifically: bounded() requires the digit run to sit at
// the very start of the text it is given, or after a non-token character.
// Screening the whole signed literal "-9876543210" as one string would find
// '-' sitting immediately before the digits — itself a token character by
// bounded()'s own rule — and refuse to match, silently leaving every
// negative PHI-shaped number in the clear. The sign must be split off first.
func TestWriterRedactsNegativeNumbers(t *testing.T) {
	out := through(t, `{"a":-9876543210,"b":-123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:mobile]", parsed["a"])
	require.Equal(t, "[REDACTED:aadhaar]", parsed["b"])
}

// A digit run of ten or more is not automatically PHI: an ordinary long
// integer that does not match any pattern's exact length or leading-digit
// requirement must survive the slow (parsing) path untouched, the same way
// it survives RedactString directly.
func TestWriterSlowPathLeavesOrdinaryLongNumbersAlone(t *testing.T) {
	const line = `{"count":1234567890,"ref":"invoice-0000000001"}` + "\n"
	out := through(t, line)
	parsed := requireValidJSON(t, out)
	require.Equal(t, float64(1234567890), parsed["count"])
	require.Equal(t, "invoice-0000000001", parsed["ref"])
}

// Round 2 fix — NEW CRITICAL: redactJSONByParsing only re-encoded the JSON
// value it parsed and silently dropped whatever trailed it, which for every
// real slog write is the "\n" that terminates the record. In a
// newline-delimited pipeline that does not corrupt one record, it corrupts
// two: the redacted record and whatever the next Write appends immediately
// after it, spliced onto the same physical line. This asserts the redacted
// output ends with exactly the terminator the input had, for a few different
// terminator shapes — not just the single "\n" every other test in this file
// happens to use, since nothing about the writer's contract promises that
// specific byte.
func TestWriterPreservesTrailingTerminator(t *testing.T) {
	for _, tc := range []struct {
		name       string
		terminator string
	}{
		{"single newline", "\n"},
		{"no terminator", ""},
		{"double newline", "\n\n"},
		{"newline then spaces", "\n  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := `{"phone":"9876543210"}` + tc.terminator
			out := through(t, line)
			require.True(t, strings.HasSuffix(out, tc.terminator),
				"expected output to end with %q, got %q", tc.terminator, out)
			// And the redaction itself must still have happened — this is
			// not a test that redaction was skipped to preserve the suffix.
			require.Contains(t, out, "[REDACTED:mobile]")
		})
	}
}

// Round 2 fix — the same NEW CRITICAL, checked the way it actually manifests
// in production: multiple records written back to back through the same
// writer, the way slog's JSON handler calls Write once per record. N records
// in must produce N physical (newline-terminated) lines out, and each one
// must independently be the correct, valid-JSON record — not N-1 lines with
// two records glued onto one of them. This deliberately does NOT TrimSpace
// anywhere, since trimming is exactly what let this defect hide from every
// other test in this file.
func TestWriterPreservesRecordCountAcrossMultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	w := logging.NewRedactingWriter(&buf)

	records := []string{
		`{"msg":"a","phone":"9876543210"}` + "\n", // redacted
		`{"msg":"b","ward":"ward-3"}` + "\n",      // clean
		`{"msg":"c","ward":"ward-4"}` + "\n",      // clean
	}
	for _, r := range records {
		n, err := w.Write([]byte(r))
		require.NoError(t, err)
		require.Equal(t, len(r), n)
	}

	out := buf.String()
	require.True(t, strings.HasSuffix(out, "\n"), "output must end with a newline: %q", out)
	// Split on "\n" and drop the trailing empty element from the final
	// terminator, rather than TrimSpace-ing the whole buffer first.
	lines := strings.Split(out, "\n")
	require.Equal(t, "", lines[len(lines)-1], "output must end exactly at a newline, not mid-record")
	lines = lines[:len(lines)-1]
	require.Len(t, lines, len(records), "N records in must produce N physical lines out:\n%s", out)

	var msgA, msgB, msgC map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &msgA))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &msgB))
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &msgC))
	require.Equal(t, "[REDACTED:mobile]", msgA["phone"])
	require.Equal(t, "b", msgB["msg"])
	require.Equal(t, "c", msgC["msg"])
}

// Round 2 fix — CRITICAL, still open after round 1: the prefilter ran on the
// raw ESCAPED bytes, and every core pattern allows `[-\s]` as a separator
// between digit groups. `\s` matches a real tab/newline/CR — exactly what
// JSON escapes as `\t`/`\n`/`\r` — so PHI whose separator happens to be an
// escape sequence sailed through the old prefilter untouched, even though
// RedactString on the DECODED text would have caught it immediately. This is
// the fix's own motivating input from the coordinator's report:
// errors.Join(errors.New("otp 98765"), errors.New("43210 failed")).
func TestWriterRedactsPHISeparatedByEscapedWhitespace(t *testing.T) {
	joined := errors.Join(errors.New("otp 98765"), errors.New("43210 failed"))
	line, err := json.Marshal(map[string]string{"err": joined.Error()})
	require.NoError(t, err)
	out := through(t, string(line)+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "otp [REDACTED:mobile] failed", parsed["err"])

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"mobile split by escaped newline", "{\"e\":\"98765\\n43210\"}\n", "[REDACTED:mobile]"},
		{"aadhaar split by escaped tabs", "{\"e\":\"1234\\t5678\\t9012\"}\n", "[REDACTED:aadhaar]"},
		{"mobile split by escaped CR", "{\"e\":\"98765\\r43210\"}\n", "[REDACTED:mobile]"},
		// Control: the same value with a literal (unescaped) space
		// separator, which the old prefilter always handled correctly —
		// included so a regression in the escaped cases above is not
		// confused with a regression in the ordinary case.
		{"mobile split by literal space (control)", `{"e":"98765 43210"}` + "\n", "[REDACTED:mobile]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := through(t, tc.in)
			parsed := requireValidJSON(t, out)
			got, _ := parsed["e"].(string)
			require.Equal(t, tc.want, got)
		})
	}
}

// Round 2 fix (Minor) — re-encoding via json.Marshal escapes `<`, `>` and
// `&`, which slog's own encoder does not. Semantically identical, but
// byte-level and grep-based log tooling sees a redacted line render a value
// differently than a clean line renders the same value. Forces the slow
// path via a co-located PHI value, since the fast path never touches
// encoding at all.
func TestWriterDoesNotHTMLEscapeRedactedStrings(t *testing.T) {
	out := through(t, `{"phone":"9876543210","note":"a<b>c&d"}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:mobile]", parsed["phone"])
	require.Equal(t, "a<b>c&d", parsed["note"])
	// The raw characters must appear literally in the output bytes.
	// json.Marshal's default HTML-escaping would instead have produced the
	// six-character escape sequences below, which slog's own encoder never
	// emits — check for their absence specifically, not for the absence of
	// the characters themselves, since the raw characters are supposed to
	// be there.
	require.Contains(t, out, "a<b>c&d", "the raw, unescaped substring must appear literally in the output")
	require.NotContains(t, out, "\\u003c", "'<' must not be HTML-escaped")
	require.NotContains(t, out, "\\u003e", "'>' must not be HTML-escaped")
	require.NotContains(t, out, "\\u0026", "'&' must not be HTML-escaped")
}

// shortWriter accepts fewer bytes than it is given but reports no error,
// simulating an inner writer's short-write behaviour so the writer's own
// handling of that case (returning io.ErrShortWrite rather than claiming a
// full, successful write) has a test.
type shortWriter struct{ accept int }

func (s *shortWriter) Write(p []byte) (int, error) {
	n := s.accept
	if n > len(p) {
		n = len(p)
	}
	return n, nil
}

func TestWriterReturnsShortWriteErrorWhenInnerWriterTruncates(t *testing.T) {
	inner := &shortWriter{accept: 3}
	w := logging.NewRedactingWriter(inner)
	n, err := w.Write([]byte(`{"ward":"ward-3"}` + "\n"))
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Zero(t, n, "a short write must not report any bytes accepted")
}

// The slow (parse-and-re-encode) path must not reorder object keys — log
// tooling and humans alike expect fields to appear in the order the caller
// (or slog's attribute ordering) produced them.
func TestWriterSlowPathPreservesKeyOrder(t *testing.T) {
	// A PHI value forces the slow path; the object has enough fields that an
	// accidental reordering (e.g. from routing keys through a Go map) would
	// be visible.
	const line = `{"z_first":"9876543210","a_second":2,"m_third":"three","b_fourth":true}` + "\n"
	out := through(t, line)
	requireValidJSON(t, out)

	order := []string{"z_first", "a_second", "m_third", "b_fourth"}
	last := -1
	for _, key := range order {
		idx := strings.Index(out, `"`+key+`"`)
		require.GreaterOrEqual(t, idx, 0, "key %q missing from output: %s", key, out)
		require.Greater(t, idx, last, "key %q appeared out of order in %s", key, out)
		last = idx
	}
}

// Round 2 fix — the empirical half of the "run of four consecutive digits is
// a safe superset" argument: for a range of decoded strings that place PHI
// behind separators of every kind this fix set out to catch — a literal
// space (the control case the old prefilter already got right), and the
// escaped tab/newline/CR forms C1's second half missed — confirm end to end
// that the writer actually redacts them, i.e. that hasFourConsecutiveDigits
// (evaluated on the JSON-encoded wire bytes) never gates out a value that
// RedactString (evaluated on the decoded text) would have changed. This is
// the cross-representation check the coordinator's report says the earlier
// review should have run: comparing the gate on the ENCODED form against
// RedactString on the DECODED form, not comparing either against itself.
func TestGateNeverRejectsWhatRedactStringWouldChange(t *testing.T) {
	separators := []string{" ", "-", "\n", "\t", "\r", ""}
	prefixes := []string{"", "otp ", "call ", "id="}
	suffixes := []string{"", " failed", " now", ".end"}
	values := []string{"98765" /* +5 more digits below */}

	for _, sep := range separators {
		for _, prefix := range prefixes {
			for _, suffix := range suffixes {
				decoded := prefix + values[0] + sep + "43210" + suffix
				wantChanged, n := logging.RedactString(decoded)
				_ = wantChanged
				if n == 0 {
					// Not every prefix/suffix/separator combination is a
					// genuine match (e.g. a prefix ending in a token
					// character can push the candidate out of bounds()) —
					// only assert the gate on cases RedactString itself
					// agrees changed something.
					continue
				}

				line, err := json.Marshal(map[string]string{"v": decoded})
				require.NoError(t, err)

				out := through(t, string(line)+"\n")
				parsed := requireValidJSON(t, out)
				got, _ := parsed["v"].(string)
				require.NotEqual(t, decoded, got,
					"gate let a PHI-bearing value (decoded=%q, encoded=%s) through unredacted", decoded, line)
			}
		}
	}
}

// requireParsesAsJSON is requireValidJSON's generic sibling for the property
// test, where the top-level document is not always an object — a randomly
// generated line may be a bare string, array, or number just as validly.
func requireParsesAsJSON(t *testing.T, line string) {
	t.Helper()
	var parsed any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &parsed),
		"output is not valid JSON: %s", line)
}

// requireNoRawDigitRun fails the test if s contains a run of ten or more
// consecutive ASCII digits anywhere outside a `[REDACTED:...]` marker. Used
// by the property test as a structure-agnostic backstop: whatever shape the
// generated document took, no PHI-length digit run may survive in the
// clear.
func requireNoRawDigitRun(t *testing.T, s string) {
	t.Helper()
	run := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			run++
			if run >= 10 {
				t.Fatalf("raw digit run of length >= 10 survived redaction: %q in %q", s[i-run+1:i+1], s)
			}
			continue
		}
		run = 0
	}
}

// TestWriterPropertyOverGeneratedLines is the permanent home of the property
// test that found C2: generate a large number of realistic log lines via
// encoding/json from assorted, randomly shaped values — nested objects and
// arrays, unicode, empty strings, PHI in both keys and values, PHI at the
// start and end of strings, bools, nulls, negatives, fractional numbers —
// redact each, and assert every output still parses as JSON and leaks no
// PHI. It is deterministically seeded so a failure is reproducible.
func TestWriterPropertyOverGeneratedLines(t *testing.T) {
	rng := rand.New(rand.NewSource(20260813))

	phiPool := []string{
		"9876543210",     // bare mobile
		"5876543210",     // bare mobile, alternate leading digit
		"123456789012",   // aadhaar
		"12345678901234", // abha
		"+919876543210",  // international mobile
	}
	// Deliberately excludes the documented hyphen/underscore-adjacent blind
	// spot (e.g. "ref_9876543210_x", "invoice-0000000001") — those are
	// covered by their own dedicated tests (TestRedactPatterns and
	// TestWriterSlowPathLeavesOrdinaryLongNumbersAlone) and would trip this
	// test's raw-digit-run backstop for a reason that is by design, not a
	// regression.
	safePool := []string{
		"11111111-1111-1111-1111-111111111111", // uuid, must survive
		"22222222-2222-2222-2222-222222222222", // uuid, must survive
		"",                                     // empty string
		"café",                                 // unicode
		"मरीज़ भर्ती",                          // unicode, non-Latin
	}

	randomLeaf := func() any {
		switch rng.Intn(9) {
		case 0:
			return phiPool[rng.Intn(len(phiPool))]
		case 1:
			return "call " + phiPool[rng.Intn(len(phiPool))] + " now" // PHI mid-sentence
		case 2:
			return phiPool[rng.Intn(len(phiPool))] + " end" // PHI at string start
		case 3:
			return "start " + phiPool[rng.Intn(len(phiPool))] // PHI at string end
		case 4:
			return safePool[rng.Intn(len(safePool))]
		case 5:
			return rng.Intn(1000) // safe small int
		case 6:
			return -9876543210 // negative PHI-shaped
		case 7:
			return 9876543210.75 // fractional PHI-shaped
		default:
			return rng.Intn(2) == 0 // bool
		}
	}

	buildValue := func(depth int) any {
		var build func(d int) any
		build = func(d int) any {
			if d <= 0 || rng.Intn(3) == 0 {
				return randomLeaf()
			}
			if rng.Intn(2) == 0 {
				n := 1 + rng.Intn(4)
				arr := make([]any, n)
				for i := range arr {
					arr[i] = build(d - 1)
				}
				return arr
			}
			n := 1 + rng.Intn(4)
			obj := make(map[string]any, n)
			for i := 0; i < n; i++ {
				key := fmt.Sprintf("k%d", i)
				if rng.Intn(4) == 0 {
					// PHI-shaped key, must also be screened.
					key = phiPool[rng.Intn(len(phiPool))]
				}
				obj[key] = build(d - 1)
			}
			return obj
		}
		return build(depth)
	}

	const iterations = 300
	for i := 0; i < iterations; i++ {
		doc := buildValue(3)
		line, err := json.Marshal(doc)
		require.NoError(t, err)

		out := through(t, string(line)+"\n")
		require.True(t, strings.HasSuffix(out, "\n"),
			"iteration %d: trailing newline must survive redaction: %q", i, out)
		requireParsesAsJSON(t, out)

		// The UUID pool members are themselves long digit-heavy strings (a
		// 12-digit contiguous run in their last group) that must legitimately
		// survive untouched, so they would trip the raw-digit-run backstop
		// below as a false positive. Strip verified-intact safe values before
		// scanning for leaked PHI.
		scanned := out
		for _, safe := range safePool {
			if safe == "" {
				continue
			}
			scanned = strings.ReplaceAll(scanned, safe, "")
		}
		requireNoRawDigitRun(t, scanned)

		for _, safe := range safePool {
			if safe == "" {
				continue
			}
			if strings.Contains(string(line), safe) {
				require.Contains(t, out, safe, "safe value must survive verbatim: iteration %d, input %s", i, line)
			}
		}
	}
}

// realisticCleanLogLine is what an actual slog record looks like in this
// codebase: it always carries a "time" field with an RFC3339 timestamp, and
// that timestamp always starts with a four-digit year. That four-digit year
// alone is enough to trip hasFourConsecutiveDigits, so — unlike the previous
// round's prefilter, which this exact line sailed past — this realistic line
// takes the slow (parse-and-re-encode) path despite carrying no PHI at all.
// This is named and benchmarked deliberately, not glossed over: it is the
// honest, representative cost of this writer on ordinary production traffic,
// and it is the number that belongs in any capacity-planning conversation.
const realisticCleanLogLine = `{"time":"2026-08-13T01:02:03Z","level":"INFO","msg":"appointment confirmed","tenant_id":"3fa85f64-5717-4562-b3fc-2c963f66afa6","ward":"ward-3","request_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"}` + "\n"

// syntheticNoDigitsLogLine has no run of four or more consecutive digits
// anywhere — not even in a timestamp — so it is the only shape of line that
// actually takes the true fast path (skip parsing entirely). No real slog
// record looks like this, since every one carries a four-digit year; this
// fixture exists purely to measure the floor cost of hasFourConsecutiveDigits
// itself, in contrast to realisticCleanLogLine above.
const syntheticNoDigitsLogLine = `{"time":"no-timestamp-field","level":"INFO","msg":"appointment confirmed","ward":"ward-a","note":"ok"}` + "\n"

// phiLogLine has PHI-shaped values, forcing the slow (parse) path.
const phiLogLine = `{"time":"2026-08-13T01:02:03Z","level":"INFO","msg":"patient call","phone":"9876543210","aadhaar":123456789012,"tenant_id":"3fa85f64-5717-4562-b3fc-2c963f66afa6"}` + "\n"

func benchmarkWrite(b *testing.B, line string) {
	b.Helper()
	var buf bytes.Buffer
	w := logging.NewRedactingWriter(&buf)
	p := []byte(line)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if _, err := w.Write(p); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRedactingWriterRealisticCleanLine measures the cost on an actual
// representative slog record with no PHI. Because every real record carries
// a four-digit year in its timestamp, this takes the slow (parse) path — see
// realisticCleanLogLine's comment.
func BenchmarkRedactingWriterRealisticCleanLine(b *testing.B) {
	benchmarkWrite(b, realisticCleanLogLine)
}

// BenchmarkRedactingWriterSyntheticNoDigitsLine measures the floor cost of
// the fast path in isolation: hasFourConsecutiveDigits rejecting a line and
// nothing else running. Not representative of real slog output — see
// syntheticNoDigitsLogLine's comment — but useful to see the prefilter's own
// cost separated from the parse-and-re-encode cost it usually gates.
func BenchmarkRedactingWriterSyntheticNoDigitsLine(b *testing.B) {
	benchmarkWrite(b, syntheticNoDigitsLogLine)
}

// BenchmarkRedactingWriterPHILine measures the slow (parse-and-re-encode)
// path on a line that does contain PHI and must be redacted.
func BenchmarkRedactingWriterPHILine(b *testing.B) {
	benchmarkWrite(b, phiLogLine)
}
