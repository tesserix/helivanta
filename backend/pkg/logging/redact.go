package logging

import (
	"bytes"
	"io"
	"regexp"
	"sync/atomic"
)

// bounded wraps a core pattern in explicit neighbour groups so a match only
// counts when it is a whole token.
//
// `\b` is not enough, and a UUID is why. Go's regexp is RE2 — no lookaround
// — so `\b\d{4}[-\s]?\d{4}[-\s]?\d{4}\b` happily matches the first thirteen
// characters of `11111111-1111-1111-1111-111111111111`, because a word
// boundary sits between `1` and the following `-`. Every tenant_id in every
// log line would come out as `[REDACTED:aadhaar]` and correlation — the
// entire point of Part C — would be destroyed by Part D. Requiring the
// neighbouring character to be outside [0-9A-Za-z_-] rejects it: the
// candidate is followed by a hyphen, so it is part of a longer token, so it
// is not an Aadhaar.
//
// What it costs, stated so a future widening is a deliberate decision:
// hyphen-adjacent PHI is a blind spot. `9876543210-9876543211` and
// `phone-9876543210` are both left in the clear. Do not widen the neighbour
// class without re-deriving the UUID case above.
//
// Group 1 is the preceding character (or start), group 2 the candidate,
// group 3 the following character (or end). 1 and 3 are preserved on
// replacement; only 2 is masked.
func bounded(core string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^0-9A-Za-z_-])(` + core + `)($|[^0-9A-Za-z_-])`)
}

// Order matters and is load-bearing in one direction: the +91 mobile form
// must be tried before Aadhaar, because `+919876543210` contains a run of
// exactly twelve digits and would otherwise be masked as an Aadhaar. The
// ABHA/Aadhaar ordering is belt-and-braces — bounded() already stops a
// 12-digit pattern from biting into a 14-digit run — but a longest-first
// list is the property worth stating.
var redactionPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// Indian mobile, international form: +91 then 10 digits beginning 5-9,
	// accepting the conventional 5-5 grouping. First, so it wins the
	// twelve-digit run it contains.
	{"mobile", bounded(`\+91[-\s]?[5-9]\d{4}[-\s]?\d{5}`)},
	// ABHA: 14 digits, optionally grouped 2-4-4-4 by hyphens or spaces.
	{"abha", bounded(`\d{2}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}`)},
	// Aadhaar: 12 digits, optionally grouped 4-4-4.
	{"aadhaar", bounded(`\d{4}[-\s]?\d{4}[-\s]?\d{4}`)},
	// Indian mobile, bare: 10 digits beginning 5-9, 5-5 grouping accepted.
	{"mobile", bounded(`[5-9]\d{4}[-\s]?\d{5}`)},
}

var redactions atomic.Uint64

// RedactionCount reports how many values this process has masked, across both
// the byte layer and the tag handler. It is an in-process counter with an
// accessor rather than a metric because no metrics system exists yet — #679 is
// unbuilt, and inventing a metrics dependency here would be worse than leaving
// an honest seam for it to wire up.
func RedactionCount() uint64 { return redactions.Load() }

// RedactString masks every known PHI pattern in s, returning the result and
// the number of masks applied.
func RedactString(s string) (string, int) {
	n := 0
	out := s
	for _, p := range redactionPatterns {
		marker := "[REDACTED:" + p.name + "]"
		re := p.re
		// Each pattern is applied to a fixpoint, not once. Because RE2 has no
		// lookbehind, a match consumes the character on either side to prove
		// it is a whole token — so in `9876543210 9876543211` the shared
		// space is eaten by the first match and the second is not seen on
		// that pass. The replacement puts the neighbour characters back, so
		// re-running over the output catches it. Two adjacent phone numbers
		// is an entirely ordinary thing to log; leaking the second one is
		// not an acceptable edge case.
		//
		// The loop terminates because every iteration that changes anything
		// strictly reduces the number of digit runs, and the marker it
		// substitutes contains no digits. The bound is belt-and-braces
		// against a future pattern that does not have that property.
		for i := 0; i < 100; i++ {
			before := n
			out = re.ReplaceAllStringFunc(out, func(m string) string {
				// The neighbour characters are part of the match so RE2 can
				// express "whole token" without lookaround; they are not part
				// of the secret, so put them back.
				groups := re.FindStringSubmatch(m)
				n++
				if len(groups) != 4 {
					// Cannot happen for a string the same regexp just
					// matched, but mask the lot rather than return it raw.
					return marker
				}
				return groups[1] + marker + groups[3]
			})
			if n == before {
				break
			}
		}
	}
	redactions.Add(uint64(n))
	return out, n
}

// NewRedactingWriter wraps w so that every line written through it is
// pattern-redacted.
//
// This sits at the writer rather than at the handler deliberately. A handler
// that inspects attribute values has to predict how slog will render each one
// — json.Marshaler vs encoding.TextMarshaler vs error vs fmt.Stringer, value
// receiver vs pointer receiver — and every version of that prediction written
// for this package leaked PHI in a different way (see the design spec's Part
// D). Here there is nothing to predict: these are the bytes.
//
// slog's JSON handler emits one Write per record under its own mutex, so this
// sees exactly one complete line at a time and needs no buffering or locking.
func NewRedactingWriter(w io.Writer) io.Writer { return &redactingWriter{inner: w} }

type redactingWriter struct{ inner io.Writer }

func (rw *redactingWriter) Write(p []byte) (int, error) {
	out := redactJSONLine(string(p))
	if _, err := rw.inner.Write([]byte(out)); err != nil {
		return 0, err
	}
	// io.Writer's contract is that a successful Write returns len(p). The
	// redacted line is a different length, and reporting that length would
	// read to any caller as a short write.
	return len(p), nil
}

// redactJSONLine masks PHI in one serialised log line, quoting the marker
// when the match sits outside a JSON string.
//
// The distinction matters because a bare marker substituted for a number
// token is not valid JSON: `{"aadhaar":123456789012}` must become
// `{"aadhaar":"[REDACTED:aadhaar]"}`, never `{"aadhaar":[REDACTED:aadhaar]}`.
// Emitting the latter would make slog's own encoder reject the record, and a
// redaction control that silently deletes log lines is its own incident.
func redactJSONLine(line string) string {
	var out bytes.Buffer
	out.Grow(len(line))

	inString := false
	escaped := false
	segStart := 0

	// flush redacts the segment [segStart,end) and appends it. Segments are
	// split at every string boundary so each one is wholly inside or wholly
	// outside a JSON string, which is what makes the quoting decision local.
	flush := func(end int, quoted bool) {
		if end <= segStart {
			return
		}
		seg := line[segStart:end]
		red, n := RedactString(seg)
		if n > 0 && !quoted {
			// A number token became a marker; it needs quotes to stay JSON.
			red = quoteBareMarkers(red)
		}
		out.WriteString(red)
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				// Close the string: flush its contents, then the quote.
				flush(i, true)
				out.WriteByte('"')
				segStart = i + 1
				inString = false
			}
			continue
		}
		if c == '"' {
			flush(i, false)
			out.WriteByte('"')
			segStart = i + 1
			inString = true
		}
	}
	flush(len(line), inString)
	return out.String()
}

// quoteBareMarkers wraps any redaction marker that is not already inside
// quotes, so a masked JSON number stays a valid JSON value.
func quoteBareMarkers(s string) string {
	return bareMarker.ReplaceAllString(s, `"$1"`)
}

var bareMarker = regexp.MustCompile(`(\[REDACTED:[a-z]+\])`)
