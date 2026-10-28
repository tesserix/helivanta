# Counter sale slice C-3: the daily sales report — implementation plan

**Goal:** an outlet's sales, credit notes, tenders and refunds summed per outlet day (#1060).

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md`, "C-3 decisions".

**Architecture:** one read-only route sums the posted sale and credit-note tables in one
statement, grouped by the document's own outlet date and currency. No migration, no new
permission, no event.

## Global constraints

- `GET …/outlets/:outlet/reports/daily?from=&to=`, `pharmacy.sale.read`, outlet `reader`.
- A period is at most 366 outlet dates; the response is bounded by it, so it is not paged and is
  listed in `unpaginatedGETAllowlist`.
- Currencies are never added together: a row is one day and one currency.
- Cash counts the amount applied to the sale, never `tendered_minor`.

## Review focus

1. A credit note on a later day than its sale counts on its own day.
2. Change given in cash is not counted as income.
3. A day without activity has no row; an empty period returns an empty list, not an error.
4. Another tenant gets 404; the permission is declared on the route.
5. Two currencies on one day give two rows (unreachable today; see the ruling in Task 1).

## Task 1: the report route

**Files:** create `backend/internal/modules/pharmacy/daily_report.go` and
`daily_report_test.go`; register in `catalogue.go`; allowlist in `internal/archtest/arch_test.go`.

- Test `TestTheDailyReportSumsEachOutletDay`: sell cash (with change) and UPI on 2026-11-01,
  return part of the cash sale on 2026-11-02 (`SetClock`). Assert both rows' counts, totals, tax,
  units, tenders by kind and refunds, and that `cash_minor` excludes change.
- Test `TestTheDailyReportIsBoundedAndScoped`: empty period gives `[]`; bad periods 400; a reader
  with `pharmacy.sale.read` 200; another tenant 404. The route's permission is the router's.
- Ruling (2026-10-28): no two-currency test. A sale takes the tenant's country currency and
  the deployment is India-only, so two currencies at one outlet cannot be produced through the
  API, and the insert-only triggers refuse a hand-made row. The grouping key stays, as the rule
  "never add currencies" requires.
- Test `TestTheDailyReportCountsDiscounts`: a 10% discounted sale shows its discount.
- Mutation-check each assertion (sum `tendered_minor`, group credit notes by sale date, drop the
  discount, swap refund kinds, count lines as units, drop the net, a null list).

## Task 2: the page

**Files:** `packages/api/src/pharmacy-daily-report.ts`, `apps/shell/components/pharmacy/daily-report.tsx`
and its test, `apps/shell/app/(workspace)/pharmacy/daily-report/page.tsx`, the workspace view,
and the `packages/ui/src/zones.ts` entry (`pharmacy.sale.read`).

- A period form (as the H1 register) and a table per day with sales, credit notes, net, tenders
  and refunds; an empty period says no sales or returns were recorded.
- Tests: rows render with money formatted in their currency; empty state; failure with retry;
  invalid period shows inline errors and sends no request.

## Task 3: contracts and docs

Run `go run ./cmd/contractdocs` and `-check`; update the requirement map
(`docs/requirements/pharmaconnect-india.md`) where it lists #1060's daily summary.
