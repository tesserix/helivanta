# Interim bills Implementation Plan

**Goal:** Issue numbered, frozen interim bills on an open admission account (#197 slice 3).

**Spec:** docs/superpowers/specs/2026-10-30-interim-bills-design.md

## Global Constraints

- Migration `0003_billing`, append-only; forced RLS on both new tables.
- Permission `billing.bill.issue`, receptionist only.
- `409` codes: `account_closed`, `nothing_new`.

## Review Focus

- An entry committed concurrently with an issue appears on the next bill (ids, not time cut).
- A line inserted later into an old bill is refused.
- Two issues at once number 1 then 2, never a duplicate or a gap.
- A closed account refuses issue.
- A reader without the grant gets 404 on every route.

### Task 1: Schema and backend

Files: `billing/interim_bills.sql`, `billing/interim_bills.go`, `module.go`,
`interim_bills_test.go`, archtest matrix and unpaginated allowlist.

- Tables `billing_interim_bills (tenant, hospital, admission, number, id, issued_by,
issued_at default now(), unpriced_bed_days)`, `billing_interim_bill_lines (tenant,
hospital, admission, number, id = entry id, new)`; unique `(tenant, admission, id)` on
  entries for the line FK.
- Triggers: contiguous number on an open account; line only in the issuing transaction;
  append-only.
- Tests: `TestInterimBillsFreezeTheAccountAndNumberInOrder` (frozen lines/totals, new flag,
  nothing_new, closed account, 404 without grant, 403 without permission),
  `TestInterimBillTablesRefuseLateLinesAndOtherWriters`.

### Task 2: Frontend

`packages/api/src/billing.ts` types; `apps/shell/components/medicore/interim-bills.tsx`
with a Vitest test; Bills toggle in `ward-board.tsx`.

### Task 3: Docs

`docs/api/domains/billing.md` section; regenerate contract docs.
