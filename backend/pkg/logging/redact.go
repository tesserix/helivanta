package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
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
// sees exactly one complete line at a time and needs no buffering or locking
// — that is the contract this writer relies on. It is only safe to wrap a
// writer that hands over one complete line per Write; a caller that splits a
// single log line across multiple Write calls can have PHI straddle the
// boundary and escape both halves, because each call is redacted
// independently. A malformed line is not rejected by anything downstream of
// this writer — it reaches stdout as-written and an ingest pipeline that
// expects one JSON object per line will simply drop it, silently, which is
// exactly the failure this package exists to avoid producing itself.
func NewRedactingWriter(w io.Writer) io.Writer { return &redactingWriter{inner: w} }

type redactingWriter struct{ inner io.Writer }

func (rw *redactingWriter) Write(p []byte) (int, error) {
	out := redactJSONLine(string(p))
	n, err := rw.inner.Write([]byte(out))
	if err != nil {
		return 0, err
	}
	if n < len(out) {
		// The inner writer accepted fewer bytes than we gave it but reported
		// no error. That is a short write by io.Writer's own contract, and
		// silently reporting success would hide a partially written — and
		// therefore possibly unredacted-looking or truncated — log line.
		return 0, io.ErrShortWrite
	}
	// io.Writer's contract is that a successful Write returns len(p). The
	// redacted line is a different length, and reporting that length would
	// read to any caller as a short write.
	return len(p), nil
}

// hasFourConsecutiveDigits reports whether line contains a run of at least
// four consecutive ASCII digit characters, evaluated directly on line's raw
// bytes exactly as they arrive from slog — before any JSON unescaping.
//
// It exists purely as a prefilter to skip parsing on lines that cannot
// possibly contain a match, and it must be a superset of "would RedactString
// change the DECODED text", checked against the ENCODED bytes — those are
// two different questions, and a prior version of this prefilter (an
// unanchored union of the core patterns, still evaluated on raw bytes) got
// this wrong in the same way a raw byte scan always will: every core pattern
// allows `[-\s]` as a separator between digit groups, `\s` matches a real
// tab/newline/CR, and those are exactly what JSON escapes as `\t`/`\n`/`\r`.
// A separator hiding behind an escape sequence — `{"e":"98765\n43210"}` —
// left the union-of-cores prefilter believing there was no possible match on
// the wire form, when the DECODED text (`98765` newline `43210`) is exactly
// the bare mobile pattern. Checking the prefilter and RedactString against
// the same representation (both encoded, or both decoded) cannot catch this:
// it takes comparing the prefilter on the wire bytes against RedactString on
// the decoded text to see the gap.
//
// A run of four consecutive digits sidesteps the whole separator question,
// because it does not look at separators at all — it only needs the fact
// that every core pattern requires an unbroken run of at least four digits
// SOMEWHERE in what it matches, regardless of what comes between groups:
//   - bare mobile is `[5-9]\d{4}` then `\d{5}` — the leading digit plus its
//     `\d{4}` group is already five contiguous digits, and the trailing
//     `\d{5}` is five more; either half alone clears the bar on its own.
//   - the `+91` mobile form is the same shape after the prefix.
//   - aadhaar is three `\d{4}` groups; each one alone clears the bar.
//   - abha's `\d{2}` head is short, but it is always followed by three
//     `\d{4}` groups, each of which clears the bar on its own.
//
// And digits are never escaped in valid JSON output: `encoding/json` (which
// is what slog's JSON handler uses) only ever escapes control characters,
// `"`, and `\` — never `0`-`9`. So whatever separator between two digit
// groups might be hiding behind an escape sequence, the digit groups
// themselves are always literal ASCII digit bytes in the wire form, with
// nothing in between them, which is exactly what this check looks for.
// Escaping the separator cannot make a real digit group invisible to a
// prefilter that never looked at separators to begin with.
//
// This is deliberately a wide superset — it will also return true for the
// stray four-digit run inside an ordinary UUID slice, or any other
// PHI-shaped-looking-but-safe digit run — and that is always fine: an
// unnecessary parse costs a few extra microseconds, while skipping a parse
// that should have happened skips redaction entirely.
//
// Accepted, not overlooked: an RFC3339 timestamp's four-digit year alone
// satisfies this gate, and every real record this codebase logs carries one
// in its "time" field. So in practice this gate trips on essentially every
// line, and the "fast path" it guards rarely fires on real traffic — most of
// the cost this package pays is the parse-and-re-encode path, not the gate
// itself. That trade was made deliberately: correctness on this package has
// already cost four review rounds, HMS is a hospital API rather than a
// high-volume firehose, and redaction cost on a genuinely high-volume path
// is exactly the kind of question #438's read-access audit exists to
// evaluate if one ever appears. Chasing microseconds here by narrowing this
// gate is not worth spending any of that correctness back.
func hasFourConsecutiveDigits(line string) bool {
	run := 0
	for i := 0; i < len(line); i++ {
		if line[i] >= '0' && line[i] <= '9' {
			run++
			if run >= 4 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

// redactJSONLine masks PHI in one serialised log line.
//
// It parses the line as JSON and re-encodes it, rather than scanning the raw
// bytes, because a scanner operates on two things it cannot get right at
// once: escaped bytes and partial number tokens.
//
//   - A newline inside a JSON string is the two raw bytes `\` `n`, and `n` is
//     a word character, so a byte scanner's boundary check reads `n` as part
//     of a longer token and refuses to match PHI immediately after it.
//     `errors.Join` — the standard library's own multi-error type — joins
//     with `\n`, so this was not a contrived input. Decoding first turns
//     `\n` into a real newline before the boundary check ever runs, which is
//     not a word character and so is a valid boundary.
//   - A regex match only ever covers a contiguous digit run, so a fractional
//     number like `9876543210.75` gets its integer part replaced but not its
//     decimal point, producing `"[REDACTED:mobile]".75` — invalid JSON.
//     Decoding hands the whole number literal to RedactString as one token,
//     and the result is re-encoded as a single JSON value, so there is no
//     partial substitution to corrupt.
//
// A line with no run of four or more consecutive digits anywhere (see
// hasFourConsecutiveDigits) cannot contain anything RedactString would
// match, so that case returns unchanged without parsing — this is the common
// case for most log lines and keeps the cost of this writer close to zero on
// them.
//
// If the line does not parse as a single JSON value, redaction falls back to
// scanning the whole line with RedactString. That fallback cannot corrupt
// JSON that was not valid JSON to begin with, but it also cannot promise the
// output is valid — which is the same guarantee (none) the input already
// had. What it must never do is invent syntax the input didn't have, so on
// that path the line's validity, or lack of it, passes through unchanged.
func redactJSONLine(line string) string {
	if !hasFourConsecutiveDigits(line) {
		return line
	}
	if out, ok := redactJSONByParsing(line); ok {
		return out
	}
	out, _ := RedactString(line)
	return out
}

// redactJSONByParsing decodes line as a single JSON value via encoding/json's
// token reader and re-encodes it with every string, key and number screened
// for PHI. Because the output is produced by the encoder rather than by
// patching the input bytes, it is valid JSON by construction whenever this
// function reports success.
//
// It reports false — meaning "did not produce output; caller must fall
// back" — whenever the input is not exactly one well-formed JSON value:
// decode errors, a truncated document, or trailing content after the first
// value all take this path rather than risk emitting something that looks
// plausible but isn't what was actually in the line.
//
// Whatever trails the JSON value in line — normally just the `\n` slog
// appends after every record — is captured at dec.InputOffset() the instant
// the top-level value finishes, and appended to the re-encoded output
// verbatim. This is not optional bookkeeping: an earlier version of this
// function only emitted the re-encoded value and silently dropped that
// trailer. slog writes one `Write` per record with no other framing, so the
// trailing newline is the only thing separating one record from the next in
// the output stream; dropping it splices every redacted record onto the
// front of whatever comes after it. In a newline-delimited log pipeline that
// does not corrupt one record, it corrupts two — the redacted one and its
// neighbour — which is the exact failure this whole function exists to
// prevent, now happening on every redacted line instead of a fraction of
// them. The trailer is copied as whatever bytes it actually was, not assumed
// to be exactly "\n", because nothing about this writer's contract promises
// that.
//
// When the parse succeeds but nothing actually matched a pattern — the
// common outcome, since hasFourConsecutiveDigits is a wide superset and
// gates in plenty of lines that turn out to be clean — this returns line
// itself, unmodified, rather than the re-encoded copy. This is a structural
// guarantee, not an incidental one: this package has produced four separate
// defects at exactly the seam of "re-encode and hope it matches the
// original" (dropped trailing newline, HTML-escaping, and two earlier
// rounds' worth of JSON-corruption bugs before that), and "we did not
// re-encode it" is a strictly stronger claim than "we re-encoded it and it
// happened to come out identical" — the latter has to keep holding across
// every future change to number formatting, escaping, key ordering, or
// whitespace, and the former cannot regress on any of those because there
// is no re-encoding to regress.
func redactJSONByParsing(line string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()

	var out bytes.Buffer
	out.Grow(len(line))

	// redactedCount totals every mask RedactString actually applied while
	// walking this document — across every string, key, and number token —
	// so the caller can tell "parsed, nothing to redact" apart from "parsed,
	// something changed" and return the untouched input in the former case.
	redactedCount := 0

	// strEnc is reused across every string/key token in this call. It is
	// configured with SetEscapeHTML(false) because json.Marshal (the
	// simpler alternative) always escapes `<`, `>` and `&` to `<` etc,
	// which slog's own encoder does not do. That divergence is semantically
	// harmless — both decode to the same string — but it means a redacted
	// line and a clean line render an identical value differently on the
	// wire, which breaks byte-level and grep-based log tooling that has no
	// reason to expect HTML-escaping from a structured logger.
	var strBuf bytes.Buffer
	strEnc := json.NewEncoder(&strBuf)
	strEnc.SetEscapeHTML(false)

	// frame tracks one open object or array so commas and, for objects,
	// key/value alternation land in the right places on re-encode.
	type frame struct {
		array     bool
		count     int  // values (object: key/value pairs) already emitted at this depth
		expectKey bool // object only: true when the next string token is a key
	}
	var stack []frame

	// beforeValue emits the comma that precedes every array element after
	// the first. It is a no-op for an object value, because that comma (if
	// any) was already emitted by beforeKey ahead of the key, and the colon
	// after the key is what separates key from value.
	beforeValue := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		if top.array && top.count > 0 {
			out.WriteByte(',')
		}
	}
	// afterValue records that a value was just emitted at the current depth
	// and, inside an object, flips back to expecting a key next.
	afterValue := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		top.count++
		if !top.array {
			top.expectKey = true
		}
	}
	beforeKey := func() {
		top := &stack[len(stack)-1]
		if top.count > 0 {
			out.WriteByte(',')
		}
	}
	writeJSONString := func(s string) bool {
		strBuf.Reset()
		if err := strEnc.Encode(s); err != nil {
			// A decoded Go string is always valid UTF-8 and always
			// encodes; this is unreachable in practice. Refuse rather than
			// guess if it ever isn't.
			return false
		}
		// Encode appends a trailing newline that has no place inside a
		// larger JSON document; trim it before splicing the value in.
		out.Write(bytes.TrimSuffix(strBuf.Bytes(), []byte("\n")))
		return true
	}
	// writeRedactedString redacts PHI patterns in the decoded (unescaped)
	// text of a string token — key or value — and re-encodes it so escaping
	// is regenerated correctly around whatever the redaction produced.
	writeRedactedString := func(s string) bool {
		red, n := RedactString(s)
		redactedCount += n
		return writeJSONString(red)
	}
	// writeRedactedNumber screens the whole number literal — sign, integer,
	// fraction and exponent together — as one token. The sign is split off
	// first: bounded()'s neighbour check requires the digit run to start at
	// the beginning of the text it is given, and a leading `-` sitting right
	// before the digits would otherwise read as "part of a longer token" and
	// block every negative PHI-shaped number from ever matching. If anything
	// matched, the whole thing — sign dropped, since a masked value has
	// nothing left worth signing — is re-emitted as a JSON string, because a
	// substituted marker is text, not a number. Otherwise the literal is
	// re-emitted unchanged and unquoted, so `42` stays `42`.
	writeRedactedNumber := func(numStr string) bool {
		sign := ""
		magnitude := numStr
		if strings.HasPrefix(magnitude, "-") {
			sign, magnitude = "-", magnitude[1:]
		}
		red, n := RedactString(magnitude)
		redactedCount += n
		if n > 0 {
			return writeJSONString(red)
		}
		out.WriteString(sign)
		out.WriteString(magnitude)
		return true
	}

	done := false
	var valueEnd int64
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}
		if done {
			// A second top-level token after the first value already
			// closed: more than one JSON value on this line. Not the
			// single-record-per-Write shape this function promises to
			// reproduce faithfully — refuse rather than concatenate.
			return "", false
		}

		switch t := tok.(type) {
		case json.Delim:
			switch rune(t) {
			case '{':
				beforeValue()
				out.WriteByte('{')
				stack = append(stack, frame{expectKey: true})
			case '[':
				beforeValue()
				out.WriteByte('[')
				stack = append(stack, frame{array: true})
			case '}':
				out.WriteByte('}')
				stack = stack[:len(stack)-1]
				afterValue()
			case ']':
				out.WriteByte(']')
				stack = stack[:len(stack)-1]
				afterValue()
			}
		case string:
			if len(stack) > 0 && !stack[len(stack)-1].array && stack[len(stack)-1].expectKey {
				beforeKey()
				if !writeRedactedString(t) {
					return "", false
				}
				out.WriteByte(':')
				stack[len(stack)-1].expectKey = false
			} else {
				beforeValue()
				if !writeRedactedString(t) {
					return "", false
				}
				afterValue()
			}
		case json.Number:
			beforeValue()
			if !writeRedactedNumber(t.String()) {
				return "", false
			}
			afterValue()
		case bool:
			beforeValue()
			if t {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
			afterValue()
		case nil:
			beforeValue()
			out.WriteString("null")
			afterValue()
		default:
			// Token() only ever returns the types handled above for the
			// standard decoder; anything else is not a document we can
			// safely reproduce.
			return "", false
		}

		if len(stack) == 0 {
			// The top-level value just finished. Record exactly how many
			// bytes of line it consumed, before the next Token() call
			// advances past any trailing whitespace looking for more.
			done = true
			valueEnd = dec.InputOffset()
		}
	}

	if len(stack) != 0 {
		// The document never closed — a truncated line. Do not report
		// success on an incomplete parse.
		return "", false
	}
	if !done {
		// No top-level value was ever seen at all (e.g. an empty or
		// all-whitespace line). Nothing to reproduce.
		return "", false
	}

	if redactedCount == 0 {
		// Nothing matched anywhere in the document. Return the original
		// input, not the re-encoded copy — see the doc comment above for
		// why this is a structural guarantee rather than a hoped-for
		// property of the re-encoding.
		return line, true
	}

	// Whatever trailed the JSON value — the newline slog appends, or
	// nothing at all — is copied through untouched.
	out.WriteString(line[valueEnd:])

	return out.String(), true
}
