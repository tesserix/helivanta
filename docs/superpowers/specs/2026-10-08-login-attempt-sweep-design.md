# Expired login attempts are swept, not left holding live session tokens

**Issue:** [#869](https://github.com/tesserix/helivanta/issues/869)

## The problem, stated precisely

`login_attempt` (`0004_iam`) holds a live Zitadel session id and token between
the password step and the step after it: a TOTP code (#867), a native
enrolment (#948), or a password change (#856). `loginAttemptStore.Get` deletes
an expired row only when that same `auth_request_id` is read again.

A browser that abandons the next step never reads its row again. Examples are
a closed tab, a clinician called away mid-shift, or a tab left open past
`expires_at`. The row, and the session token in it, then stays in Postgres
indefinitely. That table carries no forced RLS, by design, because login
precedes tenant selection.

It is not a login bypass, because `Get` treats an expired row as not found. It
is a stale credential at rest with no upper bound on its lifetime. The
`login_attempt_expires_at_idx` index exists for a sweep that was never written.

## Decisions

### D1: One statement deletes every expired row

`loginAttemptStore.SweepExpired` runs
`DELETE FROM login_attempt WHERE expires_at < now()` through the system pool,
as every other `login_attempt` query does, and returns the number of rows
deleted. It ignores `stage` and `enrolling`: an expired row is unusable at any
stage (`Get` refuses it), so all of them go. The comparison runs on
Postgres's clock, the same clock `BumpAndGet` already uses
(`expires_at > now()`), so the sweep and the readers agree on what "expired"
means.

It deletes only rows already past `expires_at`. A live attempt that a user is
completing right now is never touched. The race with a concurrent `Get` is
benign: either the sweep deletes the row first and `Get` answers "not found",
or `Get` deletes it first and the sweep finds nothing.

### D2: The sweep runs at boot and then every `loginAttemptTTL`

`(*LoginUIHandlers).RunAttemptSweeper(ctx)` is started from `cmd/api/main.go`
next to `bus.RunPruner`.

- **At boot.** It sweeps once immediately. Rows left by a previous process,
  such as a crashed pod or a long gap between deploys, are not left waiting a
  full interval.
- **Every `loginAttemptTTL` (5 minutes) after that.** An abandoned token then
  survives at most about two TTLs (`expires_at` plus one interval), instead of
  indefinitely.

The interval follows the TTL rather than the outbox pruner's hour. The
property being bounded is how long a credential lingers at rest, and that
bound should scale with how long the row was meant to live. Each pass is one
indexed `DELETE` against a table that holds at most a few rows per in-flight
sign-in, so this cadence costs nothing measurable.

Every replica runs the sweeper. The `DELETE` is idempotent, so concurrent
passes simply race to delete the same rows; no leader election is needed.

### D3: A failed or panicking pass is logged and the next one runs

`RunPruner`'s shape is reused:

- The loop stops when `ctx` ends.
- A panic is contained to the pass and logged with its stack. The loop is
  started with a bare `go`, so an escaped panic would end the process.
- An error is logged at ERROR and the loop continues.

The direction is deliberate. A sweep that cannot run leaves expired rows that
`Get` already refuses, which is today's state and not a bypass. Taking the API
down over it would cost sign-in for a hospital to avoid a delay in deleting
unusable rows.

Every pass, including one that deletes nothing, logs
`login attempt sweep deleted=N`. A sweep nobody can observe would be the same
blind spot the issue describes.

## Not covered

- **Revoking the Zitadel session itself.** Deleting the row makes the session
  unreachable from Helivanta, but the session keeps whatever lifetime Zitadel's
  own policy gives it, as the existing `bumpFactorAttempt` doc already states.
  Ending it upstream would be `DELETE /v2/sessions/{id}` per row: a Zitadel
  call inside a background loop for sessions that can no longer be used
  through Helivanta. That is a separate decision, not part of bounding the
  copy at rest.
- **Metrics.** The deleted count is logged. A metric belongs with the
  observability epic (#606).

## Tests

- An expired row inserted directly with SQL, so no `Put` or `Get` ever touches
  it, is removed by one sweep, and the sweep reports one deletion. This is the
  issue's own acceptance check.
- An unexpired row survives the sweep.
- Rows at every stage and in either enrolling state are swept once expired.
- The loop:
  - sweeps at boot with no tick;
  - sweeps again on the next tick, using a test interval;
  - returns when its context ends.
- A failing pass, against a closed database, logs an error and the loop keeps
  running: a later pass after the failure still runs.
- Mutations that must fail the suite:
  - the comparison inverted, which deletes live rows;
  - the boot sweep removed;
  - the loop never calling the sweep.
