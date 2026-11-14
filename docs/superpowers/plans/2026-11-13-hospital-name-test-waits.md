# Hospital name test waits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound the hospital-name race tests' waits, enforce their helpers' preconditions, and make the announcer callback self-contained.

**Architecture:** Test helpers in `hospital_name_receipts_test.go`; a two-line reset in `announceHospitalNames`.

**Tech Stack:** Go 1.27.4, gorm, Postgres, testify.

**Spec:** `docs/superpowers/specs/2026-11-13-hospital-name-test-waits-design.md` (#1670)

## Global Constraints

- `GOTOOLCHAIN=go1.27.4`, `-timeout=20m`.
- No change to redelivery behaviour, logs, subjects or payloads.

---

### Task 1: Test helpers

**Files:** Modify `backend/internal/modules/workforce/hospital_name_receipts_test.go`

- [ ] `passResult(t *testing.T, done <-chan error) error`: select on `done` or `time.After(30*time.Second)` → `t.Fatal("announcer pass did not return")`. Replace every `<-done`.
- [ ] `captureLogs` calls `t.Setenv("HELIVANTA_TEST_SLOG_CAPTURED", "1")` with a one-line why.
- [ ] `waitForLockWaiters(t, h, holder int64, n int64)` counts `pg_stat_activity` rows with `?=ANY(pg_blocking_pids(pid))`; both callers read `pg_backend_pid()` in their holding transaction.
- [ ] Mutations (each fails, then restore): drop one goroutine send (D1); add `t.Parallel()` to `TestRolledBackResendIsNotLogged` (D2); pass holder `0` (D3).
- [ ] Run the touched tests with `-race -count=3`, expect PASS. Commit `test(workforce): bound hospital name pass waits and scope the lock poll (#1670)`.

### Task 2: Callback reset

**Files:** Modify `backend/internal/modules/workforce/hospital_names.go`

- [ ] Reset `attempt, nameVersion = 0, 0` as the callback's first statement, with a one-line why; drop the later `attempt = 0`.
- [ ] Run the hospital-name tests, expect PASS. Commit `fix(workforce): reset hospital name re-send report per transaction (#1670)`.

### Task 3: Gates

- [ ] `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `go test -race ./internal/modules/workforce/`.
