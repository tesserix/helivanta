# Units available to dispense Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The balance reports what a sale could take, and snapshots stop reporting a hard-coded 0.

**Architecture:** `getBalance` reuses `sellableBuckets` inside its read transaction. Register, overview and CSV drop the column. The UI and types follow.

**Tech Stack:** Go 1.27.4, gorm, Postgres; Next.js, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-14-pharmacy-dispensable-units-design.md` (#1708)

## Global Constraints

- No second copy of the sellability rules: only `pharmacy_sellable_buckets`.
- No new route, permission, migration or event.

---

### Task 1: The balance computes it (D1)

**Files:** `backend/internal/modules/pharmacy/inventory.go`, `dispensable_test.go`, `release_test.go`

- [ ] Tests: `TestBalanceReportsUnitsASaleCouldTake` (quarantine 0, sellable 30), `TestBalanceExcludesABatchWithASafetyIncident`, `TestBalanceExcludesStockExpiredAtTheOutlet`. Run: they fail against the hard-coded 0.
- [ ] Implement: read the batch's product, the outlet's local date, then `sellableBuckets`; pick the location and batch.
- [ ] Mutation: report on-hand as dispensable; all three fail.

### Task 2: Snapshots drop it (D2)

**Files:** `stock_register.go`, `stock_overview.go`, `stock_csv.go` and their tests

- [ ] Replace the `== 0` assertions with absence assertions. Shorten the CSV tail check to 7 columns.
- [ ] Mutation: restore the struct field and CSV column; the register, export, expiry and footing tests fail.

### Task 3: UI, types and docs (D3)

**Files:** `packages/api/src/pharmacy.ts`, `stock-panel.tsx`, `stock-register.tsx` and their tests; `docs/api/domains/pharmacy.md`; the counter acceptance plan

- [ ] Panel test: the figure and "Before outlet and product checks." Register test: no "Dispensable" column header. Mutation: restore the header; the test fails.
- [ ] Docs: the balance row, register and overview text, CSV columns. Mark F-5 done; extend STK-11 and STK-24.

### Task 4: Gates

- [ ] `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `go run ./cmd/contractdocs -check`, `pnpm turbo lint type-check test build format:check`.
