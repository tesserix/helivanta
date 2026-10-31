# Daily collection report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a hospital-day cash collection report, JSON and CSV, and its panel on the Billing page.

**Architecture:** one billing handler file reads shifts opened in the hospital-local day with per-purpose receipt sums in one query; the CSV encodes the same struct. The shell adds a panel gated by the new permission.

**Tech Stack:** Go (gin, gorm, Postgres), Next.js, zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-30-daily-collection-design.md`

## Global Constraints

- Permission `billing.collection.read`, no roles; matrix `{RoleTenantAdmin}`; admin-only exception list.
- Day bounds `(?::date)::timestamp AT TIME ZONE tz` to `((?::date)+1)::timestamp AT TIME ZONE tz`.
- `409 timezone_unset`; `400` bad/future date; `413 collection_too_large` above 500 shifts.
- Open shift: money fields `null`. Totals over closed and pending only.
- CSV: rupees `1234.50`, `csvsafe.Text` cells, filename `billing-collection-<hospital>-<date>.csv`, audit `billing.collection.exported`.

## Review Focus

- A shift opened 23:30 IST on the 29th is on the 29th, not the 30th (UTC date).
- An open shift never reveals expected or totals, including in the CSV.
- Voided receipts are excluded from purpose totals but counted as voided.
- A refund subtracts from net cash.
- Ungranted hospital is 404; clerk is 403.

---

### Task 1: Backend report and CSV

**Files:** Create `backend/internal/modules/billing/collections.go`, `collections_test.go`; modify `module.go` (permission, routes), `backend/internal/archtest/{arch_test.go,matrix_test.go}`, `docs/api/domains/billing.md`, generated contract docs.

- [ ] Test `TestTheDailyCollectionTotalsEachShiftByPurpose`: payment, deposit, refund, a void, a short close authorised, a second open shift; assert per-shift figures, totals, `final:false` for today, open shift money null, CSV rows and TOTAL row.
- [ ] Test `TestTheCollectionDayIsTheHospitalsLocalDay`: shifts opened at 23:30 and 00:30 IST (set `opened_at` as owner); each lands on its local date; past day all closed is `final:true`.
- [ ] Test refusals: no timezone 409, future 400, bad date 400, clerk 403, outsider 404.
- [ ] Implement; run `go test ./internal/modules/billing/ ./internal/archtest/`; regenerate contract docs.

### Task 2: Frontend panel

**Files:** modify `packages/api/src/billing.ts` (types, `fetchDailyCollectionCSV`); create `apps/shell/components/medicore/daily-collection.tsx` and test; modify `billing-desk.tsx`.

- [ ] Test: renders shifts and totals for the chosen date; open shift shows "Not counted"; not-final notice; Download CSV fetches `export.csv`; panel hidden without the permission.
- [ ] Implement; run frontend gates.
