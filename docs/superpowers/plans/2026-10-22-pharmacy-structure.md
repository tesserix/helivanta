# PharmaConnect Structure Implementation Plan

**Goal:** Turn PharmaConnect into sectioned pharmacy management (Dispensing, Inventory, Purchasing, Setup) on the existing backend, with the dispense queue split on the server.

**Spec:** `docs/superpowers/specs/2026-10-22-pharmacy-structure-design.md` (#1258)

## Task 1: Dispense status filter (D4)

- `backend/internal/modules/pharmacy/dispenses.go`: `list` reads `status`:
  - an empty value returns every dispense, as today;
  - `pending` or `dispensed` adds `WHERE status = ?`;
  - anything else returns a 400.
- The handler must reject a bad value before any query. The 400 cannot go through `ListRoute`'s return value, so check how the other list handlers reject a bad filter (`catalogue.go`'s `?status=`) and do the same.
- `module_test.go`:
  - `?status=pending` returns only pending rows;
  - `?status=dispensed` returns only dispensed rows;
  - `?status=bogus` returns a 400.
- `contracts/api/clinical.openapi.json`: add the `status` query parameter (enum) on the list operation. Then run `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check`.

## Task 2: Sections and distinct icons (D1, D5)

- `packages/ui/src/zones.ts`: `ZonePage.section?: string`. Use PharmaConnect's pages per D2 (with their icons), and give distinct icons to Workforce, Emergency and the rail zones.
- `zones.test.ts` checks:
  - icons are unique among rail zones;
  - page icons are unique within each zone;
  - each zone's sections are contiguous.
- `hms-shell.tsx`: the page panel renders a section heading before the first page of each section. Test in `hms-shell.test.tsx`: on `/pharmacy`, the panel shows "Inventory" before "Stock on hand".

## Task 3: Pages (D2, D3, D6)

- `PharmacyWorkspace` views: `stock`, `receiving`, `safety`, `catalogue`, `accounts`, `setup`.
  - `StockPanel` loses `StockRegister` and `BatchSafety`; the `stock` view renders `StockRegister` and the `safety` view renders `BatchSafety`.
  - The registry setup and `AccessSetup` render only in `setup`.
- The Supply Connect card and the outlet caveat leave the pharmacy pages.
- New route pages: `/pharmacy/receiving`, `/pharmacy/safety` and `/pharmacy/setup`.
- `DispenseList`: a "Waiting" `QueryState` (`status=pending`) above a "Dispensed" one (`status=dispensed`). Tests cover both lists and that a dispense moves a row between them.
- Convert `components/pharmacy/*` to `Panel` and remove those files from `surface-legacy.mjs`.

## Verify

- Run `pnpm turbo lint type-check test build format:check`, `make lint-go` and the backend coverage gate.
- Run the e2e suite on a production build, with the smoke, journey and navigation specs unchanged where possible.
- Visually check every pharmacy page.
