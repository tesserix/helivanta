# Structured logging with PHI redaction — design

Resolves: [#678](https://github.com/tesserix/hms/issues/678) — [Go SDK]
Structured logging with PHI redaction

Date: 2026-08-13

## Problem

Three gaps, discovered while investigating a fourth that turned out not to exist.

**The standard and the code disagree.** `docs/standards/backend.md` mandates
slog with JSON output. No handler is configured anywhere — no `slog.New`, no
`slog.SetDefault`, no JSON handler — so every log line goes to stderr as plain
text from `slog.Default()`.

**Correlation is incomplete.** `requestid.Middleware` attaches `request_id` and
nothing else. A log line cannot answer which hospital or which user it belongs
to, which is the first question during an incident in a multi-tenant clinical
system.

**One unremarked line prevents a full PHI dump.** GORM's logger echoes the
executed SQL with inlined parameter values on error and slow-query paths.
Verified against the dev database:

```
[0.767ms] [rows:0] INSERT INTO phi_probe VALUES (2,'HQ-OPD-0001427','Suresh Kumar')
```

That is every column of every failing write, patient names included. The only
thing preventing it is one clause in `backend/pkg/tenantdb/db.go`:

```go
&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
```

No test asserts it, no arch rule protects it, and no comment explains it. A
developer debugging a slow query switches it to `logger.Info`, and the diff
looks entirely reasonable in review.

### A correction this design rests on

A previous claim — recorded on #70, #774 and #775 — held that
`respond.InternalErr` leaked patient values because Postgres puts them in an
error's `DETAIL` line. **That is false and has been retracted.** Postgres does
emit the value, but `pgx`'s `PgError.Error()` returns only the message and
SQLSTATE:

```
err.Error():  ERROR: duplicate key value violates unique constraint "..." (SQLSTATE 23505)
%v, %+v:      identical
```

Logging an error cannot leak the value through that path. The claim was
recorded without being tested; the test that disproved it is what found the
GORM exposure above. This matters for scope: the urgent work is the GORM guard,
not error-string redaction.

## Scope

One spec, five parts, sequenced so the guard lands first and survives an
interruption. Out of scope: log shipping and storage (infrastructure), metrics
and traces (#679).

**Shipped: Parts A–E.** Part A merged separately as the urgent guard; B, C and D
followed. **Part E (tag redaction) shipped separately as #778**, after its
original reflection-based design was implemented, reviewed twice and withdrawn
over five Critical defects. What ships is the marshal-then-mask design described
at the end of Part E: `encoding/json` renders the value, and the rendered bytes
are masked at the JSON paths the tags identify. Issue #678's tagged-field
acceptance criterion is met by `backend/pkg/logging/phitag.go`.

---

## Part A — the guard

Three mechanisms, because each catches a failure the others cannot.

**1. A comment** in `tenantdb.Open` stating why the logger is silent. Today it
reads as a noise preference. It is a PHI control.

**2. An arch test** restricting `gorm.Open` to an allowlist —
`pkg/tenantdb/db.go` and `internal/testinfra/containers_test.go` — using the
same AST walk as the existing `TestWithAdminIsOnlyCalledFromTheAllowlist`. This
is the only mechanism that catches a *new* `gorm.Open` added elsewhere, which
no amount of care inside `tenantdb.Open` would.

**3. An stderr test** that opens a pool through `tenantdb.Open`, triggers a
unique violation carrying a distinctive value, captures stdout and stderr, and
asserts the value never appears.

The third tests the property; the second tests the mechanism. Keeping both is
deliberate: the property test still holds if the logging library changes, and
the mechanism test catches the reasonable-looking diff before it merges.

## Part B — JSON output and level control

A `pkg/logging` package exposing `New(level string) *slog.Logger`, returning a
JSON handler. Level comes from `LOG_LEVEL`, defaulting to `info`; an
unrecognised value falls back to `info` rather than failing, since a
mistyped log level should not stop a hospital's API from starting.

`cmd/api` and `cmd/migrate` call `slog.SetDefault` with it during startup,
before anything else logs.

## Part C — correlation fields

`requestid.Middleware` runs before authentication, so it can only know
`request_id`. Once the principal is known, the request-scoped logger gains
`tenant_id` and `subject`.

**The enrichment lives in the platform layer, not in `pkg/authn`.** `pkg/authz`
already imports `internal/platform/respond`, which the foundation audit flagged
as a dependency pointing the wrong way — `pkg/` is meant to be the layer
`internal/` builds on, not the reverse. Having `pkg/authn` reach into
`internal/platform/requestid` would deepen that inversion for no benefit. The
platform layer already wires the middleware chain and can enrich the logger
after `authn` has populated the context, keeping the arrow pointing one way.

Both are safe to log and neither is patient data: `subject` is a GIP UID,
pseudonymous by construction, and `tenant_id` is a UUID. Together with
`request_id` they answer "which hospital, which user, which request" without
naming anyone.

## Part D — pattern redaction, as defence in depth

Two layers, split by what each can actually see:

**Pattern redaction wraps the writer, not the handler.** The JSON handler is
constructed over a redacting `io.Writer`, so what gets screened is the
serialised line slog actually emits. Masked:

- Aadhaar: 12 digits
- Indian mobile: `+91` forms and bare 10-digit numbers beginning 5-9
- ABHA: 14 digits, with or without hyphens

**Tag redaction stays at the value level**, because a `hmslog:"phi"` tag is
visible on the Go value and gone by the time it is bytes. See Part E.

### Why the writer and not the handler — this was learned, not designed

The original design here was a `slog.Handler` wrapper walking attributes
recursively. It was implemented, reviewed adversarially, and taken through two
fix rounds. Both rounds closed every finding they were given, and both
introduced new leaks **of the same class**. The class:

| Screened | Emitted | Result |
|---|---|---|
| `fmt.Sprint(v)` | `encoding/json` | `[]*Patient` leaked — pointers print as addresses to `fmt`, are followed by `json` |
| `%g` | decimal | `float64(123456789012)` leaked |
| reflection-rebuilt struct | `MarshalJSON` | a patient **name** the marshaller withheld was published *by the redactor* |
| `MarshalText()` | `Error()` | error text leaked — slog skips `Error()` for `json.Marshaler` but not for `TextMarshaler` |

A handler-level redactor has to *predict* how slog will render each value —
`json.Marshaler` vs `encoding.TextMarshaler` vs `error` vs `fmt.Stringer`,
value receiver vs pointer receiver — which means duplicating slog's dispatch
and re-duplicating it whenever Go changes. Each round did that more accurately
and was wrong differently. The reflection needed to support it (cycle sets,
depth caps, a visit budget) became its own defect source: a `big.Int`
attribute was silently replaced with `!ERROR`, deleting a log record.

Screening the emitted bytes removes the guess entirely. There is no proxy: the
bytes are the output. It also deletes the reflection walker, the marshaller
ordering, the visit budget and the cycle handling — roughly 500 lines and every
finding above — in exchange for one `io.Writer`.

**The byte layer parses the line rather than scanning it.** A first attempt
hand-rolled a scanner that tracked string boundaries and quoted the marker when
a match fell outside one. It was implemented to spec and failed two ways that
a scanner cannot avoid, because it operated on *escaped bytes* and on *partial
number tokens*:

- **PHI after a JSON escape leaked.** A newline inside a string is the two
  bytes `\` `n`, and `n` is a word character, so the boundary rule read
  `n9876543210` as one long token and declined to match.
  `errors.Join` — the standard library's own multi-error type — joins with
  `\n`, so `slog.Error("validation", "err", joined)` emitted the number in the
  clear. Tabs and `\uXXXX` failed identically.
- **Fractional numbers were corrupted into invalid JSON.** A match covers only
  the integer or the fractional run, so `{"amount":9876543210.75}` became
  `{"amount":"[REDACTED:mobile]".75}`. A property test over 4000 generated
  lines corrupted 813 of them. Invalid output is not cosmetic: the line reaches
  stdout malformed and the ingest pipeline drops it, so the redaction control
  silently deletes log records.

Both disappear when the line is decoded instead of scanned. The writer streams
the line through `encoding/json`'s token reader and re-encodes it:

- a **string** token arrives already unescaped, so `\n` is a real newline and
  the boundary rule works on the text a human would see; re-encoding escapes it
  again correctly;
- a **number** arrives as one whole token including sign, fraction and
  exponent, so the redaction decision is made on the entire value;
- **keys** are strings and get the same treatment;
- everything is re-emitted through the encoder, so the output is valid JSON by
  construction rather than by careful assertion.

A digit pre-filter short-circuits any line with no run of ten or more digits,
which is most of them, so the common path does not pay for the parse.

The property to test remains the same and is now structural: every redacted
line still parses, and no PHI survives in any position — inside escapes,
inside numbers, inside keys.

`slog`'s JSON handler emits one `Write` per record under its own mutex, so the
writer sees exactly one complete line at a time and needs no buffering or
locking of its own.

Two limits, stated here rather than discovered later:

- **Patterns cannot catch names or dates of birth.** Part A does the real work;
  this is a second line, not the defence.
- **False positives are expected.** A legitimate 12-digit identifier will be
  masked. That is the correct direction to fail, but someone will eventually be
  confused by a redacted value that was not PHI, and the redaction marker
  should make it obvious what happened.

The issue asks for a redaction counter metric. No metrics system exists — #679
is unbuilt — so this exposes an atomic counter with an accessor, documented as
the seam #679 will wire up. Inventing a metrics dependency here would be worse
than leaving an honest hook.

Redaction runs on every emitted line. At `info` level and current volumes that
is not a concern; it is noted so that a future high-volume path (#438's
read-access audit is the likely first) evaluates it rather than inheriting it
silently. The byte layer is a fixed number of regex passes over one line, with
no reflection and no allocation proportional to structure depth, so it is
cheaper than the walker it replaced.

## Part E — tag redaction — SHIPPED as #778

**Status: shipped, but not as the design first written here.** The original
reflection-based implementation was written, reviewed twice and withdrawn over
five Critical defects; the withdrawn implementation is preserved on
`backup/678-phitag-reflection`. What shipped under
[#778](https://github.com/tesserix/hms/issues/778) is the *marshal-then-mask*
replacement described at the end of this section, in
`backend/pkg/logging/phitag.go`. What follows describes the intent, then why the
reflection approach was abandoned and what replaced it.



A name, a date of birth and an address have no shape to match on. The only way
to know they are PHI is for the type to say so:

```go
type Patient struct {
    ID   string
    Name string `hmslog:"phi"`
}
```

This is the half the byte layer cannot do — the tag is a property of the Go
value and is gone by the time the record is serialised — so it stays a
`slog.Handler` wrapper. Its surface is far smaller than the walker Part D
removed: it reflects only over values whose type carries a `phi` tag somewhere
(cached per `reflect.Type`), replaces those fields with `[REDACTED:phi]`, and
returns `ok=false` for everything else so the value keeps its ordinary
rendering.

It inherits one lesson from Part D's history: a type that defines its own
`MarshalJSON` is rendered by that method, so reconstructing it from exported
fields can publish what its author deliberately withheld. The tag walker
therefore declines to rewrite any value implementing `json.Marshaler` or
`encoding.TextMarshaler`, and the limitation is recorded rather than papered
over — such a type must not carry PHI in its marshalled output, and the byte
layer is what covers it if it does.

Because it runs before serialisation and the byte layer runs after, the two
compose: a tagged field is masked by name, and anything the tag missed is
still pattern-screened on the way out.

### Why the reflection implementation was withdrawn

Two rounds produced five confirmed Critical defects, all from one root cause:
**masking a field by reconstructing the value from reflection means
reimplementing `encoding/json`.** Every rule the reconstruction did not
reproduce became a defect.

| Defect | Cause |
|---|---|
| An untagged wrapper holding `[]patient` leaked every name — `{"Rows":[{"Name":"SECRET-NAME"}]}` | tag detection never looked through a collection element type. This is the ordinary list-response DTO, the most likely shape in the system. |
| `json:"-"` fields were published — `{"Secret":"HIDDEN"}` | the reconstruction keyed on Go field names and ignored json tags, `omitempty` and `-`. The tag layer published what the type withheld, which is the exact constraint the marshaller decline exists to enforce. |
| The depth cap failed **open** — 8 plaintext names past the limit | past `maxPHIDepth` the remaining subtree was handed to the encoder raw. A true cycle was caught only because `encoding/json` bails on cycles; a finite deep chain had no such backstop. |
| Type-cache poisoning across mutually recursive types | `hasPHITag` seeded `false` before recursing, so an outer type could observe the seed and cache `false` permanently. Order-dependent and racy. |
| A 20-node shared-reference DAG hung the logger past 25s | the depth cap was per-branch, not a global node budget, so a DAG re-expanded into a tree. One log line could wedge a request goroutine. |

This is the same trap Part D fell into from the other direction: Part D tried to
predict how `encoding/json` would *render* a value; Part E tried to *reproduce*
that rendering. Both mean duplicating a library's behaviour and being wrong in a
new way each round.

**The design that should replace it — marshal first, then mask.** Do not
reconstruct the value at all:

1. `json.Marshal(v)` — the type's true rendering, honouring json tags,
   `omitempty`, `-`, embedding, custom marshallers, cycles and DAGs, because
   `encoding/json` does all of it.
2. Per type, cached, compute the set of **JSON paths** that carry
   `hmslog:"phi"`, applying `encoding/json`'s own field-naming rules.
3. Walk the marshalled JSON and replace the values at those paths.

Only step 2 reimplements anything, and it reimplements *naming* rather than
*rendering* — a far smaller and fully testable surface. `json:"-"` fields never
appear in the output, so they cannot be published; cycles and deep or wide
graphs are `encoding/json`'s problem, and it already solves them.

This is what shipped. Beyond the three steps above, two things the withdrawn
design guessed at are not guessed at here: which json tag names `encoding/json`
honours is answered by *asking it* (marshal a synthetic one-field struct and
read the emitted key), and its depth/tag conflict rule is mirrored explicitly
with a fail-closed branch. Both were the source of a name-resolution leak
during #778's own review, so neither is left to inference.

Parts A–D shipped before it, during which **names, dates of birth and addresses
were protected by Part A only** — the GORM guard that keeps bulk patient data
out of the logs entirely. Issue #678's tagged-field acceptance criterion is met
by this part.

---

## Error handling

There is no reflection walker — that was the withdrawn Part E design (see above
and #778). Part E as shipped fails closed: a value whose type carries a phi tag
and which cannot be marshalled, or whose rendering exceeds the size guard or
cannot be walked, is replaced *whole* by the marker rather than emitted
partially masked. A value whose type carries no tag is never touched.

The pattern mechanism is `NewRedactingWriter`: it parses each emitted line as
JSON and re-encodes it with every string, key and number screened for PHI. If
the line does not parse as a single well-formed JSON value, it does **not**
drop the field — it falls back to whole-line `RedactString`, screening the raw
bytes as text and passing the result through exactly as valid (or invalid) as
the input already was. Nothing is ever dropped; the fallback only changes how
the line is scanned, not whether it is emitted. An unrecognised `LOG_LEVEL`
degrades to `info` with a warning rather than refusing to boot — the opposite
choice from the `HELIVANTA_ENV` guards, because a log level cannot compromise tenant
isolation and a hospital API should not fail to start over a typo.

## Testing

- **Guard:** the arch test and the stderr property test described in Part A.
- **JSON:** capture a line, parse it as JSON, assert `level`, `msg` and
  timestamp are present.
- **Correlation:** assert a request-scoped logger carries `request_id`,
  `tenant_id` and `subject` after authentication.
- **Redaction:** a table test covering each pattern; a value nested in a group;
  a value inside a wrapped error; the counter incrementing; and non-matching
  values passing through untouched.

Every redaction test is verified non-vacuous the way this workstream has
learned to — break the check, confirm the test fails, restore it. Four inert
assertions have been found on this codebase already; assertions that match on
a bare name against entries carrying a suffix are the specific shape to avoid.

## Files

New:

- `backend/pkg/logging/logging.go` — `New(level string) *slog.Logger`
- `backend/pkg/logging/redact.go` — the pattern set, the byte-level redacting
  writer, and the counter
- `backend/pkg/logging/logging_test.go`, `redact_test.go`

Changed:

- `backend/pkg/tenantdb/db.go` — the comment
- `backend/internal/archtest/arch_test.go` — the `gorm.Open` allowlist test
- `backend/pkg/tenantdb/db_test.go` — the stderr property test
- `backend/cmd/api/main.go`, `backend/cmd/migrate/main.go` — `slog.SetDefault`
- the platform middleware chain — enrich the request logger with `tenant_id`
  and `subject` once `authn` has populated the context (not `pkg/authn`, to avoid
  deepening the `pkg/` -> `internal/` inversion)
- `backend/internal/config/config.go` — `LogLevel`

## Known limitations

- Pattern redaction cannot detect names, dates of birth, or addresses. Part E
  masks them **only where a field is tagged** `hmslog:"phi"` — an untagged field
  is not PHI as far as the logger is concerned. Part A — keeping GORM's logger
  silent — remains what protects bulk patient rows, by preventing them from
  reaching the log stream at all.
- Part E sees only static types. PHI logged through `[]any` or
  `map[string]any` carries no tag on the element type and is not masked, and a
  type with its own `MarshalJSON` renders however it likes beneath the tag.
- The redaction counter is in-process only until #679 provides a metrics sink.
  With redaction split across two layers it counts both.
- The arch test allowlists `gorm.Open` call sites; it does not verify the
  *arguments* at those sites. The stderr test covers the argument, which is why
  both exist.
- Redaction applies to what passes through slog. Anything a dependency writes
  directly to stdout bypasses it entirely — which is precisely why the GORM
  logger needed its own guard rather than relying on this.
