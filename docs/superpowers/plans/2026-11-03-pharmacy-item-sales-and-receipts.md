# Pharmacy sales by item and receipts register — implementation plan

**Goal:** two paged reads for #1060: sales by item, and every receipt and refund.

**Spec:** `docs/superpowers/specs/2026-11-03-pharmacy-item-sales-and-receipts-design.md`

## Global constraints

- Reads only: no migration, no new permission (`PermSaleRead`).
- Period through `outletPeriod`; paging through `platform.ListRoute` and
  `tenantdb.ApplyOrderedKeyset` oldest first.
- Money in minor units; amounts in one row never mix currencies.

## Task 1: backend (`backend/internal/modules/pharmacy/sales_reports.go`)

- `itemSalesRow` (keyed by product `created_at`, `id`), `itemSalesRows` SQL:
  per product, sale-line sums by `sale_date` and credit-note-line sums by
  `credit_note_date`, joined to the product, optional product filter.
- `receiptRow` (keyed by document `created_at`, tender/refund `id`),
  `receiptRows` SQL: `UNION ALL` of tenders and refunds, optional kind filter.
- Routes in `dailyReportRoutes`: `/reports/items`, `/reports/receipts`.
- Tests (`sales_reports_test.go`), written first:
  - items: a sale of two products with a discount and a service charge, a
    partial return on a later day; rows, units, amounts, MRP range; items plus
    service charges equal the daily report's sales and credits; the product
    filter; a return dated in the period of a sale outside it lists the product
    with zero sold; 400 on a bad period or product id; another outlet's sale
    absent.
  - receipts: cash + UPI tenders and a refund; order, kinds, document numbers,
    amounts per day and kind equal the daily report; kind filter; 400 on a bad
    kind; paging across two pages.
- Archtest/contract docs regenerated.

## Task 2: shell

- `packages/api/src/pharmacy-sales-reports.ts`: types and paths; export.
- `apps/shell/components/pharmacy/sales-reports.tsx`: `ItemSales` and
  `ReceiptsRegister` panels (period form, `useApiPagedQuery`, `LoadMore`,
  empty/error/loading states), shown on the daily report page.
- Tests with `renderWithProviders`: rows render; empty state; failure state;
  admission-account rows marked "not collected".

## Task 3: docs

Domain doc section, the daily report's "not covered" line corrected.

## Review focus

- A credit note dated inside the period for a sale outside it.
- Two pages of receipts whose documents share `created_at`.
- A product with sales in two MRPs.
