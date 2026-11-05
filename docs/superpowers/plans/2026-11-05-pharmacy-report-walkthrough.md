# Pharmacy report walkthrough and screen states Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the Daily sales and Sales screens in a real browser against the real API, and fix the refusal and empty-state defects that this proof finds.

**Architecture:**
- **Setup.** A Playwright spec builds a fresh outlet through owner commands, then posts sales through the counter screen. A registered patient comes from a hospital whose affiliation is verified.
- **Checks.** It checks each report and each download against what was posted.
- **Fixes.** `RequestFailure` learns to show a refusal without a retry. The report sections pass it their error. Empty sentences become announced statuses.

**Tech Stack:** Next.js, Vitest, Playwright; Go API unchanged.

**Spec:** `docs/superpowers/specs/2026-11-05-pharmacy-report-walkthrough-design.md`

## Global Constraints

- **No backend, migration or contract change.** If the walkthrough finds an API defect, fixing it is a ruling, and the spec gains it.
- **Fixtures use owner commands only,** never table or projection writes. Names and values carry a `TEST` prefix.
- **A refusal is an `ApiError` with status 401, 403 or 404.** It shows no "Try again". Any other error keeps the message and "Try again".
- **Every new assertion is mutation-checked:** it must fail on the defect it guards.

## Review Focus

- A section whose request fails leaves its sibling sections' rows in place (Task 3).
- After a period change, no row from the previous period stays on screen, in either the failure or the loading state (Task 3).
- A refusal mid-session clears rows rather than keeping the last good answer (Task 3).
- A download's rows equal the screen's rows for the same filter, not just its row-count header (Task 3).
- `RequestFailure` without `error` behaves exactly as before at its 176 call sites (Task 1).

---

### Task 1: Refusal state and announced empties

**Files:**
- `packages/ui/src/request-failure.tsx` and its test;
- `apps/shell/components/pharmacy/{sales-reports,daily-report,sales-worklist}.tsx` and their tests.

**Interfaces:**
- Produces: `RequestFailure({ message, retry, error?: unknown, refused?: string })`.
  - If `error` is an `ApiError` with status 401, 403 or 404 and `refused` is set, it renders `<Panel role="alert">{refused}</Panel>`, with no button.
  - Otherwise it renders as today.
- Also produces: each report section's empty paragraph carries `role="status"`.

Steps:

- [ ] **Failing tests** in `request-failure.test.tsx`:
  - a 403 `ApiError` with `refused` shows the sentence and no "Try again" button;
  - a 404 does the same;
  - a 500, a `TypeError`, and a 403 without `refused` each show the message and "Try again".
- [ ] **Failing tests** in the three pharmacy test files:
  - for each section (daily totals, items, receipts, patient lines, stock sales, worklist), a mocked 403 shows "You don't have access to this outlet's reports." and no "Try again";
  - each empty result is found by `getByRole("status")` with its sentence.
- [ ] **Implement.** The `ApiError` check uses `instanceof` from `@helivanta/api`, which `@helivanta/ui` already depends on (`query-state.tsx` imports it).
- [ ] **Paged sections.** `useApiPagedQuery` exposes `error`, so pass `error={items.error}`. For the others, pass `error={report.error}`.
- [ ] **Mutation-check:** drop the 404 from the refusal set; drop `role="status"` on one empty paragraph.
- [ ] **Run** `pnpm turbo lint type-check test --filter=@helivanta/ui --filter=shell`. Commit.

### Task 2: Shared outlet fixture and an affiliated patient

**Files:**
- `e2e/tests/support/pharmacy-counter.ts`;
- new `e2e/tests/support/pharmacy-reports.ts`.

**Interfaces:**
- Produces `readyOutlet(admin: Page, browser: Browser)`.
  - It is `readyCounter`'s body without the payment account. It covers the licence, tax registration, invoice profile, product with tax and schedule, quarantine and shelf, batch, receipt and release.
  - It returns `{ context, cashier, outlet, product, sku, batch, quarantine, shelf, subject, stamp }`.
  - `readyCounter` calls it, then adds the payment account.
- Produces `affiliatedPatient(admin, cashier, fixture)`, returning `{ patientId, mrn, hospitalId, name }`. Its steps:
  1. A hospital, with registrar access for the admin.
  2. A patient registered there.
  3. The pharmacy's affiliation to it, and a verification request.
  4. The hospital's acceptance, polled until the pharmacy shows the link `verified`.
  5. A lookup grant for the cashier's subject.
  6. A poll until the pharmacy has the registered patient.

  Routes are in `docs/api/domains/pharmacy.md` (Hospital affiliation verification) and `patients-and-clinical.md` (#1416, #1416b).
  - **Who accepts.** The requester can't accept their own request, and only tenant administrators hold `patient.pharmacy-affiliation.review`. So the cashier temporarily gets `tenant_admin` through `POST /iam/members` to accept, then has it removed. This follows `readyCounter`'s temporary-role pattern.
  - **If IAM refuses `tenant_admin` for a member,** stop and rule: the patient-wise steps of Task 3 then use the API-tested coverage, and the spec gains the departure.

Steps:

- [ ] **Extract `readyOutlet`.** Run `pharmacy-payment-return.spec.ts`. Expected: it passes unchanged.
- [ ] **Write `affiliatedPatient`.** Run it alone from a scratch spec. Expected: the lookup by MRN returns the patient. Delete the scratch spec. Commit.

### Task 3: The walkthrough

**Files:** new `e2e/tests/pharmacy-reports.spec.ts`. After it is added, re-run `make seed`, so the spec's per-file accounts exist.

Steps:

- [ ] **One test, "a pharmacist's sales read back the same on every report and file".**
  - **Through the counter screen, the cashier:**
    1. sells 3 units for cash to a walk-in buyer;
    2. sells 2 units for cash to the affiliated patient, chosen through the screen's hospital and MRN lookup;
    3. returns 1 unit of the walk-in sale for cash, from `/pharmacy/returns`.
  - **Expected amounts come from the posted sale and credit-note responses:**
    - total and net;
    - taxable amount and tax;
    - units sold and returned.
  - **On `/pharmacy/daily-report`, for today:**
    - **Daily sales row:** "2 for", the total, "1 for" the credit, net, and taken and refunded as cash.
    - **Sales by item:** units 5 sold and 1 returned, and the amount. The lot filter keeps the row; a category filter with no products shows the empty status.
    - **Receipts and refunds:** two receipts and one refund, with their amounts.
    - **Patient-wise:** after the MRN lookup, one line of 2 units.
    - **Sales and stock:** 5 sold. Stock on hand is 20 − 5 + returned-to-quarantine, as `readyOutlet` set it up.
  - **On `/pharmacy/sales`:**
    - two rows newest first;
    - "Walk-in" narrows to one;
    - looking up the invoice opens its statement with the credit note;
    - the download button is hidden for an invoice lookup.
  - **Downloads.** Each one is taken through `page.waitForEvent("download")`. For each, check:
    - the filename `pharmacy-<name>-<outlet>-<from>-<to>.csv`;
    - parsed rows equal the screen's rows: same count, same invoice, unit and amount values;
    - amounts in rupees with two decimals.
- [ ] **Test "report states".**
  - **Empty:** the first and last day of a past month show each section's empty status.
  - **Invalid filters:**
    - To before From shows "The last day is on or after the first";
    - a 400-day period shows "A period is at most 366 days";
    - in both cases, `page.on("request")` records no report request.
  - **Failed load:**
    1. Load today with rows.
    2. Route `**/reports/receipts**` to `abort()`, then change the period.
    3. Expected: the receipts section shows its alert with "Try again". The other sections show rows for the new period, and no previous-period invoice is visible anywhere.
    4. Unroute and press "Try again". Expected: rows return.
  - **Restricted access:**
    - the admin, who holds the owner role and no prescription read, sees no "Prescriber" combobox;
    - a stocker-less view has no "Sales and stock" heading. Use a role without `pharmacy.stock.read`; if none of the seeded roles fits, rule it and assert it in the component test instead;
    - with the page open, the admin removes the cashier's outlet grant (`DELETE …/outlets/:outlet/access/:subject`). Then the cashier changes the period. Expected: every section shows the refusal sentence, no "Try again" anywhere, and no invoice number from before.
- [ ] **Watch it fail before Task 1's fixes.** Check out Task 1's files from `main` and run. Expected: the refusal and empty-status assertions fail. Restore the files, re-run and expect a pass. Record both runs in the ledger. Commit.
- [ ] **Fix anything else found,** with its failing test first. Each one is a ledger ruling and goes in the PR body.

### Task 4: Docs

- [ ] **`pharmacy.md`.** Add one table, "Legacy pharmacy reports (#1060)". It has one row per legacy report number (449 … 444), giving the built report and route, or the reason it isn't built: formulas, wholesale, indents, ledger heads.
- [ ] **Screen states.** Add a line on the report screens' states: refusal, failure, empty and loading.
- [ ] **Superseded "Not covered" lines.** Update any that the table replaces.
- [ ] **Run the gates:**
  - `pnpm turbo lint type-check test build format:check --concurrency=2`;
  - `make lint-shell`;
  - the new spec and `pharmacy-payment-return.spec.ts` against the local stack.
