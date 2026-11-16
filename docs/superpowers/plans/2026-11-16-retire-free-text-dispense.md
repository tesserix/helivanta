# Retire the free-text dispense route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `POST /pharmacy/dispenses/:id/dispense` and the visit intake from recording unchecked hand-overs, keep history readable, and prove the counter-sale-for-an-OPD-encounter path in e2e (#1680).

**Architecture:** The pharmacy module keeps the route registered but answers 410. It drops the `pharmacy-visit-intake` consumer and the unconsumed `dispense_recorded.v1` event. The shell turns the queue into read-only "Past dispenses" and removes the dashboard dispense tile. No migration.

**Tech Stack:** Go (gin, gorm, JetStream bus), Next.js shell, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-16-retire-free-text-dispense-design.md`

## Global Constraints

- Branch `feat/1680-retire-free-text-dispense` from main. Do not touch `sale.go` (#1477 holds it).
- No migration. The `pharmacy_dispenses` schema, status CHECK and audit trigger are unchanged.
- No row is ever rewritten. Pending rows stay `pending`.
- Error code `dispense_retired`, HTTP 410, via `respond.Error(c, http.StatusGone, "dispense_retired", …)`.
- The route keeps `PermDispenseFulfil` (`pharmacy.dispense.fulfil`). Permission lists are unchanged.
- MediCore's `visit_created.v1` and lab's consumer are unchanged.
- Gates run one after another, not concurrently: Go on toolchain `go1.27.4`, web on Node `v24.21.0`.
- Commits carry no AI attribution.

## Review Focus

1. A stale client posts to the retired route for a **pending** row, and expects 410 with the row unchanged. Task 1 pins this.
2. The same post for an **already dispensed** row or an **unknown/other-tenant ID** must still return 410, not 404 or 409, and must not reveal whether the row exists. Task 1 pins this.
3. A `visit_created.v1` redelivered after the deploy creates **no** pharmacy row, while lab still records its order. Task 2 pins this.
4. A user with `pharmacy.dispense.read` but not `fulfil` opening Past dispenses sees no action and no broken button. Task 4 pins this.
5. A dashboard viewer whose only analytics permission is `pharmacy.dispense.read` loses the services view entirely, and must not get an empty panel. Task 5 pins this.

---

### Task 1: Retired route answers 410

**Files:**

- Modify: `backend/internal/modules/pharmacy/dispenses.go` (`fulfil`, `dispenseRequest`)
- Modify: `backend/internal/modules/pharmacy/module_test.go` (`TestDispenseFlow`, `TestConcurrentDispenseYieldsExactlyOneWinner`)

**Interfaces:**

- Produces: `func (h *dispenseHandlers) fulfil(c *gin.Context)`. The same name and route registration, now refusing every call.

- [ ] **Step 1: Replace `TestDispenseFlow` and delete `TestConcurrentDispenseYieldsExactlyOneWinner` with `TestRetiredDispenseRouteRefusesWithoutWriting`**
  - Seed one pending and one dispensed row for TenantA (direct insert under `WithTenant`), and capture each row's full column set plus the TenantA `outbox_events` count.
  - For each of {pending ID, dispensed ID, random UUID, a TenantB row ID}, post `{"medication":"Paracetamol"}` as a `pharmacy.dispense.fulfil` holder and assert `410` with body code `dispense_retired`.
  - Afterwards both rows are byte-equal to the capture, the outbox count is unchanged, and `audit_events` holds no `update` for either row.
  - Without the permission the same post returns `403`.
- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.4 go test -race -run TestRetiredDispenseRouteRefusesWithoutWriting ./internal/modules/pharmacy/`. Expected: FAIL (the old handler returns 200/404).
- [ ] **Step 3: Implement.** The body of `fulfil` becomes a single `respond.Error(c, http.StatusGone, "dispense_retired", "Free-text dispensing is retired (#1680). Record a counter sale against the patient's OPD encounter.")`. Delete `dispenseRequest`, the update and the publish. Add a one-line comment arguing fail-closed: it refuses before reading the row, so no row state reaches a write. Remove imports that become unused.
- [ ] **Step 4: Run** the Step 2 command. Expected: PASS.
- [ ] **Step 5: Commit** `fix(pharmacy): refuse the free-text dispense route with 410 (#1680)`.

### Task 2: Remove the visit intake consumer

**Files:**

- Modify: `backend/internal/modules/pharmacy/consumers.go` (drop the first `events.Consumer`)
- Modify: `backend/internal/modules/pharmacy/module_test.go` (`TestVisitIntakeCreatesPendingDispense`, the audit trail `system:pharmacy-visit-intake` row)
- Modify: `backend/pkg/events/retention.go:26-30` (comment example)
- Test: any test that seeds pending dispenses by publishing `visit_created` (`grep -rn SubjectVisitCreated backend/internal/modules/pharmacy`) switches to a direct insert

- [ ] **Step 1: Replace `TestVisitIntakeCreatesPendingDispense` with `TestVisitCreatedNoLongerOpensADispense`.** Publish `visit_created.v1` for TenantA through the bus with lab's and pharmacy's consumers running, wait until lab's order for that visit exists, then assert that `pharmacy_dispenses` has zero rows for that `visit_id`.
- [ ] **Step 2: Run** it. Expected: FAIL (one row is created).
- [ ] **Step 3: Implement.** Remove the consumer, and drop the `medicorecontract` import if unused. Rewrite the `retention.go` comment to describe the duplicate as "a second row from any idempotent insert consumer (for example lab's visit intake)". The constant and its test are untouched.
- [ ] **Step 4: Run** `go test -race ./internal/modules/pharmacy/ ./pkg/events/ ./internal/archtest/`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(pharmacy): stop opening dispenses from new visits (#1680)`.

### Task 3: Retire `dispense_recorded.v1` and regenerate contracts

**Files:**

- Modify: `backend/internal/modules/pharmacy/contract/events.go` (delete `SubjectDispenseRecorded`, `DispenseRecordedData`)
- Modify: `backend/internal/modules/pharmacy/module.go:331` (`Publishes()` entry)
- Modify: `docs/standards/backend.md:115,310,570-575,630-640`. Replace the worked examples with a live pharmacy subject and consumer from `Publishes()` and `Consumers()` (e.g. `pharmacy-registered-patient`).
- Modify: `docs/api/domains/pharmacy.md`. Mark the route 410, and mark the intake and `dispense_recorded.v1` retired by #1680. Fix the line-703 payload note.
- Regenerate: `docs/api/reference/{inventory.json,README.md,event-payloads.json}`, `docs/qa/staff-operational-access-inventory.json`

- [ ] **Step 1: Delete** the constant, the payload and the `Publishes()` entry, then run `go build ./...`. Expected: it compiles (Task 1 already removed the only publisher).
- [ ] **Step 2: Run** `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`, then `node scripts/staff-operational-access-inventory.mjs && node scripts/staff-operational-access-inventory.mjs --check`, then check `docs/api/schemas.md` for an `event-payloads.json` generator and run it. Expected: `-check` clean, and `git diff` shows only the subject's removal and the route's description.
- [ ] **Step 3: Run** `go test -race ./internal/archtest/` (`TestPublishedSubjectConstants`, `TestConsumerContracts`). Expected: PASS.
- [ ] **Step 4: Edit** both docs as listed, then run `grep -rn 'dispense_recorded\|pharmacy-visit-intake' docs backend`. Expected: only retirement notes and this plan/spec.
- [ ] **Step 5: Commit** `refactor(pharmacy): retire dispense_recorded.v1 and document the 410 (#1680)`.

### Task 4: Past dispenses page is read-only

**Files:**

- Modify: `apps/shell/components/pharmacy/dispense-list.tsx` (remove the `useApiMutation` at ~183, the `Can permission="pharmacy.dispense.fulfil"` block at ~119 and the button at ~208, and add the notice)
- Modify: `apps/shell/components/pharmacy/dispense-list.test.tsx`
- Modify: `packages/ui/src/zones.ts:217` label `"Dispense queue"` → `"Past dispenses"`
- Modify: `apps/shell/app/(workspace)/dashboard/page.tsx:16` → `"Past dispenses from the retired queue"`
- Modify: `apps/shell/components/medicore/visit-panel.tsx:43` → `"New visits open a pending order in Lab."`

- [ ] **Step 1: Write the tests** in `dispense-list.test.tsx` (`renderWithProviders`):
  - `"shows past dispenses without a dispense action, even for a fulfil holder"`: with both permissions granted and one pending and one dispensed row mocked, `queryByRole("button", { name: /dispense/i })` is null.
  - `"tells the pharmacist the queue is retired and where to sell instead"`: the notice text includes "retired", and the link `getByRole("link", { name: /counter sale/i })` has its href pointing to the counter-sale route (find the route under `apps/shell/app/(workspace)/pharmacy/`).
  - Delete the existing tests that click Dispense.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell exec vitest run components/pharmacy/dispense-list.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement** the notice as a `next/link` inside the existing panel styles (design tokens only). It reads: "The dispense queue is retired. Serve an OPD patient with a counter sale against their encounter; rows below are kept as recorded." The Waiting tab gets copy that its rows were never completed in the retired queue.
- [ ] **Step 4: Run** Step 2, and also `pnpm --filter @helivanta/ui test`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(shell): show past dispenses read-only (#1680)`.

### Task 5: Remove the dashboard dispense tile

**Files:**

- Modify: `apps/shell/components/dashboard/operational-dashboard.tsx` (`pharmacyCategories` at 42, `hasAnalytics` at 68-69, the `services` view `allowed` at 76, the pharmacy `AnalyticsPanel` at 263-273, and the view label `"Diagnostics & pharmacy"` → `"Diagnostics"`)
- Test: that component's existing test file (`ls apps/shell/components/dashboard/*.test.tsx`)

- [ ] **Step 1: Write the tests:**
  - `"renders no pharmacy dispense panel"`: with all permissions, there is no heading "Pharmacy" and no request to `/pharmacy/insights`.
  - `"a pharmacy-only reader gets no services view"`: with only `pharmacy.dispense.read`, there is no tab "Diagnostics" and no analytics panel.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement** by removing `pharmacy.dispense.read` from `hasAnalytics` and from the services `allowed` check, and deleting the panel and `pharmacyCategories`.
- [ ] **Step 4: Run** them again. Expected: PASS.
- [ ] **Step 5: Commit** `feat(shell): drop the retired dispense queue from the dashboard (#1680)`.

### Task 6: e2e follows the replacement path

**Files:**

- Modify: `e2e/tests/journey.spec.ts:72-140`
- Modify or delete: `e2e/tests/pharmacy-paging.spec.ts`
- Modify: `e2e/tests/navigation.spec.ts:59-61,352`, `e2e/tests/dashboard-phone.spec.ts:44`, `e2e/tests/support/queue.ts` (drop the now-unused exports)
- Uses: `clinicalFixture` (`e2e/tests/support/opd.ts:27`), `readyCounter` (`e2e/tests/support/pharmacy-counter.ts:337`)

- [ ] **Step 1: Rewrite the #976 test** as `"pharmacist sells against an OPD encounter opened by admin"`.
  - Admin opens an encounter through `clinicalFixture`. Prepare a counter with `readyCounter` at the same hospital. First read the sale's encounter check (`grep -n pharmacy_sale_encounter backend/internal/modules/pharmacy/*.sql`). If the outlet must belong to the encounter's hospital and `readyOutlet` cannot provide that, use the hospital-owned outlet setup from `e2e/tests/pharmacy-hospital-setup.spec.ts`.
  - The pharmacist chooses the patient and encounter in the counter sale and sells one scheduled line.
  - Assert the persisted sale's `encounter.encounter_id` equals the encounter, the lot's on-hand quantity dropped by the sold units, and the line appears in the H1 or prescription register read for that outlet.
- [ ] **Step 2: Paging spec.** If history rows can be seeded without the retired path (no test-only API exists today, and adding one is out of scope), delete `pharmacy-paging.spec.ts`. Keyset paging stays covered by `TestDispensesArePaginated` and the Vitest list tests.
- [ ] **Step 3: Update** the navigation spec (`current: "Past dispenses"`; the `/dispense` tab link check targets "Waiting", which still exists), dashboard-phone (title "past dispenses fit a phone", same path), and keep `smoke.spec.ts` selectors (`grep -n -i dispense e2e/tests/smoke.spec.ts`).
- [ ] **Step 4: Run** `pnpm --filter e2e exec tsc --noEmit && pnpm --filter e2e lint`, then run the changed specs against the local dev stack if it is up (`make dev-infra`, seeded). Report whether they actually ran. Expected: PASS, or "not run locally" stated in the PR.
- [ ] **Step 5: Commit** `test(e2e): prove the OPD counter sale replaces the dispense queue (#1680)`.

### Task 7: Gates and PR

- [ ] **Step 1:** `make lint-go`, `cd backend && GOTOOLCHAIN=go1.27.4 ./scripts/coverage-gate.sh`. Then, separately, run the web gate with the Node 24 and CI env vars, then `make lint-shell`. Rerun any load timeouts in isolation before treating them as real.
- [ ] **Step 2:** Remove the build artifacts (`apps/shell/.next`, coverage output).
- [ ] **Step 3:** Commit the spec and this plan (`docs(pharmacy): design and plan for retiring free-text dispense (#1680)`). Do not add the untracked #1719 spec/plan files.
- [ ] **Step 4:** Push, and open the PR with `Closes #1680`. The body lists the contracts changed (route 410, consumer and event retired), the ops step `nats consumer rm <stream> pharmacy-visit-intake`, and the slice exclusions from the spec.
