# Pharmacy patient-wise sales and sales stock — implementation plan

**Goal:** two paged reads for #1060: one registered patient's purchases
(audited), and each product's sales beside its stock.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-patient-sales-and-sales-stock-design.md`

## Global constraints

- Reads only: no migration, no new permission.
- Period through `outletPeriod`; paging through `platform.ListRoute` and
  `tenantdb.ApplyOrderedKeyset` oldest first.
- Patient-wise reads write their audit row in the read's transaction.

## Task 1: backend (`backend/internal/modules/pharmacy/sales_reports.go`)

- `patientSaleLine` (keyed by sale `created_at`, line `id`) and
  `patientSaleLines` SQL: lines of sales with `patient_id` = the patient,
  `sale_date` in the period; product, batch, admission bed code, and units
  returned on credit notes dated up to `to`.
- `stockSalesRow` (keyed by product `created_at`, `id`) and `stockSalesRows`
  SQL: products with any movement or a sale line in the period; sold and
  returned by document date; on hand and sellable from all movements.
- Routes: `/reports/patient-sales` (`PermSaleRead`), `/reports/stock-sales`
  (`PermStockRead`), in `dailyReportRoutes`.
- `patient_id` is required; a missing or malformed id is 400.
- Tests, written first (`sales_reports_test.go`):
  - patient-wise: two patients and a walk-in; only the asked patient's
    lines, with batch, expiry, MRP, discount, amount; a return counted on
    the line, and not when credited after `to`; the period; an audit row
    per page carrying the patient id; 400 without or with a bad
    `patient_id`; another outlet's sale absent.
  - stock-sales: units sold and returned equal sales by item for the
    period; on hand equals the stock balance; a product with stock and no
    sale listed with zero sold; quarantined stock counts on hand, not
    sellable; another outlet absent; a user with sale read but no stock
    read is refused.
- Contract docs and staff access inventory regenerated.

## Task 2: shell

- `packages/api/src/pharmacy-sales-reports.ts`: `PharmacyPatientSaleLine`,
  `PharmacyStockSales`, and their paths.
- `apps/shell/components/pharmacy/sales-reports.tsx`: `PatientSales`
  (patient picked with `CounterPatientLookup`, nothing fetched until one is
  chosen) and `StockSales`, both under the daily report and sharing its
  period.
- Tests with `renderWithProviders`: rows render; no request before a patient
  is chosen; empty and failure states.

## Task 3: docs

Domain doc section; the earlier section's "not covered" line corrected.

## Review focus

- A credit note dated after the period for a line inside it.
- A patient whose sale was to an admission.
- A product received into quarantine only (on hand, none sellable).
