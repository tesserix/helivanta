# Schedule Class Lowering Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** No tenant member or pair can make an approved schedule class less strict; a lowering needs an owner countersign and a Helivanta platform decision.

**Architecture:** One pharmacy migration (`0050_pharmacy_schedule_lowering`) records `lowers` at insert, refuses tenant reviews of lowerings, adds countersigns and lets `helivanta_drug_approver` decide. The pharmacy module gains a countersign route and an exported decide/list API used by a new `cmd/pharmacy-schedule-review`. The shell schedule screen shows the new states.

**Tech Stack:** Go (gin, gorm, Postgres RLS, testcontainers), Next.js shell (Vitest, `@helivanta/api`).

**Spec:** `docs/superpowers/specs/2026-11-16-schedule-class-lowering-design.md`

## Global Constraints

- Strictness rank `none < G < H < H1 < X`; NDPS `true` stricter than `false`; lowering = in-force class exists and (lower rank OR drops NDPS).
- Migration ID `0050_pharmacy_schedule_lowering`, appended to `module.go`'s list; re-check `main` and `codex/*` before merge (#1477 holds 0049).
- Tenant refusal constraint name `pharmacy_schedule_platform_review`; API code `platform_review_required` (409).
- New permission `pharmacy.schedule.countersign`, granted only to `RolePharmacyOwner`.
- Platform role: `helivanta_drug_approver`; command `cmd/pharmacy-schedule-review` with `list` and `decide`, DSN env `SCHEDULE_REVIEW_DATABASE_URL`.
- Every declaration/countersign/review table stays append-only with its audit trigger.
- Backend and web gates run one after another; `GOTOOLCHAIN=go1.27.4`, `-timeout=20m`.

## Review Focus

1. A lowering declared while an earlier lowering is still pending: in-force must stay the last approved stricter class, and the new request is still a lowering (compare against in-force, not the latest row).
2. A raise filed after a pending lowering: the raise blocks the product as today; once approved it is in force and the old lowering, if later approved by the platform, must not override it (the latest non-skipped declaration wins, so the approved raise is newer). Test in Task 3.
3. The platform role reading tenant rows: it must see only the declaration/countersign/review/product-name columns it needs, nothing in sales or patients. Test in Task 3 (a `SELECT` on `pharmacy_sales` as the approver fails).
4. Owner who is also the declarer: countersign refused with `four_eyes`. Test in Task 2.
5. Repeated `decide` on an already-decided request: refused, not double-recorded. Test in Task 3.

---

### Task 1: Record lowering and refuse tenant approval

**Files:**
- Create: `backend/internal/modules/pharmacy/schedule_lowering.sql`
- Modify: `backend/internal/modules/pharmacy/module.go` (embed `scheduleLoweringSchema`, append migration), `schedule_class.go` (`scheduleError` mapping; `lowers` on `scheduleDeclaration` and `declarationQuery`; `getSchedule` reason)
- Test: `backend/internal/modules/pharmacy/schedule_lowering_test.go`

**Interfaces:**
- Produces: column `pharmacy_product_schedule_declarations.lowers boolean`; SQL function `pharmacy_schedule_rank(class text) int`; `scheduleDeclaration.Lowers bool` (`json:"lowers"`); `pharmacy_product_schedule_in_force` unchanged signature, new semantics.

- [ ] **Step 1: Write failing tests** in `schedule_lowering_test.go`, reusing `scheduleHarness`, `declare`, `review`, `scheduleInForce`, and a local `declareWith(t, r, product, class, ndps, token)`:
  - `TestPharmacistAndOwnerCannotMoveH1ToNone`: editor declares H1, pharmacist approves (200); pharmacist declares `none`; editor reviews `approved` → 409 with code `platform_review_required`; `scheduleInForce` = `["H1"]`; GET history[0] has `lowers: true`.
  - `TestTheDatabaseRefusesATenantApprovalOfALowering`: same setup, then a direct `INSERT INTO pharmacy_product_schedule_reviews` under `WithTenant` as `user-editor` fails with constraint `pharmacy_schedule_platform_review`.
  - `TestLoweringIsDecidedByTheDatabase`: a raw insert of a `none` declaration with `lowers=false` over approved H1 is stored `lowers=true`; a raw insert of `X` with `lowers=true` is stored `false`.
  - `TestDroppingNDPSIsALowering`: approved `H` with ndps=true, then `H1` ndps=false → `lowers: true`.
  - `TestAWaitingLoweringKeepsTheOldClass`: approved H1, pending `none` → in-force `H1`, GET `in_force.regulatory_class == "H1"` and `reason` mentions Helivanta review.
  - `TestAPendingRaiseStillBlocks`: approved `H`, pending `H1` → in-force empty (existing rule preserved).
  - `TestConcurrentDeclarationsAreClassifiedAgainstTheClassInForce`: approved H1; 8 goroutines declare `none` concurrently → all `lowers=true`.
  - `TestAFirstDeclarationIsNotALowering`: first declaration `none` → `lowers: false`, tenant review approves it (documents the not-covered gap).

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/modules/pharmacy -run 'Lowering|H1ToNone|NDPS|PendingRaise|FirstDeclaration' -race -timeout=20m`; Expected: compile error on `lowers` / 200 instead of 409.

- [ ] **Step 3: Implement the migration.** `ALTER TABLE … ADD COLUMN lowers boolean NOT NULL DEFAULT false`; `pharmacy_schedule_rank` (IMMUTABLE); `BEFORE INSERT` trigger that takes `SELECT … FROM pharmacy_catalogue_products … FOR UPDATE` on the product row, then sets `NEW.lowers` from the in-force row; extend `pharmacy_schedule_review_guard` to raise `23514` / `pharmacy_schedule_platform_review` when the declaration `lowers` and `current_user <> 'helivanta_drug_approver'`; `CREATE OR REPLACE` the in-force function to pick the latest declaration `WHERE NOT lowers OR EXISTS(approved review)`, then require it approved. Header comment argues the fail-closed direction. Map the constraint to `409 platform_review_required` in `scheduleError`; add `lowers` to the query and struct; `getSchedule` sets `InForce` from `pharmacy_product_schedule_in_force` when the latest declaration is a waiting/rejected lowering, with reason "a lower class waits for Helivanta review" / "Helivanta rejected the lower class".

- [ ] **Step 4: Run, expect PASS**, plus `go test ./internal/modules/pharmacy -run Schedule -race` (existing tests unchanged).

- [ ] **Step 5: Commit** `fix(pharmacy): keep tenant staff from lowering a schedule class (#1681)`.

### Task 2: Owner countersign

**Files:**
- Modify: `schedule_lowering.sql` (countersign table, guard), `schedule_class.go` (route, handler, GET fields), `module.go` (`PermScheduleCountersign`, grant)
- Test: `schedule_lowering_test.go`

**Interfaces:**
- Consumes: `lowers` from Task 1.
- Produces: table `pharmacy_product_schedule_countersigns (tenant_id, outlet_id, declaration_id PK-part, reason, countersigned_by, created_at)`; `PermScheduleCountersign authz.Permission = "pharmacy.schedule.countersign"`; route `POST /catalogue/products/:id/schedule/:declaration/countersign` body `{"reason"}`; history fields `countersigned_by`, `countersign_reason`, `countersigned_at` (nullable).

- [ ] **Step 1: Failing tests** (add an `owner` token with `schedulePerms` + `pharmacy.schedule.countersign`, granted catalogue access):
  - `TestTheOwnerCountersignsALowering`: lowering by pharmacist → owner countersign 201; repeat same body 200; different reason 409; GET shows `countersigned_by == "user-owner"`.
  - `TestTheDeclarerCannotCountersign`: owner declares the lowering, owner countersigns → 409 `four_eyes`.
  - `TestARaiseCannotBeCountersigned`: 409.
  - `TestCountersignNeedsThePermissionAndTheTenant`: pharmacist (no perm) 403; `foreign` 404; unknown declaration 404.
  - `TestOnlyOwnersHoldCountersign`: `pharmacy.New().Permissions()` grants `pharmacy.schedule.countersign` to exactly `[RolePharmacyOwner]`.
- [ ] **Step 2: Run, expect FAIL** (404 route missing).
- [ ] **Step 3: Implement**: table with forced-RLS boilerplate matching the reviews table, audit and immutable triggers; guard raising `pharmacy_schedule_four_eyes` for the declarer and `pharmacy_schedule_countersign_lowering_only` for a non-lowering; handler mirrors `reviewScheduleDeclaration` with the idempotency check of `putScheduleDeclaration`.
- [ ] **Step 4: Run, expect PASS**; regenerate `go run ./cmd/contractdocs` and `node ../scripts/staff-operational-access-inventory.mjs`.
- [ ] **Step 5: Commit** `feat(pharmacy): owner countersigns a lower schedule class (#1681)`.

### Task 3: Platform decision and command

**Files:**
- Modify: `schedule_lowering.sql` (grants and policies for `helivanta_drug_approver`; review guard for countersign and distinct names; `by_platform boolean NOT NULL DEFAULT false` on reviews, set by trigger from `current_user`, never by the client), `schedule_class.go` (history field `decided_by_platform`)
- Create: `backend/internal/modules/pharmacy/schedule_platform.go`, `schedule_platform_test.go`, `backend/cmd/pharmacy-schedule-review/main.go`, `main_test.go`

**Interfaces:**
- Consumes: Tasks 1–2 tables.
- Produces:
  - `type ScheduleLowering struct { TenantID, OutletID, ProductID, DeclarationID uuid.UUID; ProductName, CurrentClass, RequestedClass, Source, DeclaredBy, CountersignedBy string; CurrentNDPS, RequestedNDPS bool; DeclaredAt time.Time }`
  - `func ListScheduleLowerings(ctx context.Context, db *tenantdb.DB) ([]ScheduleLowering, error)` — countersigned, undecided, oldest first, bounded 200.
  - `func DecideScheduleLowering(ctx context.Context, db *tenantdb.DB, declaration uuid.UUID, by string, approve bool, reason string) error`; errors `ErrLoweringNotWaiting`, validation for empty `by`/reason > 500.
  - Command usage: `pharmacy-schedule-review list | decide -declaration ID -by NAME (-approve|-reject) -reason TEXT`.

- [ ] **Step 1: Failing tests** in `schedule_platform_test.go`, approver DB opened via `testinfra.DrugApproverDSN(t, current_database())` as in `drugmaster/release_test.go`:
  - `TestHelivantaApprovesACountersignedLowering`: list returns it with current `H1`, requested `none`; decide approve → in-force `none`; a counter sale line for the product then needs no prescription (reuse the sale test helpers).
  - `TestHelivantaRejectionKeepsTheClass`: reject → in-force `H1`; GET reason says rejected.
  - `TestNoDecisionWithoutCountersign`: `ErrLoweringNotWaiting`, nothing listed.
  - `TestTheReviewerIsNotTheDeclarerOrCountersigner`: `by` equal to declarer subject or countersigner (case/space varied) → refused with `pharmacy_schedule_four_eyes`.
  - `TestADecidedLoweringCannotBeDecidedAgain`: second decide → `ErrLoweringNotWaiting`, one review row.
  - `TestTheApproverSeesOnlyScheduleRows`: as approver, `SELECT 1 FROM pharmacy_sales` fails with permission denied; insert into reviews for a non-lowering declaration refused.
  - `TestALaterRaiseWinsOverAnOldLowering` (Review Focus 2): approved H1, countersigned `none`, then approved `X`, then platform approves `none` → in-force `X`.
  - `TestATenantReviewIsNeverMarkedPlatform`: a tenant raw insert of an approving review for a raise with `by_platform=true` is stored `false`.
  - `TestTheAppRoleCannotActAsPlatform`: the same decide through the `hms_app` DB → refused.
  - `cmd/.../main_test.go`: `TestRunRefusesIncompleteCommands` (missing DSN, flags, both `-approve -reject`).
- [ ] **Step 2: Run, expect FAIL** (undefined functions).
- [ ] **Step 3: Implement**: grants of `SELECT` on needed columns and `INSERT` on reviews with policies `TO helivanta_drug_approver`; the guard requires a countersign and compares `reviewed_by` against declarer and countersigner; Go functions run under `db.WithSystem` and `tenantdb.WithActor(ctx, "helivanta:"+by)`; verify the audit row is written under the approver role and add the grant it needs if not (ledger a ruling). Command mirrors `cmd/drugmaster-release`.
- [ ] **Step 4: Run, expect PASS**; `go test ./cmd/pharmacy-schedule-review ./internal/modules/pharmacy -race -timeout=20m`.
- [ ] **Step 5: Commit** `feat(pharmacy): Helivanta decides a lower schedule class (#1681)`.

### Task 4: Schedule screen

**Files:**
- Modify: `apps/shell/components/pharmacy/schedule-class.tsx`, `schedule-class.test.tsx`

**Interfaces:**
- Consumes: history fields `lowers`, `countersigned_by`, `countersign_reason`, `countersigned_at`, review `decision` and `decided_by_platform` (bool, from Task 3); route from Task 2; permission `pharmacy.schedule.countersign`.

- [ ] **Step 1: Failing Vitest tests** (`renderWithProviders`): a lowering shows "Waiting for Helivanta review" and the still-in-force class; no review button on a lowering even with `pharmacy.schedule.review`; owner with countersign permission sees "Countersign", submits a required reason (inline zod error when empty), toast "Countersigned"; without the permission no button; a decided lowering shows "Decided by Helivanta".
- [ ] **Step 2: Run, expect FAIL** — `pnpm --filter shell test schedule-class`.
- [ ] **Step 3: Implement** with `useApiMutation`, `useZodForm` + `Field`, `Can`, sonner.
- [ ] **Step 4: Run, expect PASS**.
- [ ] **Step 5: Commit** `feat(shell): show and countersign a lower schedule class (#1681)`.

### Task 5: Contracts, docs and gates

**Files:**
- Modify: `docs/api/domains/pharmacy.md`, the OpenAPI file describing the schedule routes (find with `git grep -l 'schedule/{declaration}' contracts`), regenerated `docs/api/reference/*`, staff inventory; spec status → implemented with implementation notes.

- [ ] **Step 1:** Document the lowering rule, countersign route, 409 codes, platform command and the `helivanta_drug_approver` production prerequisite.
- [ ] **Step 2:** `go run ./cmd/contractdocs -check`, staff inventory `--check` → pass.
- [ ] **Step 3:** Gates one at a time: `make lint-go`, `make lint-shell`, `./scripts/coverage-gate.sh`, web turbo gate (Node 24 + CI env). Expected: green (customeronboarding journey excepted, as before).
- [ ] **Step 4: Commit** `docs(pharmacy): document schedule class lowering review (#1681)`.
