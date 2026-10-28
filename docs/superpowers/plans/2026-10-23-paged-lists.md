# Paged lists Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Counted, direction-aware keyset lists. Previous / Next paging, with the page state
kept in the URL. The Dispense queue on Waiting / Dispensed tabs.

**Architecture:**

- The server gains an opt-in counted list route. It reads the rows and the total from one
  scope inside one read-only `REPEATABLE READ` snapshot. Its keyset can run either
  direction.
- The browser fetches one page at a time and keeps the cursors it has visited for Previous.
  It holds the tab, page and cursor in the URL.

**Tech Stack:** Go (gin, GORM, Postgres), Next.js 16, TanStack Query, `@helivanta/ui`,
`@helivanta/api`, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-paged-lists-design.md` (#1295)

## Global Constraints

- `ListRoute`, its envelope and `GET /pharmacy/dispenses?status=` are unchanged.
- Newest-first cursors keep today's exact encoding. A cursor without a direction decodes as
  newest-first.
- `page.total` appears only on counted routes. It is an exact integer with no cap.
- Page size: 25, sent as `?limit=25`.
- The direction is fixed by the code that registers the route. A cursor for the other
  direction is a 400.
- Frontend rules from CLAUDE.md:
  - Use `useApiQuery`-family hooks and `next/link` for navigation, and tokens only.
  - Every new panel component gets a Vitest test using `renderWithProviders`.
- Gates:
  - `make lint-go`, the coverage gate with `GOTOOLCHAIN=go1.27.3`, and `go test -race ./...`.
  - `pnpm turbo lint type-check test build format:check`.
  - `go run ./cmd/contractdocs -check`.

## Review Focus

1. A row is inserted, or dispensed, between the count and the rows. The total must still
   match the rows from the same snapshot (Task 2 test).
2. Dispensing the last row on the last page. The page falls back rather than showing an
   empty page with "of N" (Task 6 test).
3. A reload or shared link mid-list. Previous must not guess (Task 5 test).
4. Two rows with the same `dispensed_at` or `created_at` either side of a page break. The id
   tiebreak in both directions means none is skipped or repeated (Task 1 test).
5. A tampered or old cursor. A direction mismatch is a 400 and a missing direction is
   newest-first. A cross-tenant cursor is still refused (Task 1 test).

---

### Task 1: Direction in the cursor and the keyset

**Files:**

- Modify: `backend/pkg/pagination/pagination.go`, `backend/pkg/tenantdb/keyset.go`
- Test: `backend/pkg/pagination/pagination_test.go`, `backend/pkg/tenantdb/keyset_test.go`

**Interfaces:**

- Produces:
  - `pagination.Direction` (`NewestFirst`, the zero value, and `OldestFirst`).
  - `Cursor.Direction`, and `ErrDirectionMismatch` (wrapped by `Parse` as a client error).
  - `pagination.ParseFor(c *gin.Context, tenantID string, d Direction) (Params, error)`,
    which refuses a cursor whose direction differs from `d`. `Parse` stays and equals
    `ParseFor(..., NewestFirst)`.
  - `tenantdb.Keyset{Column string; Direction pagination.Direction}` and
    `tenantdb.ApplyOrderedKeyset(tx *gorm.DB, p pagination.Params, k Keyset) *gorm.DB`.
    It quotes `Column` with `clause.Column`. `ApplyKeyset` stays and equals
    `Keyset{"created_at", NewestFirst}`.

- [ ] **Step 1: Write the failing tests**
  - `TestCursorEncodingUnchangedForNewestFirst`: a newest-first cursor encodes byte-for-byte
    as before, so the wire JSON has no `d` key.
  - `TestCursorWithoutDirectionDecodesNewestFirst`.
  - `TestParseForRefusesOtherDirection`: an oldest-first cursor with `NewestFirst` gives
    `errors.Is(err, ErrDirectionMismatch)`, and the reverse case too.
  - `TestOrderedKeysetPagesEveryRowOnceBothDirections`: 7 rows, 3 of them sharing one
    timestamp, paged 2 at a time. Each direction yields every id exactly once, in order.
- [ ] **Step 2:** Run `cd backend && go test ./pkg/pagination ./pkg/tenantdb`. Expect: FAIL,
      undefined `Direction`.
- [ ] **Step 3:** Implement. The oldest-first wire cursor adds `"d":"asc"`. The predicate is
      `(col, id) > (?, ?)` with `ORDER BY col ASC, id ASC` for oldest-first, and
      `<` / `DESC` for newest-first.
- [ ] **Step 4:** Run the same command again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pagination): cursor direction and ordered keyset (#1295)`.

### Task 2: One read-only snapshot

**Files:**

- Modify: `backend/pkg/tenantdb/db.go`
- Test: `backend/pkg/tenantdb/db_test.go`

**Interfaces:**

- Produces:
  `func (d *DB) WithTenantSnapshot(ctx context.Context, tenantID string, fn func(tx *gorm.DB) error) error`.
  It runs with `&sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}` and
  sets the same tenant and actor settings as `WithTenant`. It shares a private helper with
  `WithTenant` rather than copying it.

- [ ] **Step 1: Write the failing test**
      `TestSnapshotCountAndRowsAgreeAcrossConcurrentInsert`:
  - Inside `WithTenantSnapshot`, count a tenant table.
  - From a separate connection, insert and commit a row.
  - Read the rows again, and assert the row count equals the earlier count.
  - Also assert that the same sequence inside `WithTenant` (READ COMMITTED) disagrees. This
    proves the test can fail.
  - Also assert that a write inside the snapshot errors, showing it is read-only.
- [ ] **Step 2:** Run `go test ./pkg/tenantdb -run Snapshot`. Expect: FAIL, undefined method.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(tenantdb): read-only repeatable-read tenant snapshot (#1295)`.

### Task 3: CountedListRoute

**Files:**

- Modify: `backend/internal/platform/listroute.go`, `backend/internal/platform/router.go`
  (add `DeclaredRoute.Counted`), `backend/cmd/contractdocs/main.go` (add `counted` and a
  column in the table)
- Test: `backend/internal/platform/listroute_test.go`

**Interfaces:**

- Consumes: Task 1 `ParseFor`, `Keyset` and `ApplyOrderedKeyset`; Task 2 `WithTenantSnapshot`.
- Produces:

  ```go
  type Scope func(*gorm.DB) *gorm.DB
  type CountedListHandler func(c *gin.Context) (Scope, bool) // false: handler already responded
  func CountedListRoute[T Keyed](r *Router, db *tenantdb.DB, path string, perm authz.Permission, k tenantdb.Keyset, h CountedListHandler)
  ```
  - The platform runs `WithTenantSnapshot(principal.TenantID)`. It counts with
    `scope(tx.Model(new(T))).Count`, then fetches rows with
    `ApplyOrderedKeyset(scope(tx.Model(new(T))), p, k).Find`.
  - Envelope: `{"data":[…],"page":{"next_cursor":…,"has_more":…,"total":n}}`. `next_cursor`
    carries `k.Direction`.
  - The route is declared with `Paginated: true, Counted: true`.

- [ ] **Step 1: Write the failing tests**, against a test table:
  - `TestCountedListTotalMatchesRowsToTheEnd`: page to the end, and the sum of the rows
    equals `total` on every page, in both directions.
  - `TestCountedListHandlerAbortOpensNoTransaction`: the handler responds 400 and returns
    false. The response is that 400, and a counting scope spy is never called.
  - `TestCountedListRefusesOtherDirectionCursor`: 400.
  - `TestCountedListRefusesCrossTenantCursor`: 400.
  - `TestContractDocsRecordsCounted`, in the contractdocs tests (if present). Otherwise
    assert on the `DeclaredRoute` in `listroute_test`.
- [ ] **Step 2:** Run `go test ./internal/platform ./cmd/contractdocs`. Expect: FAIL, undefined.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(platform): counted list route (#1295)`.

### Task 4: Waiting and Dispensed routes

**Files:**

- Modify: `backend/internal/modules/pharmacy/dispenses.go`, `module.go` (routes and
  migration `0010_pharmacy`)
- Create: `backend/internal/modules/pharmacy/dispense_lists.sql`
- Test: `backend/internal/modules/pharmacy/dispenses_test.go` (existing file; create it if absent)

**Interfaces:**

- Consumes: Task 3 `CountedListRoute`.
- Produces:
  - `GET /pharmacy/dispenses/waiting`: `Keyset{"created_at", OldestFirst}`, scope
    `status = 'pending'`.
  - `GET /pharmacy/dispenses/dispensed`: `Keyset{"dispensed_at", NewestFirst}`, row type
    `dispensedRow` (it embeds `dispense`, and `PageKey` returns `*DispensedAt`), scope
    `status = 'dispensed'`.
  - Both use `PermDispenseRead` and `requirePharmacyTenant` as the existing list does.
  - Migration SQL:
    ```sql
    CREATE INDEX pharmacy_dispenses_waiting ON pharmacy_dispenses (tenant_id, created_at, id) WHERE status = 'pending';
    CREATE INDEX pharmacy_dispenses_dispensed ON pharmacy_dispenses (tenant_id, dispensed_at, id) WHERE status = 'dispensed';
    ```

- [ ] **Step 1: Write the failing tests**
  - `TestWaitingIsOldestFirstWithTotal`: 3 pending and 2 dispensed. Waiting returns the 3
    pending, oldest first, with `total: 3`.
  - `TestDispensedIsNewestByDispenseTime`: 2 rows dispensed in reverse order of creation
    come back by `dispensed_at`, newest first.
  - `TestStatusListUnchanged`: `?status=pending` still has no `total` and is newest first.
  - `TestDispenseListsOtherTenantInvisible`.
- [ ] **Step 2:** Run `go test ./internal/modules/pharmacy -run Dispens`. Expect: FAIL, 404 on
      the new routes.
- [ ] **Step 3:** Implement. Then run `go test ./internal/archtest`; if it fails, add the new
      routes to the matrix and allowlist the way the existing list is handled.
- [ ] **Step 4:** Run `go test ./internal/modules/pharmacy ./internal/archtest`. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): counted Waiting and Dispensed lists (#1295)`.

### Task 5: Page hook, Pager and PageTabs

**Files:**

- Create: `packages/api/src/page-query.ts`, `packages/api/src/page-query.test.tsx`,
  `packages/ui/src/pager.tsx`, `packages/ui/src/pager.test.tsx`,
  `packages/ui/src/page-tabs.tsx`, `packages/ui/src/page-tabs.test.tsx`
- Modify: `packages/api/src/paged.ts` (`Page<T>.page.total?: number`), and the two packages'
  `index.ts`

**Interfaces:**

- Produces:
  - Page query:
    ```ts
    useApiPageQuery<T>(key: unknown[], path: string, cursor: string | null,
      opts?: { poll?: boolean; limit?: number /* 25 */ })
      : { items: T[]; total: number | null; nextCursor: string | null; isPending; isError; error; retry }
    ```
    One `useApiQuery` per page. The key includes the cursor, and `placeholderData` keeps the
    previous rows visible while the next page loads.
  - `<Pager page={n} pageSize={25} shown={items.length} total={t} previousHref={string | null} nextHref={string | null} firstHref={string} />`
    - It renders "26–50 of 312" with `next/link` controls.
    - When `previousHref` is null and `page > 1`, it shows "First page" in place of
      Previous.
    - Next is disabled when `nextHref` is null.
  - `<PageTabs label tabs={{ key, label, href }[]} current={key} />`: links with
    `aria-current="page"` on the current tab.
- [ ] **Step 1: Write the failing tests**
  - Pager: the range text; Next disabled on the last page; "First page" shown with no
    Previous; nothing rendered when `total === 0`.
  - PageTabs: `aria-current` on the current tab, and each href is as given.
  - Page query: it requests `cursor` and `limit=25`, and reads `page.total`.
- [ ] **Step 2:** Run vitest for the three files. Expect: FAIL, missing modules.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run them again. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(ui): page query, Pager and PageTabs (#1295)`.

### Task 6: Dispense queue on tabs and pages

**Files:**

- Modify: `apps/shell/components/pharmacy/dispense-list.tsx`, `dispense-list.test.tsx`, and
  the dispense page in `apps/shell/app/(workspace)/pharmacy/page.tsx` (wrap the list in
  `<Suspense>` for `useSearchParams`)
- Test: `e2e/tests/pharmacy-paging.spec.ts` (new; add its account to the seed the way
  existing specs get theirs)

**Interfaces:**

- Consumes: Task 4 routes; Task 5 hook and components.
- URL: `?tab=waiting|dispensed&page=N&cursor=…`.
  - An unknown tab falls back to `waiting`.
  - A `ref` keyed by tab holds the cursor stack, indexed by page. It is not persisted, so
    after a reload Previous is unknown and Pager shows "First page".
  - When `items.length === 0 && page > 1`, `router.replace` goes to the previous known
    page, or else to the first page.

- [ ] **Step 1: Write the failing tests**
  - Vitest, in `dispense-list.test.tsx`:
    - It requests `/pharmacy/dispenses/waiting?limit=25` and shows "1–25 of 30".
    - Next's href carries the server's `next_cursor` and `page=2`.
    - On the dispensed tab it requests `/dispensed`.
    - An empty page 2 replaces the URL with page 1.
    - The existing dispense-form tests are kept.
  - e2e, `pharmacy-paging.spec.ts`:
    - Create 26 visits via `page.evaluate(fetch('/api/v1/medicore/visits', …))`.
    - Waiting shows "of" ≥ 26. Next, then Previous, returns the same first row.
    - A reload on page 2 keeps the rows and shows "First page".
    - The Dispensed tab URL holds `tab=dispensed`.
- [ ] **Step 2:** Run vitest for the file. Expect: FAIL.
- [ ] **Step 3:** Implement. Remove `StatusList`, the stack layout and `LoadMore`. Keep
      `DispenseForm` and the #1293 phone layout.
- [ ] **Step 4:** Run vitest, then the e2e spec on a production build. Expect: PASS.
- [ ] **Step 5:** Commit: `feat(pharmacy): dispense queue on Waiting / Dispensed tabs with pages (#1295)`.

### Task 7: Contract docs

**Files:**

- Modify: `docs/api/http-contract.md` (pagination section: `page.total` on counted lists,
  oldest-first routes, and that the cursor direction is fixed per route),
  `docs/api/domains/pharmacy.md` (the two routes)
- Regenerate: `docs/api/reference/*` via `cd backend && go run ./cmd/contractdocs`

- [ ] **Step 1:** Edit the docs. Then run `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`.
      Expect: ok, with the two routes showing `counted`.
- [ ] **Step 2:** Run the full gates from Global Constraints. Expect: all green.
- [ ] **Step 3:** Commit: `docs(api): counted and oldest-first lists (#1295)`.
