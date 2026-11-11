# Stock reconciliation totals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The stock reconciliation, register rows and stock CSV carry a signed
total for every movement kind, so Opening + the parts = Closing (and on-hand =
the parts) on every response, enforced by tests (#1638).

**Architecture:** One Go list of reconciled movement kinds generates the
`FILTER` columns of the reconciliation query and the positions source; a
database test pins that list to the live kind constraint. The panel renders
one signed line per kind and refuses to present figures that do not sum.

**Tech Stack:** Go (gin, gorm, Postgres RLS), Next.js shell, Vitest,
Playwright.

**Spec:** `docs/superpowers/specs/2026-11-10-pharmacy-stock-reconciliation-totals-design.md`

## Global Constraints

- Kinds, in this order: `receipt`, `correction`, `release`, `sale`, `return`, `supplier_dispatch`.
- New JSON fields: `release_units`, `sale_units`, `return_units`, `supplier_dispatch_units`; decimal strings like `receipt_units`. No existing field changes.
- Totals are signed sums of stored `quantity_units`; no sign is flipped.
- New CSV columns are appended after `dispensable_units`, in kind order.
- No route, permission, event or migration changes.
- Screen labels: "Opening stock", "Receipts in period", "Corrections in period", "Releases in period", "Sales in period", "Returns in period", "Supplied to hospital orders in period", "Closing stock".
- Formula copy: "Opening + the movements in this period = closing stock. Sales and supplies are negative; receipts and returns are positive; releases are negative where stock left and positive where it arrived."
- Mismatch alert copy: "These figures do not add up. Do not use this reconciliation; report it."
- Frontend arithmetic on the totals uses `BigInt`, never `Number`.

## Review Focus

1. A `from` inside the history: opening non-zero and every in-period kind excludes pre-`from` movements — Task 1's identity test uses a `from` between the receipt and the release.
2. A release seen from both ends: negative at quarantine, positive at the shelf, and the two reconciliations each foot — Task 1 asserts both locations.
3. A movement exactly at `from` or `as_of`: counted in period, inclusive at both ends, as the existing totals are — Task 1 sets `from` to the release's `created_at` and asserts the release is in period.
4. A response with an unknown or missing part (older server, new kind): the panel must not claim reconciliation — Task 3's mismatch test omits `sale_units`.
5. Values beyond 2^53 in the strings: summed exactly — Task 3's mismatch test uses `"9007199254740993"`.

---

### Task 1: Reconciliation totals for every kind

**Files:**

- Modify: `backend/internal/modules/pharmacy/stock_register.go:168-221`
- Modify: `backend/internal/modules/pharmacy/export_test.go` (expose the kind list)
- Test: `backend/internal/modules/pharmacy/stock_register_test.go`

**Interfaces:**

- Produces: `var stockMovementKinds = []string{"receipt","correction","release","sale","return","supplier_dispatch"}`, exported to tests as `pharmacy.StockMovementKinds`; `func stockKindTotals(prefix string) string` returning the `COALESCE(sum(<prefix>quantity_units) FILTER(WHERE <cond> AND <prefix>kind='<k>'),0)::text AS <k>_units` column list joined by commas, where the period condition is passed in by the caller (Task 2 uses no period); `stockReconciliationTotals` gains `ReleaseUnits`, `SaleUnits`, `ReturnUnits`, `SupplierDispatchUnits string` with the Global Constraints JSON names.

- [ ] **Step 1: Write the failing tests**

`TestStockMovementKindsMatchTheDatabaseConstraint`: read
`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='pharmacy_stock_movements_kind_check'`
through `catalogueTx`, extract the quoted words, `require.ElementsMatch` with
`pharmacy.StockMovementKinds`.

`TestStockReconciliationFootsEveryMovementKind` on `newSaleWorld(t, true)`:
`product` "TEST-RECON-1" class "none"; `receive` 100 at quarantine; a
correction of −3 at quarantine; release 30 quarantine→sellable (keep its
`created_at` from the movements list as `from`); `soldSale` 10; a return of 4
into quarantine via `returnUnits(returnBody(...))`; a `supplier_dispatch` of
−5 at the sellable location inserted through `catalogueTx` after setting
`app.pharmacy_stock_outlet`, `app.pharmacy_stock_pharmacy`,
`app.pharmacy_stock_access='authorized'`, `app.pharmacy_stock_permission='pharmacy.stock.fulfil'`
(the WITH CHECK branch in `fulfilment_stock.sql:14`), `source_reference`
`supplier-dispatch:<id>`. Then, `as_of` = the latest movement's `created_at`:

- quarantine with `from` = release time: opening 97, receipt "0", correction "0", release "-30", sale "0", return "4", supplier_dispatch "0", closing 71.
- sellable, same window: opening 0, release "30", sale "-10", supplier_dispatch "-5", closing 15.
- for both, `opening + Σ parse(kind) == closing` computed over `pharmacy.StockMovementKinds` by reading the response as `map[string]any`, so a missing field fails.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && GOTOOLCHAIN=go1.27.3 go test ./internal/modules/pharmacy -run 'TestStockMovementKindsMatch|TestStockReconciliationFoots' -count=1`
Expected: FAIL — `undefined: pharmacy.StockMovementKinds`.

- [ ] **Step 3: Implement**

Add the list and `stockKindTotals`; rebuild the reconciliation SQL from it with
one named `@from` / `@as_of` (gorm `sql.Named`) instead of the repeated
positional `from` arguments. Comment at the list: it must equal the kind
constraint, which the test enforces. If the dispatch insert is refused by RLS,
ledger a ruling and insert it the way `fulfilment_stock.go:127` scopes its
transaction — do not drop the kind from the test.

- [ ] **Step 4: Run to verify they pass, plus the existing register tests**

Run: `cd backend && GOTOOLCHAIN=go1.27.3 go test ./internal/modules/pharmacy -run 'TestStock' -count=1`
Expected: PASS, including `TestStockRegisterReconcilesExactPeriodAndNeverAdvertisesUsableStock` unchanged.

- [ ] **Step 5: Mutation check** — drop `return` from `stockMovementKinds`; both new tests fail. Restore.

- [ ] **Step 6: Commit** `feat(pharmacy): reconcile every stock movement kind (#1638)`

### Task 2: Register rows, overview and CSV

**Files:**

- Modify: `backend/internal/modules/pharmacy/stock_register.go:76-84,247-250` (`stockPosition`, `stockPositionsSource`)
- Modify: `backend/internal/modules/pharmacy/stock_overview.go:159` (column list)
- Modify: `backend/internal/modules/pharmacy/stock_csv.go:14,18`
- Test: `backend/internal/modules/pharmacy/stock_register_test.go`, `stock_export_test.go`

**Interfaces:**

- Consumes: `stockMovementKinds`, `stockKindTotals` (Task 1).
- Produces: `stockPosition` gains the four string fields with the same JSON names.

- [ ] **Step 1: Write the failing test** — extend Task 1's world into `TestStockPositionsAndExportFootEveryMovementKind`: `GET /stock/positions?as_of=…` and the CSV export; for each row, `on_hand_units == Σ kind totals` and the sellable row has release "30", sale "-10", supplier_dispatch "-5"; the CSV header ends `…,dispensable_units,release_units,sale_units,return_units,supplier_dispatch_units`.
- [ ] **Step 2: Run to verify it fails** — same command with `-run TestStockPositionsAndExportFoot`; Expected: FAIL on the missing `release_units`.
- [ ] **Step 3: Implement** — positions source uses `stockKindTotals("m.")` with no period condition; overview's column list and CSV header/row append the four fields.
- [ ] **Step 4: Run** `go test ./internal/modules/pharmacy -run 'TestStock' -count=1`; Expected: PASS, existing export tests unchanged.
- [ ] **Step 5: Commit** `feat(pharmacy): register rows and stock CSV carry every movement kind (#1638)`

### Task 3: Panel shows every signed part and refuses a mismatch

**Files:**

- Modify: `packages/api/src/pharmacy.ts:140-159`
- Modify: `apps/shell/components/pharmacy/stock-register.tsx:395-402,567-597`
- Test: `apps/shell/components/pharmacy/stock-register.test.tsx`

**Interfaces:**

- Consumes: the JSON fields from Tasks 1 and 2.
- Produces: the position row type and `PharmacyStockReconciliation` gain the four `string` fields.

- [ ] **Step 1: Write the failing tests**
  - Update the reconciliation mock (line ~104) to a footing response: opening 10, receipt "2", correction "-3", release "-4", sale "0", return "1", supplier_dispatch "0", closing 6. `it("shows every movement kind with its sign and the formula")`: each Global Constraints label is present, values `+2`, `−3`, `−4`, `0`, `+1`, the formula copy is present, no `alert`.
  - `it("does not present figures that do not add up")`: a response without `sale_units` and one with `receipt_units: "9007199254740993"`, closing 10 — each shows the mismatch alert copy as `role="alert"`.
  - The expanded register row shows the four new columns.
- [ ] **Step 2: Run** `pnpm --filter shell exec vitest run components/pharmacy/stock-register.test.tsx`; Expected: FAIL on the missing labels.
- [ ] **Step 3: Implement** — a `reconciles(data): boolean` helper summing with `BigInt` over the six fields (any missing or non-integer string → false); signed rendering via the existing `quantity()` with a `+` for positives and U+2212 for negatives; replace the old formula text.
- [ ] **Step 4: Run** the same command; Expected: PASS. Mutation: make `reconciles` return true; the mismatch test fails. Restore.
- [ ] **Step 5: Commit** `feat(pharmacy): the reconciliation shows every movement and refuses a mismatch (#1638)`

### Task 4: Docs, acceptance plan and browser check

**Files:**

- Modify: `docs/api/domains/pharmacy.md` (stock register/reconciliation section; hand-edit, do not run prettier on this file)
- Modify: `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md` (STK-20, F-4)
- Test: `e2e/tests/pharmacy-stock-reconciliation.spec.ts` (new)

- [ ] **Step 1: Write the e2e test** — `readyOutlet` (20 received at quarantine, released to the shelf), then a 2-unit cash sale by the cashier through the API as `pharmacy-payment-return.spec.ts` does; open the shelf lot's reconciliation the way `pharmacy-stock-pane.spec.ts:103-106` does; expect "Releases in period" `+20`, "Sales in period" `−2`, "Closing stock" `18`, and no alert.
- [ ] **Step 2: Run it** against the local stack: `pnpm --filter e2e exec playwright test pharmacy-stock-reconciliation`; Expected: PASS (it would fail on main: no "Releases in period").
- [ ] **Step 3: Docs** — the domain doc lists the reconciliation fields, signs and the identity, and the CSV's appended columns. STK-20's expectation becomes "Opening + the parts = Closing on Q1 and S1"; F-4's action "Done in #<PR> (#1638); verify with STK-20".
- [ ] **Step 4: Gate** — `pnpm turbo lint type-check test build format:check --continue --concurrency=4`, `make lint-shell`, `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `GOTOOLCHAIN=go1.27.3 go run ./cmd/contractdocs -check`. Expected: all green, contractdocs reports no drift.
- [ ] **Step 5: Commit** `docs(pharmacy): reconciliation totals and STK-20 (#1638)`
