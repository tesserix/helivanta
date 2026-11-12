# Hospital name redelivery hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove every #1653 guarantee with a test that can fail, and make the announcer's log and errors describe only what happened.

**Architecture:** One production change in `announceHospitalNames` (log after commit, `%w` step wrapping). Everything else is test hardening in the workforce package and archtest.

**Tech Stack:** Go 1.27.4, gorm, Postgres, testify 1.12.

**Spec:** `docs/superpowers/specs/2026-11-12-hospital-name-redelivery-hardening-design.md` (#1660)

## Global Constraints

- `GOTOOLCHAIN=go1.27.4`, `-timeout=20m`.
- No change to redelivery behaviour, backoff, subjects or payloads.
- Log message and fields unchanged: `re-sent unconfirmed hospital name` with `tenant_id`, `hospital_id`, `name_version`, `attempt`.
- Background test passes use `t.Context()`.

## Review Focus

- A cancelled pass must stay silent after wrapping (`errors.Is(err, context.Canceled)`).
- The D4 race test must not hang if a pass never reaches the lock: bound every wait.
- The workflows stub must never be called by a consumer constructor; a constructor that calls it panics the archtest.
- `pg_locks` counts must be scoped to this test's database so parallel packages cannot satisfy them.
- The deferred trigger in the D1 test must be dropped in cleanup so other tests in the package are unaffected.

---

### Task 1: Announcer logs after commit and names its failing step

**Files:**

- Modify: `backend/internal/modules/workforce/hospital_names.go`
- Test: `backend/internal/modules/workforce/hospital_name_receipts_test.go`

- [ ] **Step 1: Write the failing tests**
  - `TestRolledBackResendIsNotLogged`: announce, make due, create (via `WithAdmin`) a `DEFERRABLE INITIALLY DEFERRED` constraint trigger on `workforce_hospital_name_announcements` that raises on UPDATE, dropped in `t.Cleanup`. Capture slog to a buffer (`slog.SetDefault`, restored in cleanup). Run one pass. Assert the buffer contains `retrying later` and not `re-sent unconfirmed hospital name`, `attempts` is still 0, and one `hospital_named` row exists.
  - `TestCancelledScanNamesItsStep`: `ctx, cancel := context.WithCancel(t.Context()); cancel()`; `err := workforce.AnnounceHospitalNamesOnce(ctx, h.db)`; assert `ErrorIs(err, context.Canceled)` and `ErrorContains(err, "scan due hospital names")`.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/modules/workforce/ -run 'TestRolledBackResendIsNotLogged|TestCancelledScanNamesItsStep'`: the log test sees the warning; the scan test lacks the step name.
- [ ] **Step 3: Implement** — hoist `attempt` and `nameVersion` outside the callback; log after `WithTenant` returns nil. Wrap each error with `fmt.Errorf("<step>: %w", err)` using the spec's D2 step names.
- [ ] **Step 4: Run the workforce hospital-name tests, expect PASS** — `-run 'HospitalName|Announcer|Receipt|Resend|Rename|Confirm|Migration|Due|Claim|FirstAnnouncements'`.
- [ ] **Step 5: Commit** `fix(workforce): log hospital name re-sends after commit (#1660)`.

### Task 2: Workforce tests force the race, stay on goroutine, check the migration

**Files:**

- Modify: `backend/internal/modules/workforce/hospital_name_receipts_test.go`, `backend/internal/modules/workforce/hospital_names_test.go`

- [ ] **Step 1: Rewrite `TestResendRacingPassesSendOnce`** per D4: hold the hospital `FOR UPDATE` in `h.tx`, start two passes with `t.Context()`, wait with `require.EventuallyWithT` until `pg_locks` shows at least two ungranted locks in `current_database()`, then release. Assert two `hospital_named` rows total and `attempts == 1`.
- [ ] **Step 2: Mutation check** — temporarily drop `AND next_attempt_at<=now()` from the locked re-check: the test fails (three rows). Restore.
- [ ] **Step 3: `TestConfirmationDuringAPassStopsItsResend`** — `EventuallyWithT` with `assert.NoError(c, …)`, and pass `t.Context()`. Factor the lock-wait poll into one helper `waitForLockWaiters(t, h, n)` used by both tests.
- [ ] **Step 4: `TestMigrationMakesExistingMarkersDue`** — after the migration, assert the `pg_indexes` definition and `has_column_privilege` per D8. Mutation: remove the index line, then the grant line, from a copy of the migration SQL fed to `db.Migrate`; each fails the test.
- [ ] **Step 5: `announced` helper** — `Order("created_at, id")` with a comment.
- [ ] **Step 6: Run the workforce package with `-race -count=3` on the touched tests, expect PASS.**
- [ ] **Step 7: Commit** `test(workforce): force the hospital name re-send race and check 0101 (#1660)`.

### Task 3: The single-consumer guard decides, over both consumer lists

**Files:**

- Modify: `backend/internal/archtest/arch_test.go`

- [ ] **Step 1: Write the failing proof test** `TestOneConfirmingConsumerRejectsAWorkflowOnlyConsumer`: a stub module whose `Consumers(deps)` returns a second `SubjectHospitalNamed` consumer only when `deps.Workflows != nil`; require `oneConfirmingConsumer(append(allModules(), stub))` to return an error containing `secondConfirmerMsg`.
- [ ] **Step 2: Run, expect FAIL** (undefined function).
- [ ] **Step 3: Implement `oneConfirmingConsumer(mods []platform.Module) error`**, collecting from `platform.Deps{}` and `platform.Deps{Workflows: workflowsStub{}}` (`type workflowsStub struct{ client.Client }`), deduplicated, sorted; error lists the names. Replace `confirmingConsumers`; `TestHospitalNamedHasOneConfirmingConsumer` requires `NoError`; the existing proof test requires an error for a plain second consumer.
- [ ] **Step 4: Mutation check** — make the guard read only `platform.Deps{}`: the workflow-only test fails. Restore.
- [ ] **Step 5: Run `go test -race ./internal/archtest/`, expect PASS.**
- [ ] **Step 6: Commit** `test(archtest): the hospital name guard rejects a workflow-only consumer (#1660)`.

### Task 4: Gates

- [ ] `make lint-go`, `make lint-shell`, `cd backend && ./scripts/coverage-gate.sh`, `go run ./cmd/contractdocs -check`, `node scripts/staff-operational-access-inventory.mjs --check`, `pnpm turbo format:check`.
