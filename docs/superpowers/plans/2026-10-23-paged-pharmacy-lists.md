# Paged pharmacy lists Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Supplier bills, the Product catalogue and Expiry & safety onto 25-row pages with
Previous / Next, a total, tabs and the URL as state.

**Architecture:**

- The platform's counted list gains a guarded form, so outlet-scoped pharmacy reads can run
  their access check inside the snapshot that counts and fetches.
- Five counted routes reuse the existing scope helpers.
- A shell hook carries the URL paging that the Dispense queue does inline today, and the
  three pages and the queue use it.

**Tech Stack:** Go (gin, GORM, Postgres RLS), Next.js 16, TanStack Query via `@helivanta/api`,
Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-paged-pharmacy-lists-design.md`. It builds on
`2026-10-23-paged-lists-design.md` (slice 1) and `2026-10-23-pharmacy-overview-design.md`
(slice 2).

## Global Constraints

- **Paging:**
  - 25 rows a page (`PAGE_SIZE`), newest first by `created_at`.
  - Previous / Next with the total via `Pager`, tabs via `PageTabs`. No new paging component.
- **The URL is the state.** `?tab=`, the filters, `?page=` and `?cursor=`:
  - changing tab or filter resets the page to 1 and keeps the other filters;
  - after a reload, "First page" replaces Previous;
  - a page that empties falls back a page;
  - no rows are carried over while a page loads.
- **Routes:**
  - New counted routes; the uncounted v1 routes are unchanged.
  - An unknown or repeated filter is a 400.
  - Without an outlet grant, or for a foreign outlet, the answer is 404.
  - If access cannot be checked, the answer is 503, never 500, and no count is read.
- **One scope:** count, rows and overview use the same scope helper.
- **Outlet date and time zone** are read once per request, inside the snapshot.
- **Frontend:**
  - `useApiQuery`-family hooks only; `next/link` tabs.
  - No hardcoded colours; `SelectControl`, not a native `<select>`.
  - Every changed panel has a Vitest test using `renderWithProviders`.
- **Git:** no AI attribution in commits or the PR.
- **Gates:**
  - `pnpm turbo lint type-check test build format:check`, `make lint-shell`, `make lint-go`.
  - `cd backend && GOTOOLCHAIN=go1.27.3 ./scripts/coverage-gate.sh`, `go test -race`.
  - `go run ./cmd/contractdocs`, then `-check`.
  - Run them one at a time.

## Review Focus

1. **A guard that fails after it has set session settings.** The response must be the
   handler's 404 or 503, not a 500, with no partial body. Pinned in Task 1 by
   `TestGuardedListGuardErrorUsesResponder`.
2. **The outlet's date read once.** Count and rows must use one `today`. A second read near
   midnight would split them. Pinned in Task 2: the guard returns the scope with `today` bound.
3. **A cursor from one filter used under another**, for example an Overdue cursor on All
   bills. The rows still come from the cursor; the range label must not lie. The frontend
   keeps visited cursors per list key (tab plus filters), as the queue does. Pinned in Task 4
   by "changing tab resets page".
4. **A catalogue with no tax scheme and `?tax=unclassified` in the URL.** The filter must be
   ignored, not applied invisibly. Pinned in Task 5.
5. **A batch from another outlet in `?batch=`.** The answer is 404, and the page shows "batch
   not found" rather than a blank list. Pinned in Task 3 (server) and Task 6 (page).

---

### Task 1: Guarded counted list in the platform

**Files:**

- Modify: `backend/internal/platform/listroute.go`
- Test: `backend/internal/platform/counted_listroute_test.go`

**Interfaces:**

- Produces:
  ```go
  // CountedQuery is a counted list that needs work inside its snapshot.
  type CountedQuery[T any] struct {
      // Guard runs first, inside the snapshot, and returns the list's scope.
      // An error goes to Fail; nothing is counted.
      Guard func(tx *gorm.DB) (Scope, error)
      // Rows, if set, applies to the page fetch only (a Select or projection
      // that would break Count).
      Rows Scope
      // After, if set, runs on the fetched page inside the snapshot.
      After func(tx *gorm.DB, rows []T) error
      // Fail answers a Guard or After error.
      Fail func(c *gin.Context, err error)
  }
  type GuardedListHandler[T any] func(c *gin.Context) (CountedQuery[T], bool)
  func GuardedCountedListRoute[T Keyed](r *Router, db *tenantdb.DB, path string, perm authz.Permission, k tenantdb.Keyset, h GuardedListHandler[T])
  ```
- `CountedListRoute` keeps its signature. It becomes a thin adapter: a Guard that returns
  its scope, with `Fail` unused.

- [ ] **Step 1: Write the failing tests** in `counted_listroute_test.go`, reusing
      `countedHarness`:
  - `TestGuardedListGuardErrorUsesResponder`:
    - The Guard returns `gorm.ErrRecordNotFound`, and `Fail` answers `respond.NotFound`.
    - Expect 404.
    - Expect that a Scope whose body sets a flag never ran.
  - `TestGuardedListGuardSeesTheSnapshot`:
    - The Guard inserts nothing. It runs a `SELECT 1` so the snapshot is taken, then
      commits one "keep" row in a separate `db.WithTenant` transaction.
    - Expect `total` to be 5, not 6.
    - Prove the test can fail by running it once with the Guard placed outside the snapshot.
  - `TestGuardedListRowsProjectionOnlyOnFetch`:
    - Set `Rows: tx.Select("id, tenant_id, 'projected' AS kind, created_at")`.
    - Expect `total` to be 5 and every row's `kind` to be `"projected"`.
  - `TestGuardedListAfterSeesThePage`:
    - Expect After to receive exactly the page's rows: length 2 at `?limit=2`.
    - An After error goes to `Fail`.
- [ ] **Step 2: Run** `cd backend && go test ./internal/platform -run 'GuardedList' -count=1`.
      Expected: FAIL (undefined: `platform.GuardedCountedListRoute`).
- [ ] **Step 3: Implement** `GuardedCountedListRoute` in `listroute.go`:
  - Move the body of `CountedListRoute` into it.
  - Inside `WithTenantSnapshot`, call Guard, then count, offset count and fetch, then After.
  - Hold a guard error in a local so it goes to `Fail`, while database errors from
    count and fetch stay `respond.InternalErr`.
  - `declare` records `Counted: true` as before.
  - Rewrite `CountedListRoute` as an adapter over it.
- [ ] **Step 4: Run** `go test ./internal/platform -count=1` and
      `go test ./internal/modules/pharmacy -run 'Dispense' -count=1`.
      Expected: PASS, the existing counted and dispense tests unchanged.
- [ ] **Step 5: Commit** `feat(platform): counted lists can guard their snapshot (#576)`

### Task 2: Pharmacy read guard; counted bills and inventory audit

**Files:**

- Modify:
  - `backend/internal/modules/pharmacy/catalogue.go` (the `transaction` preamble)
  - `backend/internal/modules/pharmacy/payables.go`
  - `backend/internal/modules/pharmacy/inventory_audit.go`
- Test: `backend/internal/modules/pharmacy/counted_lists_test.go` (new)

**Interfaces:**

- Consumes: Task 1's `GuardedCountedListRoute`, `CountedQuery`.
- Produces:

  ```go
  // outletAccess sets the pharmacy session settings and checks the caller's
  // outlet grant. lock takes FOR SHARE (writes); a read-only snapshot cannot.
  func outletAccess(tx *gorm.DB, p authn.Principal, outlet, pharmacy uuid.UUID, lock bool) (level string, err error)
  // outletReadGuard is the read preamble for a counted outlet list: access,
  // then fn builds the scope (fn may read the outlet date).
  func outletReadGuard(c *gin.Context, p authn.Principal, outlet uuid.UUID, fn func(tx *gorm.DB) (platform.Scope, error)) func(tx *gorm.DB) (platform.Scope, error)
  ```
  - `transaction` calls `outletAccess(..., true)`. Its errors are unchanged:
    `errPharmacyAccessUnavailable` gives 503, and no grant gives `gorm.ErrRecordNotFound`.
  - Routes:
    - `GET …/accounts/payables/counted`, which takes `due`.
    - `GET …/accounts/inventory-audit/counted`.
  - Gin 1.12 matches the static segment before `:id`.

- [ ] **Step 1: Write the failing tests.** The harness is modelled on `overviewHarness`, with
      the editor, ungranted and foreign tokens:
  - `TestCountedPayablesTotalsMatchPaging`:
    - Seed 3 overdue, 2 due this week and 1 settled bill.
    - Page with `limit=2` to the end, for no filter, `due=overdue` and `due=week`.
    - Expect each total to equal the rows reached, 6/3/2.
    - Expect each to equal the overview's `bills` counts.
  - `TestCountedInventoryAuditTotal`: the total equals the rows reached by paging.
  - `TestCountedPharmacyListsRefuseWithoutAccess`, for both routes:
    - ungranted gives 404;
    - foreign outlet gives 404;
    - `due=soon` gives 400;
    - `due=overdue&due=week` gives 400.
  - `TestCountedPharmacyListsUnavailableIs503`: make the access check fail, the same way the
    existing 503 test in `catalogue_test.go` does, and expect 503.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -run 'Counted' -count=1`. Expected:
      FAIL with 404 from the router (route not registered).
- [ ] **Step 3: Implement.**
  - Extract `outletAccess` from `transaction`. Locked and unlocked differ only by the
    `FOR SHARE OF a,o` clause.
  - Payables handler:
    - Validate `due` with `validPayableDue`, as `listPayables` does.
    - In the guard, read `outletLocalDate` once.
    - The scope is `Table("pharmacy_payable_balances")`, the tenant and outlet where-clause,
      and `payableDueScope` when `due` is set.
    - `Rows` selects `payableColumns`.
    - `Fail` is `payableError`.
  - Audit handler: scope over the `pharmacy_inventory_audit` view; `Rows` is its select list.
  - Register both with `tenantdb.Keyset{Column: "created_at"}`.
- [ ] **Step 4: Run** `go test ./internal/modules/pharmacy -count=1`. Expected: PASS, the whole
      package.
- [ ] **Step 5: Commit** `feat(pharmacy): counted supplier bills and inventory audit (#576)`

### Task 3: Counted safety concerns and catalogue

**Files:**

- Modify:
  - `backend/internal/modules/pharmacy/batch_safety.go`
  - `backend/internal/modules/pharmacy/inventory.go` (routes)
  - `backend/internal/modules/pharmacy/catalogue.go`
- Test: `backend/internal/modules/pharmacy/counted_lists_test.go`

**Interfaces:**

- Consumes: Task 2's `outletReadGuard`.
- Produces:
  - `GET …/stock/safety-incidents/recent`, with no query filters. The scope is
    `recentSafetyScope(today, tz)`, with the outlet's date and time zone read once in the
    guard.
  - `GET …/stock/batches/:id/safety-incidents`. The guard runs `stockReference` on the
    batch, which is 404 when it is not the outlet's.
  - `GET …/catalogue/products/counted`:
    - `q`, `status` and `tax` are validated exactly as `listProducts` does (the prefix
      match).
    - `Rows` selects `productColumns`.
    - `After` is `attachTax`.
    - `Fail` is `catalogueError`.
    - With `tax=unclassified` on an outlet whose country has no tax scheme, the answer is 400. The page never sends it (spec D3).

- [ ] **Step 1: Write the failing tests:**
  - `TestCountedRecentSafetyTotal`: the total equals the rows reached by paging, and equals
    the overview's `safety.recent`.
  - `TestCountedBatchSafety`:
    - The batch's concerns only.
    - A sibling outlet's batch gives 404.
    - A malformed id gives 400.
  - `TestCountedCatalogueFilters`:
    - The total for no filter, `status=active`, `q=<prefix>` and `tax=unclassified` each
      equals the rows reached.
    - Expect the `unclassified` total to equal the overview's `setup.unclassified`.
    - Every row carries `tax`.
  - Extend `TestCountedPharmacyListsRefuseWithoutAccess` with the three new routes.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -run 'Counted' -count=1`. Expected:
      FAIL on the new routes with 404.
- [ ] **Step 3: Implement** the three handlers on `outletReadGuard`.
- [ ] **Step 4: Run** `go test ./internal/modules/pharmacy -count=1` and
      `go test ./internal/archtest -count=1`. Expected: PASS. The counted routes are paginated,
      so archtest needs no allowlist.
- [ ] **Step 5: Run** `go run ./cmd/contractdocs`, then `go run ./cmd/contractdocs -check`.
      Add the five routes to `docs/api/domains/pharmacy.md`. Expected: check clean.
- [ ] **Step 6: Commit** `feat(pharmacy): counted safety concerns and catalogue (#576)`

### Task 4: URL paging hook; Supplier bills on tabs

**Files:**

- Create: `apps/shell/lib/use-url-page.ts`, `apps/shell/lib/use-url-page.test.tsx`
- Modify:
  - `apps/shell/components/pharmacy/dispense-list.tsx` (use the hook; behaviour unchanged)
  - `apps/shell/components/pharmacy/accounts-panel.tsx` and its test
  - `apps/shell/components/pharmacy/pharmacy-workspace.tsx` (drop `initialDue`) and its test
  - `apps/shell/components/pharmacy/overview-tiles.tsx` and its test (bill links become
    `?tab=overdue` and `?tab=week`)
  - `packages/api/src/page-query.ts` (pass `request` through to `useApiQuery`)

**Interfaces:**

- Produces:
  ```ts
  // The page, cursor and links of a URL-paged list (#1295 D5). `list` is
  // every URL value that defines the list (tab and filters): visited cursors
  // are kept per list, and every link preserves them.
  export function useUrlPage(list: Record<string, string>): {
    page: number;
    cursor: string | null;
    first: string; // page 1 of this list
    previousHref: string | null; // null after a reload: Pager offers First page
    nextHref: (next: string | null) => string | null;
    hrefFor: (list: Record<string, string>) => string; // another list, page 1
  };
  // Step back a page when an open later page empties (#1295 D6).
  export function useStepBackWhenEmpty(
    query: { isPending: boolean; isError: boolean; items: unknown[] },
    url: ReturnType<typeof useUrlPage>,
  ): boolean; // true while stepping back
  ```
  - Empty values are left out of the URL.
  - `useApiPageQuery(key, path, cursor, { poll?, limit?, request? })`.
- Bills tabs: `overdue | week | all | audit`. The default is `all`; any other value, an old
  `?due=` link included, reads as `all`. Tabs map to `due`, except that `audit` lists the
  audit route.

- [ ] **Step 1: Write the failing tests:**
  - `use-url-page.test.tsx`, a hook test through a small component with a mocked
    `next/navigation`:
    - page 2 with a cursor gives `previousHref` to page 1;
    - after a reload on page 2, `previousHref` is null;
    - `hrefFor` drops the page and cursor;
    - empty values are left out.
  - `accounts-panel.test.tsx`:
    - `?tab=overdue` requests `/accounts/payables/counted?due=overdue&limit=25` and shows
      "1–3 of 3";
    - the audit tab requests `/accounts/inventory-audit/counted`;
    - the tab links keep no page;
    - the "Show all bills" button is gone.
  - `overview-tiles.test.tsx`: the bill links are `/pharmacy/accounts?tab=overdue` and
    `?tab=week`.
- [ ] **Step 2: Run** `pnpm --filter shell test -- use-url-page accounts-panel overview-tiles`.
      Expected: FAIL.
- [ ] **Step 3: Implement:**
  - The hook: lift the visited-cursor logic from `DispenseList`, then refactor
    `DispenseList` onto it.
  - `AccountsPanel`:
    - `PageTabs`, `useApiPageQuery`, `Pager`.
    - The bills table and audit table markup unchanged.
    - The audit table renders only on its tab.
  - Remove `initialDue` from the workspace.
- [ ] **Step 4: Run** `pnpm --filter shell test` and `pnpm --filter @helivanta/api test`.
      Expected: PASS, including the unchanged `dispense-list.test.tsx`.
- [ ] **Step 5: Commit** `feat(pharmacy): supplier bills on tabs and pages (#576)`

### Task 5: Catalogue on tabs and pages

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/catalogue-panel.tsx`
  - `apps/shell/components/pharmacy/tax-classification.test.tsx` or a new
    `catalogue-panel.test.tsx`
  - `pharmacy-workspace.tsx` (drop `initialUnclassified`)
  - `overview-tiles.tsx` and its test (if the catalogue link changes; it stays
    `?tax=unclassified`)

**Interfaces:**

- Consumes: Task 4's `useUrlPage`.
- Tabs `all | active | discontinued | withdrawn` map to `status`. URL values `q` and `tax`.
- Without a tax scheme, `tax` is dropped from the list key and the request.

- [ ] **Step 1: Write the failing tests:**
  - `?tab=active&q=par` requests `/catalogue/products/counted?status=active&q=par&limit=25`;
  - submitting a search keeps the tab and drops the page;
  - pressing "Not classified" sets `tax=unclassified` and keeps `q`;
  - without a tax scheme, `?tax=unclassified` is not sent and no toggle is shown;
  - "1–25 of N" is shown.
- [ ] **Step 2: Run** `pnpm --filter shell test -- catalogue tax-classification`. Expected:
      FAIL.
- [ ] **Step 3: Implement:**
  - The search form submits through `router.replace(url.hrefFor({...}))`, keyed by `q` as
    `WaitingSearch` is.
  - The product rows' markup is unchanged.
  - `LoadMore` is removed from the product list only. `ProductHistory` keeps it.
- [ ] **Step 4: Run** `pnpm --filter shell test`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): catalogue on tabs and pages (#576)`

### Task 6: Expiry & safety on tabs and pages

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/batch-safety.tsx` and `batch-safety.test.tsx`
  - `pharmacy-workspace.tsx` (drop `showRecent`)
  - `overview-tiles.tsx` and its test (the safety link becomes `/pharmacy/safety?tab=recent`)

**Interfaces:**

- Consumes: `useUrlPage`, and `useApiPageQuery` with `request: requestFor(scope)`.
- Tabs `recent | batch`. The default is `recent`, and an old `?recorded=30d` lands there.
- `batch` reads `?batch=<id>`. Choosing a batch in `SafetyBatchPicker` sets it with
  `router.replace(url.hrefFor({ tab: "batch", batch }))`.
- The concern form and the picker's option lists are unchanged; they stay on "Load more".

- [ ] **Step 1: Write the failing tests:**
  - with no tab, the page requests `/stock/safety-incidents/recent?limit=25` and shows
    "1–N of N";
  - `?tab=batch&batch=<id>` requests `/stock/batches/<id>/safety-incidents?limit=25` with
    the picker preselected;
  - a 404 for the batch shows "This batch is not at this outlet" and no list;
  - the tile link is `?tab=recent`.
- [ ] **Step 2: Run** `pnpm --filter shell test -- batch-safety overview-tiles`. Expected:
      FAIL.
- [ ] **Step 3: Implement.** `RecentConcerns` is always rendered on its tab.
      `SelectedBatchSafety` takes the batch from the URL, not from state.
- [ ] **Step 4: Run** `pnpm --filter shell test`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): expiry and safety on tabs and pages (#576)`

### Task 7: e2e, docs and gates

**Files:**

- Modify:
  - `e2e/tests/pharmacy-overview.spec.ts` (the links land on tabs; each total equals the
    tile's count)
  - `e2e/tests/pharmacy-paging.spec.ts`: the catalogue gets more than 25 products, Next then
    Previous returns to the same first row, a reload on page 2 keeps page 2, and a search
    survives paging.
  - Any other spec that reads these pages: `grep -rln "pharmacy/(accounts|catalogue|safety)" e2e/tests`
- Modify:
  - `docs/superpowers/specs/2026-10-23-pharmacy-overview-design.md`: D4's link list now
    points at the tabs; add a pointer to this spec.
  - `docs/superpowers/specs/2026-10-23-paged-lists-design.md` "Not covered": these three
    pages moved in #576.

- [ ] **Step 1:** Update the specs, then rebuild the shell and restart the API.
- [ ] **Step 2: Run** `npx playwright test --project=specs --reporter=line --workers=2` from
      `e2e/`. Expected: all pass.
- [ ] **Step 3: Run the gates** from the Global Constraints, one at a time. Expected: all
      green.
- [ ] **Step 4: Commit** `test(pharmacy): paged lists end to end (#576)`
