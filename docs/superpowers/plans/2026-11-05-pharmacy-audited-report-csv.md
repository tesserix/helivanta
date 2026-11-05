# Audited CSV downloads of the personal pharmacy reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Download patient-wise sales, sales by a prescriber, the sales worklist and the daily report, with personal files audited in the read's transaction.

**Architecture:** `reportFile[T]` gains a `read` that runs a callback on the file's rows inside the read's transaction, before any audit row; the export handler's size check and encoding move into that callback, so an audit row exists only for a file that was built. `salesReport.read` takes the audit action and the callback; the worklist read is split the same way. The daily report gets its own small `read`.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16; Next.js, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-05-pharmacy-audited-report-csv-design.md`

## Global Constraints

- No migration.
- Audit actions: `report_export` (patient sales, item sales by prescriber), `list_export` (worklist with an admission sale); field names only in `audit_events`.
- No audit row for a refused file; a failed audit write is a 500 with no file.
- Slice 1's bounds, format and parameter allowlist.

## Review Focus

- An admission sale beyond the first page of a worklist file is still audited (Task 1).
- A file refused at the bound writes no audit row (Task 1).
- The audit fault seam fails each personal file with no attachment (Task 1).
- A prescriber file without prescription read is 403 (Task 1).
- The JSON pages still audit as `report_read`/`list_read` (existing tests).

---

### Task 1: API

**Files:** `report_csv.go`, `sales_reports.go`, `sales_worklist.go`, `daily_report.go`, `arch_test.go`; test `audited_report_csv_test.go`.

- [ ] Tests:
  - `TestPatientSalesCSV`: rows match the page; one `report_export` row with field names `from,to,patient_id`; fault → 500, no attachment; bound → 413 and no row;
  - `TestPrescriberSalesCSV`: prescriber file rows; `report_export` row; 403 for the owner;
  - `TestWorklistCSV`: rows match pages; walk-in-only file writes no row; admission sale beyond the first 50 rows is audited (`list_export`); `invoice` 400;
  - `TestDailyReportCSV`: rows and amounts match JSON, no audit row.
- [ ] Implement; mutation-check the in-transaction ordering, the all-rows admission check and the export action.

### Task 2: Screens

**Files:** `sales-reports.tsx`, `sales-worklist.tsx`, `daily-report.tsx`, `packages/api/src/pharmacy-sales-reports.ts`; tests beside them.

- [ ] `pharmacyReportCSVPath` maps the daily, patient-sales and worklist paths; the filename check accepts their names.
- [ ] Tests: each button requests its path with its filters; the prescriber button is back; the recorded-download note shows where a file is audited.

### Task 3: Docs

- [ ] `pharmacy.md`: routes, the audited-reads table rows, not-covered lists; regenerate `contractdocs` and the staff-access inventory.
