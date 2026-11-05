# CSV downloads of three pharmacy sales reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Download sales by item, receipts and refunds, and sales and stock as bounded, exact CSV files.

**Architecture:** `salesReportPage` is split so that a CSV handler can run the same query in one transaction and get its rows and filter back. One generic export handler per report validates the allowed parameters, fetches limit+1 rows, refuses overflow, encodes with `csvsafe` behind a BOM, writes the structured audit log and sends the file. Amounts are formatted by the currency's minor digits from the generated country profile. The shell reuses `RegisterExport`.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16; Node generator; Next.js, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-05-pharmacy-report-csv-design.md`

## Global Constraints

- No migration.
- At most 5,000 rows and 8 MiB; more is `413 report_export_too_large` with no file.
- Only each report's own parameters; others, repeats and empties are 400. The prescriber filter is refused.
- Amounts: exact major units from `currency_minor_digits`; an undeclared currency is 500 and no file.
- `pharmacy.report.exported` in the structured audit stream after a complete file only; no `audit_events` row.

## Review Focus

- A negative amount (net of a period with more credits than sales) is written `-12.50`, not `-11.50` or `12.-50` (Task 2).
- A brand name beginning `=` or `@` is apostrophe-prefixed (Task 2).
- The 5,001st row is refused, not dropped (Task 2, via the row-limit seam).
- A failed read writes no audit log line and no file (Task 2).
- A filter change while downloading cancels the download (Task 3).

---

### Task 1: Currency minor digits in the country profile

**Files:** `scripts/generate-country-profiles.mjs`, `backend/pkg/countryprofile/profiles_gen.go` (generated), `backend/pkg/countryprofile/profiles_test.go`.

- [ ] Test `TestCurrencyMinorDigits`: INR is 2; an undeclared currency is false.
- [ ] Generate `CurrencyMinorDigits(code string) (int, bool)`; the generator refuses two countries sharing a currency with different digits. Run the generator and `--check`.

### Task 2: API

**Files:** `backend/internal/modules/pharmacy/sales_reports.go`, new `report_csv.go`, `daily_report.go`, `backend/internal/archtest/arch_test.go`; test `report_csv_test.go`.

- [ ] Tests:
  - `TestSalesByItemCSV`: bytes match the JSON page (BOM, header, period, amounts in rupees), the filters apply, headers and filename, audit log line with row count and digest;
  - `TestReceiptsCSV` and `TestStockSalesCSV`: rows and columns match JSON;
  - `TestReportCSVRefuses`: unknown, repeated, empty, `cursor` and prescriber parameters are 400; permissions 403; foreign outlet 404; the row seam set to 1 gives 413 with no audit line; a formula-like brand is prefixed; a header-only file for an empty period.
  - `TestMajorUnits` (unit): 0, 5, -5, 123450, -1250, digits 0 and 3, unknown currency.
- [ ] Implement; mutation-check the overflow check, the parameter allowlist and the negative-amount formatting.

### Task 3: Screens

**Files:** `apps/shell/components/pharmacy/h1-register.tsx` (`RegisterExport` gains the noun), `sales-reports.tsx`, `packages/api/src/pharmacy-sales-reports.ts`; test `sales-reports.test.tsx`.

- [ ] Tests: each report's button requests its `.csv` path with the applied filters; a prescriber filter shows the note and no button; a 413 shows its message.
- [ ] Implement `pharmacyReportCSVPath(path)` and `fetchPharmacyReportCSV(scope, path, signal)`; key the export by its path.

### Task 4: Docs and generated contracts

- [ ] `docs/api/domains/pharmacy.md`: the three routes, limits, format and audit; remove CSV export from those reports' not-covered lists.
- [ ] `contractdocs` and the staff-access inventory regenerated and checked.
