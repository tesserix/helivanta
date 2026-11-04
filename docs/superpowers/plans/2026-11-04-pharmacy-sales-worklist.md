# Pharmacy sales worklist and sale summary — implementation plan

**Goal:** #987 slice 1: a filtered, paged sales worklist and a per-sale
statement whose amounts reconcile.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-sales-worklist-design.md`

## Global constraints

- Reads only: no migration, no new permission (`PermSaleRead`).
- No buyer name, phone or patient id in either response.
- Period through `outletPeriod`; paging through `platform.ListRoute` and
  `tenantdb.ApplyOrderedKeyset`, newest first.

## Task 1: backend (`backend/internal/modules/pharmacy/sales_worklist.go`)

- `saleWorkRow` (keyed by sale `created_at`, `id`) and `saleWorkRows` SQL:
  sales with `sale_date` in the period, or the one `invoice_number`; the
  customer kind from `admission_id` / `patient_id`; credits, admission billed
  and reversed from credit notes, tenders and refunds.
- `saleSummary`, read in one transaction: lines with returned units and
  credited amounts, charge lines with credited amounts, tenders by kind,
  credit notes with refunds by kind, balances.
- Routes: `/sales/worklist` (list) and `/sales/:id/summary`, before
  `/sales/:id`.
- Tests, written first (`sales_worklist_test.go`):
  - worklist: a walk-in cash sale, a registered-patient sale with a partial
    return, an admission sale with a partial return; kinds, bed code,
    credits, net, admission balance; newest first; `customer` filter; exact
    `invoice` without a period; period bounds; no `buyer_name`,
    `buyer_phone` or `patient_id` key in the body; 400 for a bad customer or
    period; another outlet's sale absent; paging.
  - summary: the balance identities for each of the three sales, line and
    charge credits, credit notes and refunds; no personal keys; 404 for an
    unknown id and another tenant.
- Contract docs and staff access inventory regenerated.

## Task 2: shell

- `packages/api/src/pharmacy-sales-worklist.ts`: types and paths.
- `apps/shell/components/pharmacy/sales-worklist.tsx`: filters, the list,
  the summary opening beside it with `useListDetail`.
- Route `apps/shell/app/(workspace)/pharmacy/sales/page.tsx`, a workspace
  view, and a "Sales" entry in `packages/ui/src/zones.ts`.
- Tests with `renderWithProviders`: rows and kinds; summary balances; empty
  and failure states; filter request paths.

## Task 3: docs

Domain doc section; spec "not covered" recorded in the PR.

## Review focus

- A sale with service charges and a credit note crediting a charge.
- An admission sale fully reversed (nothing left on the account).
- Two sales sharing `created_at` across a page boundary.
