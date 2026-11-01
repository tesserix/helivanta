# Cash pickups and counting by note Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record cash taken from an open drawer to the safe, and close shifts by a count of notes and coins.

**Architecture:** Migration `0013_billing` (`pickups.sql`) adds the denomination and count functions, `billing_cash_pickups`, `declared_counts` on shifts, and replaces `billing_shift_expected` and `billing_shift_guard`. Handlers in `pickups.go`; the close in `cash.go` takes a count. Shell: a shared count component in `cash-desk.tsx`.

**Tech Stack:** Go/Gin/GORM, PostgreSQL 16 triggers, Next.js, zod.

**Spec:** `docs/superpowers/specs/2026-11-02-cash-pickups-design.md`

## Global Constraints

- Migration ID `0013_billing`; append-only; one permissive RLS policy per table.
- Count keys `note:<paise>`/`coin:<paise>`, counts 0–1,000,000.
- Errors: `own_pickup`, `drawer_short`, `over_pickup`, `shift_closed` (409); bad count 400.
- Pickup and correction routes need `billing.shift.authorise`; the denomination list `billing.account.read`.

## Review Focus

- A pickup racing a refund or a void: both must see each other (counter lock).
- A pickup racing the close: the close's expected cash must include it or the pickup must fail.
- A correction on a correction, or on another shift's pickup.
- The close replay with the same count after a lost response.
- The collection report blinding open shifts' pickups.

---

### Task 1: Schema

**Files:** Create `backend/internal/modules/billing/pickups.sql`; modify `module.go` (embed, `0013_billing`).

- [ ] Test `TestCountsAndPickupsHoldInTheDatabase` (in `pickups_test.go`): unknown key / negative count → total null; own-shift pickup insert fails `billing_cash_pickup_own`; over-drawer `billing_cash_pickup_drawer`; correction over pickup `billing_cash_pickup_over`; closed shift `billing_cash_pickup_shift`; shift close without counts refused; update/delete of a pickup refused.
- [ ] Implement; run `go test -race -run TestCountsAndPickups ./internal/modules/billing/` → PASS.

### Task 2: API

**Files:** Create `pickups.go`; modify `cash.go` (close takes `counts`, shift gains `picked_up_minor`, `declared_counts`), `module.go` routes, `collections.go` (`picked_up_minor`, CSV `picked_up`).

- [ ] Tests in `pickups_test.go`: `TestAPickupLowersTheExpectedCash` (close after pickup matches, refund over the remaining drawer `drawer_short`), `TestAPickupIsNeverTheCashiersOwn`, `TestACorrectionPutsBackWhatWasOverstated`, `TestTheCloseCountsByNote` (replay, different count 409, bad key 400), `TestPickupsAndRefundsRaceForTheDrawer`; collection test asserts `picked_up_minor` and CSV column.
- [ ] Update existing close callers in tests to send counts.
- [ ] `go run ./cmd/contractdocs`, lint, race tests → green.

### Task 3: Shell

**Files:** `packages/api/src/billing.ts` (types, `cashCountSchema`, `pickupCorrectionSchema`), `cash-desk.tsx` (`CashCount`, close by count, `OpenShifts` with pickups), `daily-collection.tsx`; tests in `cash-desk.test.tsx`, `daily-collection.test.tsx`.

- [ ] Tests: close sends `{counts}` and shows the running total; pickup form posts counts; own shift offers no pickup; correction posts amount and reason; collection shows pickups.
- [ ] Frontend gates green.

### Task 4: Docs

`docs/api/domains/billing.md` (section, "Not covered" lines), portfolio, generated reference, QA inventory.
