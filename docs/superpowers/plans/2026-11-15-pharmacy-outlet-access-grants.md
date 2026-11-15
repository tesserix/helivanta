# Pharmacy outlet access grants Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A person with `pharmacy.access.manage` sees who has access to an outlet, and grants, changes and revokes it from the outlet screen.

**Architecture:** A new keyset list route returns the outlet's `pharmacy_outlet_access` rows after the same pharmacy-scope check `changeAccess` runs. The shell's `OutletAccess` panel moves to its own file and gains the list, per-row level change and revoke, and a grant form whose choices are the signed-in subject plus `/iam/members`. The grant and revoke calls are the existing `PUT` and `DELETE` routes.

**Tech Stack:** Go (gin, gorm, Postgres), Next.js shell, `@helivanta/api`, `@helivanta/ui`, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-15-pharmacy-outlet-access-grants-design.md`

## Global Constraints

- Route: `GET /v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/access-grants`, permission `pharmacy.access.manage`, via `platform.ListRoute`.
- Row fields: `id`, `subject`, `level`, `created_at`, `updated_at`. Nothing else.
- No outlet grant is needed to list, as for `PUT`/`DELETE`. An outlet outside the path's pharmacy or tenant is 404. A database error is never an empty list.
- No migration and no change to the `PUT`/`DELETE` routes.
- Copy: "Nobody has access to this outlet yet.", "Access changed", "Access removed", "Pharmacy access granted", "Remove {subject}'s access to {outlet}?", "You will lose access to this outlet.", "Only a tenant administrator can add a new person here. You can still change or remove existing access."
- Forms use `useZodForm` and `noValidate`; selects are `FormSelectField`/`SelectControl`; revoke uses `ConfirmDialog`.

## Review Focus

- Removing your own grant: the confirmation warns, and afterwards the panel shows "You don't have access to this outlet yet." rather than a stale level. Covered in Task 2.
- A 403 from `/iam/members` must not hide the grant list or block self-grant. Covered in Task 2.
- A person already granted is not offered in the grant form, so a grant never silently changes an existing level. Covered in Task 2.
- A grant at another pharmacy's outlet must never be listed under this one. Covered in Task 1.
- A failed list read must show `RequestFailure`, never "Nobody has access". Covered in Task 2.

---

### Task 1: The grant list route

**Files:**

- Create: `backend/internal/modules/pharmacy/outlet_access_grants.go` (route, handler, row type), registered from `module.go`. `catalogue.go` stays unchanged: it is a pinned authority source in `docs/qa/staff-operational-access-reviews.json`, and changing it would invalidate that review.
- Test: `backend/internal/modules/pharmacy/outlet_access_grants_test.go`
- Modify (test harness only): `catalogue_test.go` `catalogueSetup`, adding a `"registrar"` token for `testutil.TenantB` with `pharmacy.access.manage` and `pharmacy.catalogue.read`

**Interfaces:**

- Produces: `type outletGrant struct { ID uuid.UUID; Subject, Level string; CreatedAt, UpdatedAt time.Time }` with JSON names `id, subject, level, created_at, updated_at`, `TableName() "pharmacy_outlet_access"`, and `PageKey()` returning `(CreatedAt, ID)`.
- Produces: `func (h *catalogueHandlers) listGrants(c *gin.Context, params pagination.Params) ([]outletGrant, error)`, registered by `outletAccessGrantRoutes(g, deps)` as `platform.ListRoute(…, "/access-grants", PermAccessManage, h.listGrants)` on the outlet group.

- [ ] **Step 1: Write the failing tests** in `outlet_access_grants_test.go`, using `catalogueHarness` and a helper `grantsPath(pharmacy, outlet string) string`:
  - `TestOutletGrantsListsEachSubject`: grant `user-a` editor, `user-b` reader, `user-c` editor through `PUT …/access/{subject}` as `"editor"`. The list as `"steward"` holds exactly those three with those levels.
  - `TestOutletGrantsReflectChanges`: change `user-b` to editor and revoke `user-c`. The list holds `user-a` and `user-b`, both editor.
  - `TestOutletGrantsStayInTheirScope`: as `"steward"`, the same outlet under another pharmacy ID is 404; as `"registrar"` (tenant B) the outlet is 404; as `"reader"` (no `pharmacy.access.manage`) it is 403.
  - `TestOutletGrantsPage`: three grants with `?limit=2`: page one has 2 and a cursor, page two has 1, and the three subjects appear once each.
- [ ] **Step 2: Run** `GOTOOLCHAIN=go1.27.4 go test ./internal/modules/pharmacy -run TestOutletGrants -count=1`. Expected: FAIL with 404, because there is no route.
- [ ] **Step 3: Implement.** In `listGrants`, use `catalogueScope`, then `h.db.WithTenant`: count the outlet with `tenant_id=? AND pharmacy_id=? AND id=?` as `changeAccess` does (not 1 → `gorm.ErrRecordNotFound`), then `tenantdb.ApplyKeyset(tx, params).Select("id,subject,level,created_at,updated_at").Where("tenant_id=? AND outlet_id=?", …).Find(&rows)`. Errors go to `catalogueError`.
- [ ] **Step 4: Run** the Step 2 command, then `go test ./internal/modules/pharmacy -run 'TestCatalogue|TestOutlet' -count=1`. Expected: PASS.
- [ ] **Step 5: Mutation, reverted.** Drop `pharmacy_id=?` from the outlet check: `TestOutletGrantsStayInTheirScope` fails on the other-pharmacy case.
- [ ] **Step 6: Commit** `feat(pharmacy): list an outlet's access grants (#1718)`.

### Task 2: The outlet access panel

**Files:**

- Create: `apps/shell/components/pharmacy/outlet-access.tsx` (moved `OutletAccess` and `accessSchema` out of `outlets-setup.tsx`), `outlet-access.test.tsx`
- Modify: `apps/shell/components/pharmacy/outlets-setup.tsx` (import it), `e2e/tests/pharmacy-outlets-pane.spec.ts`

**Interfaces:**

- Consumes: Task 1's route and row shape.
- Produces: `export function OutletAccess(props: { scope: PharmacyScope; subject: string; outletName: string; canManage: boolean; onStateChange?: FormState })`. `canManage` replaces `canGrant`; the caller passes `can("pharmacy.access.manage")`.
- Query keys: `pharmacyKey(scope, "access")` (own level, unchanged) and `pharmacyKey(scope, "access-grants")`. Every mutation invalidates both.
- The grant form schema: `{ subject: string (min 1), level: "reader" | "editor" (default "editor"), confirmed: boolean refined true }`. The checkbox label becomes "Confirm this is the outlet to grant access at", and its error stays "Confirm the outlet before granting access."

- [ ] **Step 1: Write the failing tests** in `outlet-access.test.tsx` with `renderWithProviders` and a mocked fetch that records `(method, url, body)`:
  - without `canManage`: only "Your access here: Editor" shows, and no request goes to `/access-grants` or `/iam/members`;
  - with it: rows for `user-owner` (the signed-in subject, marked "You") and `user-ph1`, each with its level;
  - an empty list shows "Nobody has access to this outlet yet."; a 500 shows the `RequestFailure` and not that text;
  - grant: choose `user-new` (from `/iam/members`) and Reader, tick the checkbox, submit → `PUT …/access/user-new` with `{"level":"reader"}`, then both lists are fetched again; `user-ph1` is not among the person options;
  - change: choose Editor on `user-ph1`'s row → `PUT …/access/user-ph1` with `{"level":"editor"}`, toast "Access changed";
  - revoke: "Remove" on `user-ph1` → the dialog text "Remove user-ph1's access to {outlet}?"; Cancel sends nothing; Confirm sends `DELETE …/access/user-ph1`, toast "Access removed";
  - revoking your own row adds "You will lose access to this outlet." to the dialog;
  - `/iam/members` 403: the administrator message shows, the grant list still renders, and the signed-in subject is still an option.
- [ ] **Step 2: Run** `pnpm --filter shell test -- outlet-access` (Node and env from memory). Expected: FAIL, because the module does not exist.
- [ ] **Step 3: Implement.** Move the component. Load members with `useApiPagedQuery<Member>(["iam","members","pharmacy-grant", scope.tenantId, subject], "/iam/members", …)` and `LoadMore`, as `supply/organisation-access.tsx` does. Load grants with `useApiPagedQuery` on `${pharmacyRoot(scope)}/access-grants`. Person options are the signed-in subject plus member subjects, minus subjects already granted, without duplicates. The 403 test is `members.error instanceof ApiError && members.error.status === 403`.
- [ ] **Step 4: Update the e2e spec.** The fixture already granted the signed-in account, so instead of re-granting, assert the pane's grant list shows the signed-in row with "You" and "Editor".
- [ ] **Step 5: Run** `pnpm turbo lint type-check test --filter=shell --filter=e2e`. Expected: PASS.
- [ ] **Step 6: Mutations, each reverted.** Keep already-granted subjects among the options: the grant test fails. Skip the confirmation and send `DELETE` at once: the cancel test fails.
- [ ] **Step 7: Commit** `feat(shell): manage outlet access for other people from the outlet screen (#1718)`.

### Task 3: Contracts and docs

**Files:**

- Modify: `docs/api/domains/pharmacy.md` (registry and grants row), `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md` (F-2, F-8, SET-07, SET-08, SET-10), generated `docs/api/reference/*`, `docs/qa/staff-operational-access-inventory.json`

- [ ] **Step 1:** Registry and grants row: add the `access-grants` list, under `pharmacy.access.manage`, needing no outlet grant.
- [ ] **Step 2:** Acceptance plan:
  - F-8: Done (#1718); verify with SET-08 and SET-10.
  - F-2: #1581 merged; verify with SET-19 to SET-23 once deployed.
  - SET-07: the panel's grant form with the signed-in account and Editor; the unticked case still shows "Confirm the outlet before granting access."
  - SET-08: grant OWN1, OWN2, PH1, PH2 and PHD as Editor and RDR as Reader from the panel as ADM; the list shows each; each is audited. Grant NOG Editor at B from B's panel.
  - SET-10: revoke RDR from the panel and confirm; RDR's catalogue read is 404.
  - Add SET-10a: as OWN1 (not a tenant administrator) the panel shows the administrator message, and OWN1 can still change PH2's level.
- [ ] **Step 3: Run** from `backend/`: `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`, then the staff access inventory generator and its `--check`. Expected: the reference shows the new GET, and both checks pass.
- [ ] **Step 4: Commit** `docs(pharmacy): outlet access is managed from the outlet screen (#1718)`, including the spec and this plan.

Final gates: `make lint-go`, `./scripts/coverage-gate.sh`, `go test -race ./internal/modules/pharmacy`, `pnpm turbo lint type-check test build format:check`, then remove the build output.
