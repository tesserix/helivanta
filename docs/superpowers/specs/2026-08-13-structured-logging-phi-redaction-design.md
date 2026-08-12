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

One spec, four parts, sequenced so the guard lands first and survives an
interruption. Out of scope: log shipping and storage (infrastructure), metrics
and traces (#679).

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
`request_id`. After `authn` verifies the token, it enriches the request-scoped
logger with `tenant_id` and `subject`.

Both are safe to log and neither is patient data: `subject` is a GIP UID,
pseudonymous by construction, and `tenant_id` is a UUID. Together with
`request_id` they answer "which hospital, which user, which request" without
naming anyone.

## Part D — pattern redaction, as defence in depth

A `slog.Handler` wrapper that walks attributes recursively — including inside
groups and inside error values — masking anything matching:

- Aadhaar: 12 digits
- Indian mobile: `+91` forms and bare 10-digit numbers beginning 5-9
- ABHA: 14 digits, with or without hyphens

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

Redaction runs on every attribute of every emitted line. At `info` level and
current volumes that is not a concern; it is noted so that a future
high-volume path (#438's read-access audit is the likely first) evaluates it
rather than inheriting it silently.

---

## Error handling

Redaction fails safe: if the walker cannot process a value, it drops the field
rather than emitting it raw. An unrecognised `LOG_LEVEL` degrades to `info`
with a warning rather than refusing to boot — the opposite choice from the
`HMS_ENV` guards, because a log level cannot compromise tenant isolation and a
hospital API should not fail to start over a typo.

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
- `backend/pkg/logging/redact.go` — the redacting handler and counter
- `backend/pkg/logging/logging_test.go`, `redact_test.go`

Changed:

- `backend/pkg/tenantdb/db.go` — the comment
- `backend/internal/archtest/arch_test.go` — the `gorm.Open` allowlist test
- `backend/pkg/tenantdb/db_test.go` — the stderr property test
- `backend/cmd/api/main.go`, `backend/cmd/migrate/main.go` — `slog.SetDefault`
- `backend/pkg/authn/authn.go` — enrich the request logger after verification
- `backend/internal/config/config.go` — `LogLevel`

## Known limitations

- Pattern redaction cannot detect names, dates of birth, or addresses. Part A
  is what protects those.
- The redaction counter is in-process only until #679 provides a metrics sink.
- The arch test allowlists `gorm.Open` call sites; it does not verify the
  *arguments* at those sites. The stderr test covers the argument, which is why
  both exist.
- Redaction applies to what passes through slog. Anything a dependency writes
  directly to stdout bypasses it entirely — which is precisely why the GORM
  logger needed its own guard rather than relying on this.
