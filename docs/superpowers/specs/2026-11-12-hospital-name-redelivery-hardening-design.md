# Hospital name redelivery hardening — design

- **Issue:** #1660 (follow-up to #1653, PR #1658)
- **Date:** 2026-11-12
- **Status:** proposed

## Problem

The #1653 reviews deferred nine minor findings. None changes delivered
behaviour, but each leaves a guarantee weaker than the tests or logs claim:
one test asserts off its own goroutine, one race test does not force its race,
the single-consumer guard is proven only through its helper and reads one of
two consumer lists, the migration's index and grant are untested, a test
helper relies on unstated ordering, the re-send warning can report a re-send
that rolled back, and announcer errors carry no step context.

## Goal

Every #1653 guarantee is proven by a test that fails when the guarantee is
broken, and the announcer's log and errors describe only what happened.

## Decisions

### D1. The re-send warning is logged after commit

`announceHospitalNames` records the attempt number inside the `WithTenant`
callback and logs `re-sent unconfirmed hospital name` only when `WithTenant`
returns nil. A pass whose transaction rolls back logs the existing
`workforce hospital name announcement failed; retrying later` warning and no
re-send. Fields are unchanged: `tenant_id`, `hospital_id`, `name_version`,
`attempt`.

### D2. Announcer errors name their step

Each error `announceHospitalNames` returns or logs is wrapped with `%w` and the
step that failed: `scan due hospital names`, `lock hospital`,
`claim hospital name version`, `re-check unconfirmed hospital name`,
`schedule hospital name re-send`, `encode hospital name`,
`enqueue hospital name`. `errors.Is(err, context.Canceled)` still holds
through the wrapping, so cancellation stays silent.

### D3. The `json.Marshal` branch stays

Marshalling the fixed `HospitalNamedData` struct cannot fail, but every
workforce event payload checks its `json.Marshal` error
(`assignment_scope.go`, `attendance_corrections.go`, …) and `errcheck` is
enabled, so the alternative is `data, _ :=` with a lint exception. D2 wraps
the error like the other steps. This closes the issue's item by
justification, with no code change beyond D2.

### D4. Two racing passes are forced to race

`TestResendRacingPassesSendOnce` holds the hospital row lock in a test
transaction, starts two passes, and waits until `pg_locks` shows two
ungranted locks, so both passes have scanned the marker as due. It then
releases the lock. Exactly one re-send results and `attempts == 1`. This
proves the locked re-check's "another replica re-sent already" branch, which
the current test only exercises by chance.

### D5. The mid-pass confirmation test stays on its goroutine

`TestConfirmationDuringAPassStopsItsResend` uses `require.EventuallyWithT`
with `assert.NoError(c, …)`, so the lock-wait poll reports failures through
the collector instead of calling `require` off the test goroutine. Both
background passes (here and in D4) use `t.Context()`, so they stop when the
test ends.

### D6. The single-consumer guard is a function that returns an error

`oneConfirmingConsumer(mods []platform.Module) error` returns nil when
`pharmacy-hospital-names` is the only consumer of `SubjectHospitalNamed`, and
an error naming every consumer plus the existing per-consumer-confirmation
message otherwise. `TestHospitalNamedHasOneConfirmingConsumer` requires no
error over `allModules()`. The proof test requires an error containing that
message over `allModules()` plus a stub second consumer. The guard's own
decision is then what fails, not a separate assertion on the helper's output.

### D7. The guard reads every consumer list a module can build

A module's `Consumers(deps)` can return a different list when
`deps.Workflows` is set (workforce does) or a feature flag is on (privacy
behind `PrivacyDeadlinesEnabled`, emergency behind
`EmergencyWorkflowsEnabled`/`EmergencyDispatchEnabled`). The guard collects
consumers from `platform.Deps{}`, from `platform.Deps{Workflows: <stub>}`, and
from the same with every boolean field of `platform.Deps` set by reflection,
so a flag added later is covered without editing the guard. The stub embeds
`client.Client` and is never called; constructors only capture it. Names are
deduplicated. Proof tests add a stub module whose second consumer appears only
when `Workflows` is set, and one whose consumer appears only behind a flag; the
guard must reject both.

### D8. The migration's index and grant are asserted

The workforce migration test gains assertions, after the 0101 migration
runs, that:

- `pg_indexes` has `workforce_hospital_name_announcements_due`, with a
  definition containing `WHERE (next_attempt_at IS NOT NULL)`;
- `has_column_privilege('helivanta_system',
'workforce_hospital_name_announcements', 'next_attempt_at', 'SELECT')` is
  true.

### D9. The announced-events helper orders explicitly

`announced` in `hospital_names_test.go` orders outbox rows by
`created_at, id`, so two rows with the same timestamp still come back in a
stable order. The helper's comment states that order.

## Out of scope

- Any change to the redelivery behaviour, backoff or contracts from #1653.
- Generic dead-letter replay (#600).

## Tests

Every changed assertion is shown to fail against a mutation:

- D1: a test-only `DEFERRABLE INITIALLY DEFERRED` constraint trigger on
  `workforce_hospital_name_announcements` raises at commit, so a due re-send
  rolls back after its enqueue. With slog captured, the pass logs the
  retrying-later warning and no `re-sent unconfirmed hospital name`, and the
  marker's `attempts` is unchanged. Mutation: log inside the callback.
- D2: `AnnounceHospitalNamesOnce` with an already-cancelled context returns an
  error that contains `scan due hospital names` and satisfies
  `errors.Is(err, context.Canceled)`. Mutation: return the raw error.
- D4: removing the locked re-check's `next_attempt_at<=now()` makes the test
  fail deterministically.
- D6/D7: the proof tests fail if the guard ignores a second consumer, in
  either consumer list.
- D8: dropping the index or the grant from the migration fails the test.

Gates: `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`,
`go test -race` on workforce and archtest.
