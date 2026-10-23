# PharmaConnect overview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A PharmaConnect landing page of exact, permission-gated counts, each opening its list
already filtered.

**Architecture:**

- The server gains two list filters (`?due=` on payables, an outlet-wide `recorded=30d` safety
  list) and one counts route. Every count is the same SQL predicate as the list it links to.
- The shell moves the Dispense queue to `/pharmacy/dispense`, adds the overview at `/pharmacy`,
  and teaches four target pages to read their filter from the URL.

**Tech Stack:** Go (gin, GORM, Postgres), Next.js 16, `@helivanta/ui`, `@helivanta/api`,
Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-pharmacy-overview-design.md` (#576)

## Global Constraints

- Every overview count shares its predicate with the list its link opens: one Go function per
  filter, used by both.
- The outlet's local date comes from `outletLocalDate` (tax_classifications.go), never
  `CURRENT_DATE`.
- Outlet-scoped reads run inside `h.transaction(..., p, outlet, false, ...)`, so a missing grant
  is 404.
- A section the principal lacks the permission for is absent from the JSON, never zero.
- Frontend: `useApiQuery`, `next/link`, tokens only, and a Vitest test per panel with
  `renderWithProviders`.
- Gates:
  - `make lint-go`, the coverage gate with `GOTOOLCHAIN=go1.27.3`, and `go test -race` on the
    changed packages.
  - `pnpm turbo lint type-check test build format:check`.
  - `go run ./cmd/contractdocs -check`.

## Review Focus

1. Boundary dates in the outlet's time zone (UTC is a different day from Asia/Kolkata near
   midnight): expiry at exactly 30 days, a bill due exactly today, a concern 30 days ago.
2. A fully paid or over-credited bill must count as settled in both the tile and the list.
3. A permission held for one section but not another: the absent section must not appear, and
   must not 403 the whole route.
4. Old shared queue links (`/pharmacy?tab=…`) must still open the queue.
5. A failed section, or the waiting count failing, must show a failure, never a zero.

---

### Task 1: `?due=` on the payables list

**Files:**

- Modify: `backend/internal/modules/pharmacy/payables.go`
- Test: `backend/internal/modules/pharmacy/payables_test.go`

**Interfaces:**

- Produces: `payableDueScope(due string, today time.Time) (func(*gorm.DB) *gorm.DB, error)`
  - `""` → no filter.
  - `overdue` → `outstanding_minor > 0 AND due_on < today`.
  - `week` → `outstanding_minor > 0 AND due_on BETWEEN today AND today+7`.
  - Anything else → error, answered as 400.
  - A repeated `due` → 400.

- [ ] **Step 1: Write the failing test** `TestPayablesDueFilter` (one outlet in Asia/Kolkata):
  - Bills due yesterday, today, today+7 and today+8, plus a fully paid one due yesterday.
  - `due=overdue` → only the unpaid bill due yesterday.
  - `due=week` → today and today+7.
  - No `due` → all five.
  - `due=soon` → 400.
- [ ] **Step 2:** Run `go test ./internal/modules/pharmacy -run PayablesDue`. Expect: FAIL.
- [ ] **Step 3:** Implement. Today is `outletLocalDate`, read inside the same transaction.
- [ ] **Step 4:** Run it again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): filter supplier bills by due (#576)`.

### Task 2: Outlet-wide recent safety concerns

**Files:**

- Modify: `backend/internal/modules/pharmacy/batch_safety.go`
- Test: `backend/internal/modules/pharmacy/batch_safety_test.go`

**Interfaces:**

- Produces: `recentSafetyScope(today time.Time) func(*gorm.DB) *gorm.DB`, which keeps rows
  whose `created_at` falls on or after the outlet-local date `today − 29`, compared in the
  outlet's time zone.
- `GET …/safety-incidents` with no `batch_id`:
  - With `recorded=30d`, it lists the outlet's recent concerns, newest first.
  - Without `recorded`, it is still 400, as today.
  - With `batch_id`, behaviour is unchanged.

- [ ] **Step 1: Write the failing test** `TestRecentSafetyConcernsOutletWide`:
  - Concerns created 0, 29 and 30 days ago (outlet-local) on two batches.
  - `recorded=30d` returns the 0- and 29-day concerns.
  - No `batch_id` and no `recorded` → 400.
  - `recorded=7d` → 400.
- [ ] **Step 2:** Run it. Expect: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): outlet-wide recent safety concerns (#576)`.

### Task 3: Overview route

**Files:**

- Create: `backend/internal/modules/pharmacy/overview.go`,
  `backend/internal/modules/pharmacy/overview_test.go`
- Modify: `catalogue.go` (route registration), `internal/archtest/arch_test.go` (unpaginated
  GET allowlist)
- Regenerate: `docs/api/reference`

**Interfaces:**

- Consumes: Task 1 `payableDueScope`, Task 2 `recentSafetyScope`, the register's expiry
  predicate (extracted as `stockExpiryScope(expiry string)`, so the list and the overview share
  it), and catalogue `tax=unclassified` (extracted as `unclassifiedScope(today)`).
- Produces: `GET /v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/overview`, permission
  `PermCatalogueRead`. Response:

  ```json
  {"as_of":"…RFC3339 in outlet tz…","time_zone":"Asia/Kolkata",
   "stock":{"expired":n,"due_30":n,"due_90":n},
   "safety":{"recent":n},
   "bills":{"overdue":n,"week":n},
   "setup":{"unclassified":n,"registration_in_force":bool}}
  ```
  - `stock` and `safety` appear only with `pharmacy.stock.read`.
  - `bills` appears only with `pharmacy.accounts.read`.
  - `setup.registration_in_force` appears only with `pharmacy.tax.manage`, and only when the
    country has a scheme.
  - Everything is computed in one transaction.

- [ ] **Step 1: Write the failing tests**
  - `TestOverviewCountsEqualTheirLists`: seed each kind. For every count, page the linked list
    with the same filter to the end and assert the totals are equal.
  - `TestOverviewSectionsFollowPermissions`: a catalogue-only token gets only
    `setup.unclassified`.
  - `TestOverviewOtherTenantAndNoGrant`: 404.
- [ ] **Step 2:** Run them. Expect: FAIL (404 on the route).
- [ ] **Step 3:** Implement. Extract the shared scopes and refactor the list handlers onto them.
      Then run `go run ./cmd/contractdocs`, and allowlist the route in `arch_test.go`.
- [ ] **Step 4:** Run `go test ./internal/modules/pharmacy ./internal/archtest ./cmd/contractdocs`.
      Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): overview counts route (#576)`.

### Task 4: Routes and navigation

**Files:**

- Move: `apps/shell/app/(workspace)/pharmacy/page.tsx` → `apps/shell/app/(workspace)/pharmacy/dispense/page.tsx`
- Create: the new `apps/shell/app/(workspace)/pharmacy/page.tsx`, which redirects when queue
  parameters are present (D1), sends a viewer without catalogue read on to `/pharmacy/dispense`,
  and otherwise renders the overview (Task 5).
- Modify:
  - `packages/ui/src/zones.ts`: add an "Overview" page first, href `/pharmacy`, permission
    `pharmacy.catalogue.read`, no section. Dispense queue href becomes `/pharmacy/dispense`.
  - `packages/ui/src/zones.test.ts` (PharmaConnect structure).
  - `e2e/tests/support/queue.ts`, and specs that `goto("/pharmacy")` for the queue.
  - `apps/shell/components/pharmacy/dispense-list.tsx`: any hardcoded `/pharmacy` hrefs; it
    uses `usePathname`.

- [ ] **Step 1: Write the failing tests**
  - `zones.test`: the PharmaConnect pages start with Overview, then the Dispense queue at
    `/pharmacy/dispense`. A doctor (dispense read only) opens at `/pharmacy/dispense`.
  - e2e `pharmacy-overview.spec.ts`: `/pharmacy?tab=waiting&q=x` lands on
    `/pharmacy/dispense?tab=waiting&q=x`.
- [ ] **Step 2:** Run them. Expect: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `vitest` for ui, and dispense-list in the shell. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): overview at /pharmacy, dispense queue at /pharmacy/dispense (#576)`.

### Task 5: Overview page

**Files:**

- Create: `apps/shell/components/pharmacy/overview.tsx`,
  `apps/shell/components/pharmacy/overview.test.tsx`
- Modify: `pharmacy-workspace.tsx`. Add `view="overview"` so the picker and outlet selection are
  shared; its outlet body renders `<OverviewTiles>`.

**Interfaces:**

- Consumes: Task 3's JSON and `/pharmacy/dispenses/waiting?limit=1` (via `useApiPageQuery`;
  `total` and `items[0].created_at`).
- Tiles in spec order, each a `Panel` with a heading, the count(s) and `next/link`s.
- Absent section → no tile.
- Failed request → a `RequestFailure` in that tile only.
- "as of hh:mm" on the outlet tiles, from `as_of` and `time_zone`.

- [ ] **Step 1: Write the failing tests**
  - Tiles render in order with the server numbers.
  - The link hrefs carry the filters (`/pharmacy/stock?balance=on_hand&expiry=due_30`,
    `/pharmacy/accounts?due=overdue`, `/pharmacy/safety?recorded=30d`,
    `/pharmacy/catalogue?tax=unclassified`, `/pharmacy/dispense?tab=waiting`).
  - A missing `bills` section → no Supplier bills tile.
  - A 503 on the overview → the outlet tiles show "Could not load", and no "0" appears.
  - The waiting tile shows "oldest waiting since …" from the first row.
  - No dispense read permission → no waiting tile.
- [ ] **Step 2:** Run them. Expect: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run them again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): overview tiles (#576)`.

### Task 6: Target pages read their filter from the URL

**Files:**

- Modify: the stock register component (reads `expiry` and `balance`), the accounts panel
  (reads `due`, sends `?due=`, shows the filter, offers to clear it), the catalogue panel
  (reads `tax=unclassified`), and the batch safety page (reads `recorded=30d` and shows the
  outlet-wide list above the per-batch view). Add their tests.
- Each page wraps its client component in `<Suspense>` where it adds `useSearchParams`.

- [ ] **Step 1: Write the failing tests.** For each component, the URL filter is sent to the
      server and shown as selected.
- [ ] **Step 2:** Run them. Expect: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run them again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): lists open filtered from the overview (#576)`.

### Task 7: e2e, docs, gates

- [ ] **Step 1:** `e2e/tests/pharmacy-overview.spec.ts`, set up through the API in the page, as
      `pharmacy-tax.spec` does:
  - Create a pharmacy and outlet with a GSTIN-less setup, one unclassified product, and a bill
    due in the past.
  - Assert the tile numbers.
  - Follow "Overdue" and assert the bill list shows exactly that many rows.
  - Follow "Not classified" and assert the same.
  - The waiting count equals the queue's total.
- [ ] **Step 2:** Docs:
  - `docs/api/domains/pharmacy.md`: the overview route, `?due=`, and `recorded=30d`.
  - #576 noted with what is out of this slice.
  - Run `go run ./cmd/contractdocs` and `-check`.
- [ ] **Step 3:** Run all gates from Global Constraints and the full e2e. Expect: green.
- [ ] **Step 4:** Commit: `docs(pharmacy): overview contract (#576)`.
