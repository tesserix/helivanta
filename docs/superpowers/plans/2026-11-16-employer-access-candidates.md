# Employer access candidates, slice 1 (pharmacy) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A manager with `pharmacy.access.manage` sees the pharmacy's own linked staff by name in the outlet access panel and grants one of them access, without tenant administration.

**Architecture:** Workforce implements a new `authz.EmployerAccessCandidates` interface. It reads active staff with an active, current-version login link under a new purpose-bound RLS admission (migration `0122_workforce_access_candidates`) and audits every returned row in the same transaction. `platform.EmployerAccessCandidates` finds the provider the way `platform.StaffActors` does, and `platform.Deps` carries it. Pharmacy adds `GET /v1/pharmacy/pharmacies/{pharmacy}/access-candidates`, and the shell panel uses it instead of `/iam/members`.

**Tech Stack:** Go (gin, gorm, Postgres RLS), Next.js shell, `@helivanta/api`, `@helivanta/ui`, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-16-employer-access-candidates-design.md` (#1719)

## Gates before Task 1

- **Sam123ben's reply on #1719** ([comment](https://github.com/tesserix/helivanta/issues/1719#issuecomment-6902717554)). Task 1 changes the RLS that #1669 rebuilt. Until the reply arrives, Task 1 follows this plan's defaults:
  - a separate session setting `app.workforce_access_candidates`, not an extended operating-selection proof;
  - no edit to `staff_policy_plans.sql` or `staff_generic_plans.sql`.

  If the reply changes either default, amend Task 1 before starting.
- **The user approves the ledger re-record.** Registering the migration adds one entry to `Migrations()` in `workforce/module.go`. Every review in `docs/qa/staff-operational-access-reviews.json` that pins that file has to be re-recorded, and no ledger edit happens without explicit approval.
- **Migration ID re-check.** `0122_workforce_*` must still be free on main and on `codex/*` branches at merge time. It was free on 2026-11-16.

## Global Constraints

- **Interface**, in `backend/pkg/authz/employer_access_candidates.go`, exactly as the spec defines it: `EmployerAccessCandidates`, `AccessCandidateRequest{TenantID, Actor, Kind, EmployerID, Purpose, Page}`, `AccessCandidate{Subject, DisplayName, CreatedAt, StaffID}`.
  - `AccessCandidate.PageKey()` returns `(CreatedAt, StaffID)`.
  - `StaffID` is never serialised.
- **Constants:**
  - kinds `"pharmacy"` and `"supplier"`;
  - purposes `"pharmacy.outlet_access"` and `"supply.party_access"`;
  - audit action `workforce.access_candidate.read`.
- **Who is returned:** employer row `establishment_kind = Kind`, `s.employment_status='active'`, `l.status='active'`, `l.independent_staff_version=s.version`, `l.employment_generation_id IS NULL`.
  - The order is `(s.created_at, s.id)` descending, and each staff record appears at most once.
- **Response rows** are `{subject, display_name}` only.
- **Errors:**
  - an unregistered employer is an empty page;
  - an employer of the other kind is `authz.ErrRecordNotFound` (404);
  - any other failure, or a nil provider, is 503 `access_candidates_unavailable`.
- **Pinned files stay unchanged:** `workforce/handlers.go`, `pharmacy/catalogue.go`, `pharmacy/module.go` beyond one route-registration line, and every other file the ledger pins except `workforce/module.go` (approved per Gates).
- **Shell copy:**
  - the empty state is "No staff with a linked account at this pharmacy yet. Add them in Staff first.";
  - the failure message is "Could not load this pharmacy's staff. Try again in a moment.";
  - the grant button and toasts are unchanged from #1718.

## Review Focus

- **A link replaced by a new employment generation, or by a newer staff version**, must not surface a stale subject. Covered by `TestAccessCandidatesExcludeStaleLinks` in Task 1.
- **A caller who is a Workforce manager at the pharmacy** gets the same rows through the new admission, with no duplicates, and the existing policies stay intact for every other reader. Covered by `TestAccessCandidatesAdmissionIsPurposeBound` in Task 1.
- **The proof setting leaking past the transaction** (a later statement on the same connection reading staff) must be impossible. `set_config(...,true)` is transaction-local, and the test reads in a second transaction. Covered in Task 1.
- **A 503 from candidates** must disable the grant form while still showing "Who has access" and its level/remove controls. Covered in Task 3.
- **A grant to someone who left** keeps showing the subject, not a blank or another person's name. Covered in Task 3.

---

### Task 1: Workforce access candidates (interface, migration, implementation)

**Files:**

- Create:
  - `backend/pkg/authz/employer_access_candidates.go`
  - `backend/internal/modules/workforce/access_candidates.sql`
  - `backend/internal/modules/workforce/access_candidates.go`
  - `backend/internal/platform/access_candidates.go`
- Modify:
  - `backend/internal/modules/workforce/module.go`: add `//go:embed access_candidates.sql` and append `{ID: "0122_workforce_access_candidates", SQL: accessCandidatesSchema}` to `Migrations()`;
  - `backend/internal/platform/module.go`: add the `Deps.AccessCandidates authz.EmployerAccessCandidates` field;
  - `backend/cmd/api/main.go` and `backend/internal/testutil/harness.go`: set `AccessCandidates: platform.EmployerAccessCandidates(…, db)`.
- Test: `backend/internal/modules/workforce/access_candidates_test.go`

**Interfaces:**

- Produces `func (*Module) AccessCandidates(db *tenantdb.DB) authz.EmployerAccessCandidates` in Workforce.
- Produces `func EmployerAccessCandidates(modules []Module, db *tenantdb.DB) authz.EmployerAccessCandidates` in platform. It finds the `workforce` module with a provider interface, as `platform.StaffActors` does, and returns nil when there is none.

**Migration design** (`access_candidates.sql`), following `operating_staff_choices.sql`:

- `workforce_access_candidate_context(t uuid, e uuid) boolean` is true only when `current_setting('app.workforce_access_candidates', true)` parses as JSON whose `tenant_id = t`, `employer_id = e`, `purpose` is one of the two purposes, and `kind` matches `workforce_hospitals.establishment_kind` for `(t, e)`.
- Additive permissive `SELECT` policies `workforce_staff_access_candidates` and `workforce_login_links_access_candidates`, each `USING (hms_tenant_visible(tenant_id) AND workforce_access_candidate_context(tenant_id, hospital_id))`. These are new policies, so the existing ones and the #1669 digest checks are untouched.
- An `INSERT` policy on `workforce_audit` whose `WITH CHECK` admits a row only under that context, with `action = 'workforce.access_candidate.read'`, `actor = proof->>'actor_subject'` and `changes ? 'purpose'`.
- `workforce_access_candidate_support() RETURNS boolean`, so the Go side refuses with an unavailable error when the migration is absent, as `operating_staff_choices.go:52` does.

- [ ] **Step 1: Write the failing tests** in `access_candidates_test.go` against the Postgres harness. Seed pharmacy P with staff A (active, active link `user-a`) and B (active, active link `user-b`). Then:
  - `TestAccessCandidatesListLinkedActiveStaff`: as a caller with no Workforce access row, P returns exactly `user-a` and `user-b` with their display names, newest first.
  - `TestAccessCandidatesExcludeStaleLinks`: these are excluded: C inactive; D's link revoked; E's link at `independent_staff_version = version-1`; F's link with a non-null `employment_generation_id`.
  - `TestAccessCandidatesStayInEmployerAndTenant`: staff of pharmacy Q, and of P's twin in tenant B, never appear.
  - `TestAccessCandidatesKind`: `Kind:"supplier"` for P gives `authz.ErrRecordNotFound`. A random unregistered UUID gives an empty slice and nil.
  - `TestAccessCandidatesAuditEachRow`: one `workforce_audit` row per returned staff, with the action, the actor, the resource staff ID and `changes` holding `purpose`, `staff_version`, `link_id` and `link_version`.
  - `TestAccessCandidatesAuditFailureFailsRead`: with the audit insert made to fail (drop the audit policy in a test-only transaction), the call returns an error and no rows.
  - `TestAccessCandidatesAdmissionIsPurposeBound`:
    - with no proof set, a direct `SELECT` from `workforce_staff` as the same caller returns 0 rows;
    - a proof for Q does not admit P's rows;
    - a second transaction after the call sees 0 rows.
  - `TestAccessCandidatesPage`: three candidates with `Limit: 2` give two pages, each subject once.
  - `TestAccessCandidatesRefuseBadRequest`: an unknown purpose, a nil employer, an invalid actor or a non-UUID tenant gives an error, not an empty list.
- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.4 go test ./internal/modules/workforce -run TestAccessCandidates -count=1`. Expected: FAIL to compile (`AccessCandidates` undefined).
- [ ] **Step 3: Implement.**
  - In one `WithTenantCommand`:
    1. check `workforce_access_candidate_support()`;
    2. resolve the employer kind (none → empty; other → `ErrRecordNotFound`);
    3. `set_config('app.workforce_access_candidates', proof, true)`;
    4. run the joined keyset query;
    5. validate each row (`validLoginSubject`, `validBenefitText(name,1,150)`); an invalid row fails the read, never skips silently;
    6. insert the audit rows with `jsonb_to_recordset`, as `operating_staff_choices.go` does;
    7. reset the setting.
  - Add the Deps field, the platform finder and the two `AccessCandidates:` wiring lines.
- [ ] **Step 4: Run** the Step 2 command, then `go test ./internal/modules/workforce -count=1 -timeout=20m` and `go test ./internal/archtest -count=1`. Expected: PASS, including the #1669 `staff_policy_plans` and `staff_generic_plans` integrity tests.
- [ ] **Step 5: Mutations, each reverted:**
  - drop `l.independent_staff_version=s.version`: `TestAccessCandidatesExcludeStaleLinks` fails;
  - skip the audit insert: `TestAccessCandidatesAuditEachRow` fails;
  - drop `employer_id = e` from the context function: `TestAccessCandidatesAdmissionIsPurposeBound` fails.
- [ ] **Step 6: Commit** `feat(workforce): list an employer's linked staff as access candidates (#1719)`.

### Task 2: The pharmacy access-candidates route

**Files:**

- Create: `backend/internal/modules/pharmacy/access_candidates.go`, `access_candidates_test.go`
- Modify: `backend/internal/modules/pharmacy/module.go`, adding one line `accessCandidateRoutes(g, deps)` after `outletAccessGrantRoutes`.

**Interfaces:**

- Consumes `deps.AccessCandidates` (Task 1).
- Produces `GET /pharmacy/pharmacies/:pharmacy/access-candidates` through `platform.ListRoute(g.Group("/pharmacies/:pharmacy"), "/access-candidates", PermAccessManage, h.listCandidates)`. Rows have the shape `accessCandidate{Subject string "subject"; DisplayName string "display_name"}`, with a `PageKey` carried from the `authz` row.

- [ ] **Step 1: Write the failing tests**, using the Workforce module plus pharmacy in the harness and staff seeded at pharmacy P:
  - `TestAccessCandidatesRouteListsNames`: as `"steward"` (`pharmacy.access.manage` only), the response is `[{subject, display_name}]` with no other keys.
  - `TestAccessCandidatesRouteScope`:
    - without the permission, 403;
    - another tenant's pharmacy ID, 404;
    - a UUID that is not a `pharmacy_organizations` row in the tenant, 404;
    - a malformed ID, 404.
  - `TestAccessCandidatesRouteUnavailable`: a harness with a nil provider gives 503 `access_candidates_unavailable`, and so does a stub that returns `errors.New("down")`. Neither is 200.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -run TestAccessCandidatesRoute -count=1`. Expected: FAIL with 404 (no route).
- [ ] **Step 3: Implement** `listCandidates`.
  - Do not use `catalogueScope`, which requires an outlet. Use `pharmacyPrincipal`, parse `:pharmacy`, and check that `pharmacy_organizations` holds `tenant_id=? AND id=?` (count is not 1 → 404).
  - Then call `deps.AccessCandidates.AccessCandidates` with `Kind:"pharmacy"`, `Purpose:"pharmacy.outlet_access"`, `Actor: p.Subject`.
  - Map `authz.ErrRecordNotFound` to 404, and a nil provider or any other error to `respond` 503 `access_candidates_unavailable`. Add a comment arguing that the fail-closed direction is right: an empty list would read as "no staff".
- [ ] **Step 4: Run** the Step 2 command, then `go test ./internal/modules/pharmacy -count=1`. Expected: PASS.
- [ ] **Step 5: Mutation, reverted.** Return `nil, nil` on provider error: `TestAccessCandidatesRouteUnavailable` fails.
- [ ] **Step 6: Commit** `feat(pharmacy): list a pharmacy's linked staff for access grants (#1719)`.

### Task 3: The outlet access panel

**Files:**

- Modify:
  - `apps/shell/components/pharmacy/outlet-access.tsx`
  - `outlet-access.test.tsx`
  - `e2e/tests/pharmacy-outlets-pane.spec.ts` and its fixture: seed a staff record with a linked account for the signed-in user, or use an existing one
- Maybe modify: `packages/api/src/pharmacy.ts`, adding `pharmacyOrgRoot(scope)` = `/pharmacy/pharmacies/{pharmacyId}`, only if no equivalent helper exists.

**Interfaces:**

- Consumes the Task 2 route.
- Query key: `["pharmacy", scope.tenantId, scope.pharmacyId, "access-candidates"]`.
- `type Candidate = { subject: string; display_name: string }`. The `Member` type and the `/iam/members` query are removed.

- [ ] **Step 1: Rewrite the failing tests.** Replace the `/iam/members` stub with an `/access-candidates` stub serving `{user-owner, "Asha Rao"}`, `{user-ph1, "Vikram Shah"}` and `{user-new, "Neha Iyer"}`. Then test:
  - person options show "Neha Iyer" only, since the other two already have grants. Choosing it sends `PUT …/access/user-new`;
  - the "Who has access" rows show "Asha Rao" (marked "You") and "Vikram Shah". A grant for `user-gone`, who is not a candidate, shows `user-gone`;
  - an empty candidate page shows "No staff with a linked account at this pharmacy yet. Add them in Staff first.";
  - a candidates 503 shows the `RequestFailure`; the Person select and "Grant pharmacy access" are disabled; the grant list and its controls still work;
  - no request URL contains `/iam/members`, in every test (check in `serve`);
  - delete the old 403/self-fallback test, and add "without the manage permission, no request goes to `/access-candidates`".
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell test -- outlet-access`, with Node and env set as in memory. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Load candidates with `useApiPagedQuery<Candidate>` and keep `LoadMore`.
  - `names = new Map(subject → display_name)`.
  - `GrantRow` shows `names.get(row.subject) ?? row.subject` in the label, the aria-labels and the confirm text.
  - Choices are candidates not already granted, labelled by name and valued by subject.
  - Pass `disabled` to `GrantForm` when candidates are pending or errored.
  - Remove the self-fallback and the administrator message.
- [ ] **Step 4: Update the e2e spec.** Choose the person by display name.
- [ ] **Step 5: Run** `pnpm turbo lint type-check test --filter=@helivanta/shell --filter=@helivanta/e2e`. Expected: PASS.
- [ ] **Step 6: Mutations, each reverted:**
  - fall back to `[]` on a candidates error and leave the form enabled: the 503 test fails;
  - label options by subject: the name test fails.
- [ ] **Step 7: Commit** `feat(shell): grant outlet access to pharmacy staff by name (#1719)`.

### Task 4: Contracts, inventory, ledger and docs

**Files:**

- Modify:
  - `docs/api/domains/pharmacy.md`: the registry row for `access-candidates`, plus a note that the grants panel uses it;
  - `docs/api/domains/workforce.md`: the access-candidates disclosure, the admission and the audit action;
  - `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md`:
    - SET-07 and SET-08: choose by name;
    - SET-10a: OWN1, who is not a tenant administrator, sees pharmacy staff by name and grants PH2;
  - the generated `docs/api/reference/*` and `docs/qa/staff-operational-access-inventory.json`;
  - `docs/qa/staff-operational-access-reviews.json`, **only after the user approves**: re-record the reviews pinning `workforce/module.go`.
- The spec's status changes to accepted, with the decision to use the separate setting, or whatever Sam123ben's reply settled.

- [ ] **Step 1:** Update the domain docs and the acceptance plan as listed.
- [ ] **Step 2: Run** from `backend/`: `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`. Then `node scripts/staff-operational-access-inventory.mjs` and `--check` from the repo root.
  - Expected: contractdocs shows the new GET and migration `0122`.
  - The inventory generator refuses on `workforce/module.go`. Stop, show the user the affected review IDs, and wait for approval before editing the ledger. Then re-run both checks. Expected: PASS.
- [ ] **Step 3: Commit** `docs(pharmacy): outlet access grants choose pharmacy staff by name (#1719)`, including the spec and this plan.

Final gates:
- `make lint-go`, `./scripts/coverage-gate.sh`, `go test -race ./internal/modules/workforce ./internal/modules/pharmacy ./internal/archtest -timeout=20m`;
- `pnpm turbo lint type-check test build format:check`, in the background;
- re-check `0122` on main and `codex/*`;
- remove the build output.

The PR body states what this slice does not cover:
- supply (slice 2);
- PUT/DELETE restriction to staff;
- hospital screens;
- names for non-candidates;
- search.
