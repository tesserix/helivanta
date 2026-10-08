# The log redactor masks Aadhaar numbers, not every 12-digit number

**Issue:** [#840](https://github.com/tesserix/helivanta/issues/840)
**Amends:** `pkg/logging`'s byte-layer redactor (`redact.go`).

## The problem, stated precisely

`pkg/logging`'s byte layer masks any whole-token run of 12 digits as
`[REDACTED:aadhaar]`, 14 digits as ABHA, and 10 digits starting 5–9 as a
mobile. `slog` renders a `time.Duration` as its nanosecond `int64`, so:

| Logged | Rendered | Emitted |
|---|---|---|
| `15 * time.Minute` | `900000000000` | `[REDACTED:aadhaar]` |
| `24 * time.Hour` | `86400000000000` | `[REDACTED:abha]` |
| `5 * time.Second` | `5000000000` | `[REDACTED:mobile]` |

Every byte count, row count or epoch value of those lengths goes the same
way. Twice, a log line whose whole purpose was to show an operator a
mistyped value showed a mask instead. Separately, `cmd/api/main.go` says the
package's thesis is "everything the process emits is screened". It screens
PHI shapes, and real key material passes through untouched.

## Decisions

### D1: Durations render as text, at the one handler

`NewWithWriter` is the only place a `*slog.Logger` is built. Its JSON
handler now has a `ReplaceAttr` that renders every `slog.KindDuration` value
with `Duration.String()`: `15m0s`, `24h0m0s`.

This is the structural form of the convention that #838 got wrong twice. No
call site can forget it, because no call site is involved. `ReplaceAttr`
sees values after `LogValuer` resolution and inside groups, which covers
every duration passed as an attribute.

A `time.Duration` field inside a struct that slog marshals through
`encoding/json` still renders as an integer, because `ReplaceAttr` does not
see inside a JSON-marshalled value. D2 bounds what happens to that integer.

### D2: A 12-digit run is masked as Aadhaar only if it could be one

UIDAI issues Aadhaar numbers whose first digit is 2–9 and whose twelfth
digit is a Verhoeff check digit over the first eleven. A 12-digit run that
breaks either rule cannot be an Aadhaar, so leaving it unmasked lets through
no PHI the old rule would have caught.

The Aadhaar pattern's core becomes `[2-9]\d{3}[-\s]?\d{4}[-\s]?\d{4}`, and a
candidate is masked only if its digits pass Verhoeff. Of uniformly random
12-digit numbers, 8 in 10 start 2–9 and 1 in 10 of those pass the check, so
about 8% are still masked. That is the stated residual false positive, down
from 100%. It errs toward masking: a number that happens to be
Aadhaar-valid is treated as one.

**Why not only the delimiter, or only key-based exclusion** (the issue's
other options):
- **Delimiters:** `bounded()` already requires whole tokens. `900000000000`
  is a whole token, so tightening the delimiter alone fixes nothing.
- **Key-based exclusion:** an allowlist of "numeric" keys is a convention
  someone has to maintain, and an Aadhaar logged under one of those keys
  would leak.

### D3: Skipping a candidate must never hide its neighbour

The fixpoint loop relied on every match being replaced. With RE2 and no
lookbehind, a match consumes the separator on each side to prove it is a
whole token, and the loop re-ran until no match remained. A candidate that
D2 declines would match again on every pass, and it would keep consuming
the separator in front of a valid Aadhaar right after it. That Aadhaar
would never be matched, which is a leak.

So each pattern is applied by an explicit left-to-right scanner. After a
candidate, masked or skipped, the next search starts one character before
the candidate's end. It uses a variant of the pattern without the `^`
alternative, so the shared separator is available as the next candidate's
left neighbour, exactly as lookbehind would allow. The scanner replaces the
fixpoint loop for every pattern, so there is one mechanism, and it has no
iteration bound to reason about.

### D4: ABHA and mobile are unchanged, and the boundary is stated

ABHA (14 digits) and bare mobile (10 digits starting 5–9) keep their
shape-only rules. There is no published check digit for them that this
change can rely on and verify.

Their worst false positives are durations (`24h`, `5s`), which D1 removes.
Other 14-digit or 10-digit numbers (a 5–9 GB byte count, for instance) are
still masked. That is the stated residual, and it errs toward masking.

**The boundary.** `pkg/logging`'s package doc and `cmd/api/main.go` now say
it plainly: the redactor screens **PHI shapes and tagged fields, not
secrets**. Key material, tokens and passwords are not detected, and keeping
them out of logs is a separate control and a separate decision.

## Not covered

- Secret screening (D4).
- ABHA and mobile checksums (D4).
- Durations nested in JSON-marshalled structs. They render as integers and
  are bounded only by D2 and D4.

## Tests

- **D1.** Durations of 15m, 24h and 5s logged through `NewWithWriter` come
  out as `"15m0s"`, `"24h0m0s"` and `"5s"`, with no `REDACTED`. This fails
  with the `ReplaceAttr` removed.
- **D2, masked:** Verhoeff-valid, 2–9-leading Aadhaar-shaped numbers in
  ungrouped, space-grouped and hyphen-grouped forms, as a string and as a
  JSON number.
- **D2, not masked:**
  - the same numbers with the check digit changed;
  - `900000000000`;
  - a number starting with 0 or 1.
- **Can the loosening go too far?** A test asserts specific valid numbers
  stay masked. Dropping the Verhoeff call, or making it accept everything,
  fails the "not masked" cases. Making it reject everything fails the
  "masked" cases.
- **D3.** An invalid 12-digit run immediately followed by a valid Aadhaar,
  sharing one space, masks the second. This fails against the replaced
  fixpoint loop with skip semantics.
- **Verhoeff** against published vectors (`2363`, `123451`).
- **Existing fixtures.** These used `123456789012`, which can never be an
  Aadhaar. They now use a synthetic Verhoeff-valid number, so every existing
  assertion keeps its meaning.
