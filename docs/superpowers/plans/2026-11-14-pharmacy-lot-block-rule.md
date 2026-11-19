# One lot block rule — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Issue:** #1634 (pharmacy part). The supply module's copies stay in #1634.

**Goal:** one database function decides whether a pharmacy lot may leave a
sellable shelf. Release, sale, the dispensable balance, supplier choices,
snapshots and the supplier hold and dispatch guards all ask it, on the outlet's
own date.

**Spec:** `docs/superpowers/specs/2026-11-14-pharmacy-inventory-lifecycle-design.md`
P4 and §3.14 ("0. Prerequisites"). Slice 0a of
`docs/superpowers/plans/2026-11-14-pharmacy-inventory-lifecycle.md`.

**Tech stack:** Go, gorm, PostgreSQL 16 (plpgsql), testcontainers.

## What exists today

The rule (expired, no MRP, safety incident) has these live copies:

| Site                                                    | Expiry                        | MRP                         | Incident | Date source                    |
| ------------------------------------------------------- | ----------------------------- | --------------------------- | -------- | ------------------------------ |
| `pharmacy_sellable_buckets` (`fulfilment_stock.sql:19`) | yes                           | yes                         | yes      | caller's `day`                 |
| `release.go:98-114`                                     | yes                           | yes                         | yes      | outlet zone, Go clock          |
| hold guard (`fulfilment_stock.sql:43`)                  | yes                           | **no**                      | yes      | fixed `Asia/Kolkata`, DB clock |
| dispatch guard (`fulfilment_stock.sql:56`)              | yes, from allocation evidence | **no**                      | **no**   | fixed `Asia/Kolkata`, DB clock |
| `fulfilment_safety.go:51`                               | yes                           | evidence comparison (`:47`) | yes      | fixed `Asia/Kolkata`, DB clock |
| `supplier_receiving_corrections.sql:28`                 | yes                           | —                           | —        | fixed `Asia/Kolkata`, DB clock |

The supplier paths pass `pharmacy_sellable_buckets` a fixed-zone date:
`fulfilment_choices.go:34` and `:67` (DB clock), and `fulfilment_stock.go:88`
(`time.Now()`, the API host's wall clock).

The dispatch guard therefore lets an incident lot leave if the Go check is
skipped, and the hold guard reserves a lot with no MRP.

**Incident checks fail open in the supplier fulfil frame.** The red run of
AC-0a-07, -08 and -15 showed it. `operating_receipts.sql:15` widened locations,
batches, movements and catalogue products to `pharmacy_operating_stock_scope`, and
`fulfilment_stock.sql:2` added `pharmacy.stock.fulfil` to that scope. The incident
policy (`batch_safety.sql:25`, widened only for receiving review at
`supplier_receiving_release.sql:26`) was never widened. In that frame the lot is
visible and its incidents are not, so every `NOT EXISTS(incident)` passes.
Supplier choices offer an incident lot, the hold guard reserves it, and the
receiving pre-check (`WithSafeShipment`) accepts a shipment of it. The stock
overview and non-review hospital receiving frames have the same gap.

`dispensable_units` (#1714) already calls `sellableBuckets`, so it follows the
new rule with no change.

## Decisions

- **D1. The function.** `pharmacy_lot_block_reason(tenant uuid, outlet uuid,
batch uuid, day date) RETURNS text`, plpgsql, `STABLE`.
  - It returns `expired` (`expires_on <= day`), `mrp_missing` or
    `safety_incident`, checked in that order, or null when the lot is not
    blocked.
  - It raises `22004` for a null `day` and `P0002` for a lot it cannot see.
    Fail closed: null means "may sell", so a call it cannot answer must abort
    the transaction, never return null.
  - Order matters only for the reason reported. Any reason blocks.
- **D2. The outlet's date.** `pharmacy_outlet_today(tenant uuid, outlet uuid)
RETURNS date`, plpgsql, `VOLATILE`. It is
  `(clock_timestamp() AT TIME ZONE pharmacy_outlets.timezone)::date`, and raises
  `P0002` when the outlet is not visible.
- **D3. Callers keep their clock.** Go paths that already use the injected
  `clock()` (release, balance, sale) pass `outletLocalDate`. Database paths
  (the guards, supplier choices, snapshot and the dispatch pre-check) use
  `pharmacy_outlet_today`, so the read and the guard that follows use the same
  date. No caller computes a date in a fixed zone.
- **D4. Guards.**
  - The hold guard replaces its expiry and incident checks with
    `pharmacy_lot_block_reason(…) IS NOT NULL`. It gains the MRP check.
  - The dispatch guard does the same on `NEW.batch_id`. It gains the MRP and
    incident checks, so the database alone refuses an unsafe dispatch.
  - Both keep their error text and `23514`, which the store maps to
    `ErrConflict`.
- **D5. Correction guard.** Only its date changes. The migration rewrites the
  one expression in the live definition through `pg_get_functiondef` and
  fails if it does not replace exactly that expression. Copying its 40 lines
  to change one is the larger risk.
- **D6. Release text.** Each reason keeps today's `errNotReleasable` message. A
  reason with no message yet (a later slice's) still refuses with `409`.
- **D7. Incidents are visible wherever their lot is.** The incident policy's
  `USING` becomes `hms_tenant_visible(tenant_id) AND EXISTS(the incident's batch,
under the batch's own policy)`. Today's two disjuncts are subsets of it. Any
  later widening of batch visibility carries incidents with it, so the block rule
  cannot see a lot without seeing its incidents. A fourth hand-added scope would
  leave the next frame to fail open the same way. `WITH CHECK` is unchanged:
  only outlet staff record incidents. An incident carries no patient data. Its
  `actor_subject` is already visible on the same frame's movements.

## Global constraints

- One migration, `0051_pharmacy_lot_block_rule`, append-only.
- No new route, permission, event or error code.
- `pharmacy_batch_safety_incidents` is read only by
  `pharmacy_lot_block_reason` and by the batch-safety module's own code.

## Acceptance tests

All run on real PostgreSQL through `testutil.NewHarness`. A "fixture" is a
`db.Migrate` test step that disables the batch immutability trigger to simulate
legacy data or the calendar moving on, as `leave_routing_test.go` does.

| ID       | Given                                                                                                                            | When                                                       | Then                                                                                                                                     | Test                                                                              |
| -------- | -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| AC-0a-01 | Four lots: expired with no MRP and an incident; unexpired with no MRP and an incident; unexpired with MRP and an incident; clean | `pharmacy_lot_block_reason` on the outlet's date           | `expired`, `mrp_missing`, `safety_incident`, null                                                                                        | `block_rule_test.go` `TestLotBlockReasonReportsExpiryThenMRPThenIncident`         |
| AC-0a-02 | A clean lot expiring on 2030-10-31                                                                                               | The function for 2030-10-30 and 2030-10-31                 | null, then `expired`                                                                                                                     | `TestLotBlockReasonTreatsTheExpiryDayAsExpired`                                   |
| AC-0a-03 | A null day; an unknown lot                                                                                                       | The function                                               | `22004`; `P0002`. Never null                                                                                                             | `TestLotBlockReasonFailsClosed`                                                   |
| AC-0a-04 | One outlet set to `Pacific/Kiritimati` (UTC+14), another to `Pacific/Pago_Pago` (UTC−11)                                         | `pharmacy_outlet_today` for each                           | Each equals `clock_timestamp()` in its zone, and the two differ                                                                          | `TestOutletTodayUsesTheOutletsOwnZone`                                            |
| AC-0a-05 | 30 units on a sellable shelf, then each reason applied                                                                           | `GET …/stock/balance`                                      | `dispensable_units` 0 for each reason, 30 for a clean control lot                                                                        | `TestEveryBlockReasonStopsDispensing`                                             |
| AC-0a-06 | A quarantine lot with each reason                                                                                                | `PUT …/stock/releases/{id}`                                | `409` with today's text; nothing moves                                                                                                   | `release_test.go` `TestUnsafeBatchesAreNotReleased` (adds the MRP case)           |
| AC-0a-07 | An outlet's supplier stock, then each reason applied                                                                             | `Choices`, `Locations`, `Snapshot`                         | The lot is not offered; `Snapshot` reports 0 available                                                                                   | `globalapi/supplier_block_rule_test.go` `TestSupplierStockNeverOffersABlockedLot` |
| AC-0a-08 | Each reason applied before reserving                                                                                             | `store.Create`                                             | `ErrConflict`; no hold row                                                                                                               | `TestSupplierHoldRefusesEveryBlockReason`                                         |
| AC-0a-09 | A live hold, then each reason applied                                                                                            | `store.Command` dispatch, bypassing the Go pre-check       | `ErrConflict`; no `supplier_dispatch` movement                                                                                           | `TestSupplierDispatchGuardRefusesEveryBlockReason`                                |
| AC-0a-10 | The shipment pre-check (`WithSafeShipment`, `fulfilment_safety.go`)                                                              | Scan pharmacy Go sources                                   | It asks `pharmacy_lot_block_reason`; no Go file but `batch_safety.go` names `pharmacy_batch_safety_incidents`                            | `TestPharmacyDatesUseTheOutletsZone` (with AC-0a-12)                              |
| AC-0a-11 | The migrated database                                                                                                            | Scan `pg_proc` bodies of `pharmacy_%` functions            | Only `pharmacy_lot_block_reason` (and batch-safety's own functions) read `pharmacy_batch_safety_incidents`; none contains `Asia/Kolkata` | `TestOnlyTheBlockRuleReadsSafetyIncidents`                                        |
| AC-0a-12 | Pharmacy Go sources                                                                                                              | Scan non-test `.go` files                                  | No `time.FixedZone` and no `"Asia/Kolkata"` outside the new-outlet default in `customer_workspace.go`                                    | `TestPharmacyDatesUseTheOutletsZone`                                              |
| AC-0a-13 | Existing supplier and receiving correction suites                                                                                | `go test ./internal/globalapi ./internal/modules/pharmacy` | Green, unchanged                                                                                                                         | —                                                                                 |
| AC-0a-14 | Each new guard                                                                                                                   | Remove its call to the function (mutation)                 | AC-0a-01, -05, -06, -07, -08, -09 or -15 fails as listed in the PR                                                                       | Recorded in the PR body                                                           |
| AC-0a-15 | A shipment dispatched from a clean lot, then each reason applied                                                                 | The real `WithSafeShipment`                                | `ErrConflict`; the receiving action never runs. A clean lot is received                                                                  | `TestShipmentSafetyRefusesEveryBlockReason`                                       |

## Tasks

### Task 1: the function and the outlet date (D1, D2)

**Files:** `backend/internal/modules/pharmacy/lot_block_rule.sql`, `module.go`,
`block_rule_test.go`

- [x] Write AC-0a-01 to -04. Run: they fail, because the functions do not
      exist.
- [x] Add the migration with both functions. Run: green.
- [x] Mutation: swap the MRP and incident order; AC-0a-01 fails.

### Task 2: sale, balance and release (D3, D6)

**Files:** `lot_block_rule.sql`, `release.go`, `block_rule.go`,
`block_rule_test.go`, `release_test.go`

- [x] Write AC-0a-05 and the MRP case of AC-0a-06. Run against today's code:
      AC-0a-05 passes (the copy agrees) and the MRP release case passes.
      Record that both are characterisation tests that must stay green.
      (Run on a `07af22ab` snapshot with only these tests copied in: both
      pass.)
- [x] Redefine `pharmacy_sellable_buckets` to call the function. Replace
      `release.go:98-114` with one call through `lotBlockReason`.
- [x] Mutation: drop the incident branch from the function; AC-0a-05 and
      AC-0a-06 fail.

### Task 3: supplier reads and guards (D3, D4)

**Files:** `lot_block_rule.sql`, `fulfilment_choices.go`, `fulfilment_stock.go`,
`fulfilment_safety.go`, `globalapi/supplier_block_rule_test.go`

- [x] Write AC-0a-07 to -09 and -15. Red run: AC-0a-07 offers the
      `safety_incident` lot; AC-0a-08 accepts `mrp_missing` and
      `safety_incident`; AC-0a-09 accepts all three; AC-0a-15 receives the
      `safety_incident` lot. AC-0a-09's `expired` case fails only because the
      fixture moves the batch's expiry, not the allocation's copied evidence;
      on a real calendar the old guard caught it.
- [x] Redefine the hold and dispatch guards. Widen the incident policy (D7).
      Supplier reads use `pharmacy_outlet_today`. `fulfilment_safety.go` calls
      the function.
- [x] Run: green. Mutation: restore the old dispatch guard; AC-0a-09 fails
      (the red run above is that guard; removing the new call fails all three
      reasons).

### Task 4: the correction guard and the audits (D5)

**Files:** `lot_block_rule.sql`, `block_rule_test.go`

- [x] Write AC-0a-10 to -12. Run: they fail and list today's copies.
- [x] Rewrite the correction guard's date. Run: green, and AC-0a-13 green
      (`go test -p 1 -timeout=20m` of both packages: pharmacy 349s, globalapi
      838s; globalapi needs more than the default 10m).

### Task 5: gates

- [x] `make lint-go`: 0 issues. `go run ./cmd/contractdocs -check`: green
      after regenerating `docs/api/reference/inventory.json`, whose only
      change is the 0051 migration entry and its SQL hash.
- [ ] `./scripts/coverage-gate.sh` (runs `go test -race ./...`): not green
      locally, so the 70% floor was never evaluated. With `GOFLAGS=-p=1`,
      63 packages pass and 3 fail. None of the failures is in pharmacy or
      globalapi: - `customeronboarding` `TestProtectedStaffAccountJourney…` needs
      `STAFF_IDENTITY_OWNER_SCHEMA`, which only CI prepares. It also fails
      on `07af22ab`. - `workforce` `TestMigrationMakesExistingMarkersDue` also fails on
      `07af22ab` when run alone. Cause not determined. - The rest are Docker connection drops on the 2-CPU, 2 GiB Colima VM.
      The two with assertion messages (`practitioner`
      `TestProfessionalEvidenceConcurrentRevocation…`, `workforce`
      `TestEnrollmentAcceptsDirectoryOnlySupervisor`) pass alone on both
      trees.
      CI's run of the gate is the deciding one.
- [x] Remove build artifacts created by validation. No ignored files
      appeared in the worktree. The one test container stranded in
      `Created` by a Docker drop was removed.

## Does not cover

- The supply module's copies (`supply/fulfilment_choices.go:21`,
  `supply/fulfilment_stock.sql:37`, `supply/stock_quality.sql:68`,
  `supply/stock_quality_rules.go:21`). They stay in #1634.
- The register's `expiry_status`/`expiry_band`
  (`stock_register.go:263`). It is a report band with its own `today` value,
  not a block decision.
- Incident decisions and recalls. Slices C/D and G change this function.

## Mutation results (AC-0a-14)

Each mutation applied alone, then restored byte-for-byte.

| Mutation                                   | Failing tests                                      |
| ------------------------------------------ | -------------------------------------------------- |
| Swap the MRP and incident order            | AC-0a-01                                           |
| Drop the incident branch                   | AC-0a-01, -05, -06, -07, -08, -09, -11, -15        |
| `pharmacy_sellable_buckets` skips the rule | AC-0a-05, -07                                      |
| Hold guard skips the rule                  | AC-0a-08 (all three reasons)                       |
| Dispatch guard skips the rule              | AC-0a-09 (all three reasons)                       |
| Incident policy not widened (D7)           | AC-0a-07, AC-0a-08/-09/-15 `safety_incident`       |
| Correction guard keeps the fixed zone      | AC-0a-11                                           |
| Shipment pre-check skips the rule          | AC-0a-10, AC-0a-15 `expired` and `safety_incident` |
| Release skips the rule                     | AC-0a-06                                           |

The shipment pre-check mutation leaves AC-0a-15 `mrp_missing` green: the
evidence comparison at `fulfilment_safety.go:47` already refuses a null MRP.
