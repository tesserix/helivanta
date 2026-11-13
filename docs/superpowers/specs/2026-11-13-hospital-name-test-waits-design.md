# Hospital name test waits and helpers — design

- **Issue:** #1670 (follow-up to #1660, PR #1662)
- **Date:** 2026-11-13
- **Status:** proposed

## Problem

The #1660 final review deferred four minor findings. Two race tests wait on
their background passes without a bound, the log-capture helper is safe only
by convention, the lock-wait poll counts any ungranted lock in the test
database, and the announcer's per-hospital callback is correct only while
`WithTenant` never re-runs it.

## Decisions

### D1. Background passes are awaited with a bound

`passResult(t, done)` receives one result from a background pass, or fails
the test after 30 seconds with `announcer pass did not return`. Both race
tests use it instead of a bare `<-done`. A stuck pass fails its own test
instead of hanging the package until the 20-minute timeout.

### D2. Log capture refuses a parallel test

`captureLogs` calls `t.Setenv`. Go panics when a test using `t.Setenv` is,
or later becomes, parallel, and parallel tests never run while a sequential
test is running. The global `slog` swap is then structurally exclusive.

### D3. The lock-wait poll counts only sessions the test is blocking

The holding transaction reads its `pg_backend_pid()`.
`waitForLockWaiters(t, h, holder, n)` counts sessions with
`holder = ANY(pg_blocking_pids(pid))`, so only passes queued behind the
test's own lock satisfy it.

### D4. The callback resets what it reports

`attempt` and `nameVersion` are reset at the start of the `WithTenant`
callback, so each run of it reports only its own re-send. `WithTenant` does
not retry today (`pkg/tenantdb/db.go`), so no test can re-run the callback;
the reset is argued in a comment at the decision point instead.

## Out of scope

Any behaviour change to redelivery, its logs or its contracts.

## Tests

- D1: a pass that never sends makes the race test fail after the bound.
- D2: adding `t.Parallel()` to `TestRolledBackResendIsNotLogged` panics.
- D3: passing a pid that holds no lock makes the wait fail.
- D4: none possible without a retrying `WithTenant`; stated in the PR.
