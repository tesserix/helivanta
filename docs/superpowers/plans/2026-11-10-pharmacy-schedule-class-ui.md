# Schedule class in the catalogue (#1636) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a pharmacist declares a product's schedule class on the open product, and a second person approves or rejects it, with no API call by hand.

**Architecture:**

- A `ProductScheduleSection`, shaped like `ProductCategorySection` (`item-categories.tsx`), mounted in `OpenProduct` above Tax.
- A typed client in `packages/api`.
- No backend change.

**Tech Stack:** React 19, React Query via `@helivanta/api`, `@helivanta/ui` forms, Vitest with `renderWithProviders`, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-10-pharmacy-schedule-class-ui-design.md`

## Global Constraints

- **Request bodies:**
  - Declaration: `{regulatory_class: "none"|"G"|"H"|"H1"|"X", ndps: boolean, source}`, where source is 1-300 characters after trimming.
  - Review: `{decision: "approved"|"rejected", reason}`, where reason is 1-500 characters.
- **Class and NDPS start empty and are required.** The errors are "Choose the schedule class" and "Say whether it is under NDPS".
- **One `newPharmacyId()` per declaration form instance.** The form is keyed on `history[0]?.id ?? "none"`.
- **Copy:**
  - In force: "Schedule {class} · {NDPS|not NDPS}, approved {time}". The class `none` reads "Not scheduled".
  - Nothing in force: "No schedule class in force: {reason}".
  - Declare: button "Declare schedule class", toast "Schedule class declared".
  - Review: label "What you checked", buttons "Approve" and "Reject", toast "Review recorded".
- **Times** use `formatZonedDateTime(iso, timeZone)`.
- **Invalidation** after either command: `pharmacyKey(scope, "product-schedule", product.id)` and `pharmacyKey(scope, "counter", "availability", product.id)`.
- **Failures:**
  - A failed read is `RequestFailure` (#1595).
  - Mutation errors show inline as `role="alert"` with `error.message`, using `suppressErrorToast: true`.
- **Stage files by path.** Never stage `apps/shell/AGENTS.md`, `.idea/` or `.serena/`.

## Review Focus

1. **A double submit or a retried 503** declares once, because the same id is reused. Pinned in Task 2.
2. **A review that lost a race** (409 already reviewed, or 404) refetches, so a stale "Awaiting review" disappears. Pinned in Task 3.
3. **An unread NDPS field** never records `ndps: false`. Pinned in Task 2.
4. **A reader, or an editor without the grants,** sees the state with no forms. Pinned in Tasks 1 and 4.
5. **An older pending declaration** is shown as superseded and never offered for review. Pinned in Task 3.

---

### Task 1: Client and the state display

**Files:**

- Create: `packages/api/src/pharmacy-schedule.ts`. Add `export * from "./pharmacy-schedule";` to `packages/api/src/index.ts` next to `pharmacy-item-categories`.
- Create: `apps/shell/components/pharmacy/schedule-class.tsx`
- Test: `apps/shell/components/pharmacy/schedule-class.test.tsx`

**Interfaces:**

- **Produces:**
  - `type PharmacyScheduleDeclaration = {id, product_id, regulatory_class: "none"|"G"|"H"|"H1"|"X", ndps: boolean, source, declared_by, created_at, decision: "approved"|"rejected"|null, decision_reason: string|null, reviewed_by: string|null, reviewed_at: string|null}`
  - `type PharmacyScheduleState = {in_force: PharmacyScheduleDeclaration|null, origin: "declared_by_outlet", reason: string, history: PharmacyScheduleDeclaration[]}`
  - `pharmacyProductSchedulePath(scope, productId) => \`${pharmacyRoot(scope)}/catalogue/products/${productId}/schedule\``
  - `ProductScheduleSection({scope, product, timeZone, canDeclare, canReview, onStateChange?, onReviewStateChange?})`. The section has heading "Schedule class" with id `product-schedule-heading`.

- [ ] **Step 1: Write failing tests** in `describe("ProductScheduleSection")`. Copy the `scope`, `product` and `serve` harness from `item-categories.test.tsx`, serving `/schedule`.
  - `it("shows the class in force with its approval time")`: `in_force` is H1 with `ndps: false` and `reviewed_at: "2026-11-09T05:00:00Z"`, and `timeZone` is "Asia/Kolkata". Expect the text "Schedule H1 · not NDPS, approved" and the zoned time.
  - `it("says none is in force, with the API's reason")`: `in_force` is null with reason "the latest declaration awaits review by a second person". Expect "No schedule class in force: the latest declaration awaits review by a second person", and text saying the product can't be released or sold.
  - `it("warns that X or NDPS cannot be released or sold yet")`: once with class X in force, once with `ndps: true` in force.
  - `it("says only the latest 50 declarations are shown")`: 50 history rows.
  - `it("reports a failed read through RequestFailure")`: a 503 offers "Try again" and refetches. A 401 shows "Your session has ended…" with no button.
  - `it("offers no forms without the grants")`: `canDeclare` and `canReview` are false. Expect no "Declare schedule class" and no "Approve".
- [ ] **Step 2:** `pnpm --filter @helivanta/shell test schedule-class` fails (no module).
- [ ] **Step 3:** Implement the client types and path, and the section's read and display. Use `useApiQuery<PharmacyScheduleState>(pharmacyKey(scope, "product-schedule", product.id), path)`. Render history rows the way `ProductCategorySection` does, with class, NDPS, source, declared by and `formatZonedDateTime(created_at)`. The status line comes in Task 3, so for now show the decision text only.
- [ ] **Step 4:** The tests pass, and `pnpm --filter @helivanta/shell lint type-check` passes.
- [ ] **Step 5:** Commit `feat(pharmacy): show a product's schedule class and its history (#1636)`.

### Task 2: Declaring

**Files:**

- Modify: `schedule-class.tsx`
- Test: `schedule-class.test.tsx`

**Interfaces:**

- Consumes: Task 1's types, path and section.
- Produces: `ScheduleDeclarationForm({path, invalidate, onStateChange})`, internal to the file.

- [ ] **Step 1: Failing tests.**
  - `it("requires the class and NDPS to be chosen, with no default")`: submitting empty shows "Choose the schedule class" and "Say whether it is under NDPS", and there is no PUT.
  - `it("refuses a blank source")`: a source of spaces shows an inline error, and there is no PUT.
  - `it("declares with the chosen values")`: choose H and No (via `chooseOption`), source "pack label". Expect one PUT to `…/schedule/{uuidv7}` with body `{regulatory_class:"H", ndps:false, source:"pack label"}`, the toast "Schedule class declared", and a refetch of `/schedule`.
  - `it("reuses the id when a failed declaration is retried")`: the first PUT returns 503 and the second 201. Both PUT URLs are equal, and the 503 message is shown in `role="alert"`.
  - `it("offers no declaration on a withdrawn product")`.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement the form.
  - **Schema:**
    - `regulatory_class: z.enum(["none","G","H","H1","X"])`, with "" refused using the message above.
    - `ndps: z.enum(["yes","no"])`, mapped to a boolean on submit.
    - `source: z.string().trim().min(1, "Name the source, such as the pack label").max(300)`.
  - **Controls:** both selects are `FormSelectField`, with options Not scheduled, G, H, H1, X and Yes, No.
  - **Unsaved state:** use `useReportState(onStateChange, isDirty, isPending)`.
- [ ] **Step 4:** The tests pass, and lint and type-check pass.
- [ ] **Step 5:** Commit `feat(pharmacy): declare a product's schedule class (#1636)`.

### Task 3: Reviewing and the status line

**Files:**

- Modify: `schedule-class.tsx`
- Test: `schedule-class.test.tsx`

**Interfaces:**

- Consumes: Task 1's types and section.
- Produces: `ScheduleReviewForm({declaration, path, invalidate, onStateChange, onStale})`, where `onStale` refetches the schedule read.

- [ ] **Step 1: Failing tests.**
  - `it("approves the newest pending declaration with what was checked")`: expect a POST to `…/{id}/review` with `{decision:"approved", reason}` and the toast "Review recorded". A blank reason is refused inline with no POST.
  - `it("rejects with a reason")`: expect the body to have `decision: "rejected"`.
  - `it("marks an older pending declaration superseded and offers no review of it")`: there are two pending rows. Only one "Approve" appears, for the newest. The older row reads "Superseded by a newer declaration".
  - `it("shows the four-eyes refusal inline")`: a 409 with `{error:"four_eyes", message:"a schedule class is reviewed by someone other than who declared it"}` shows that text in `role="alert"`.
  - `it("refetches when the declaration was already reviewed")`: the 409 "this declaration has already been reviewed" is shown, and `/schedule` is fetched again.
  - `it("refetches on a 404")`, the same way.
  - `it("allows a review on a withdrawn product")`.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Implement the form.
  - **When it's offered:** with `canReview` and `history[0]?.decision === null`.
  - **Status lines:**
    - "Awaiting review" for the newest pending row;
    - "Superseded by a newer declaration" for an older pending row;
    - "Approved by {reviewed_by}: {decision_reason}";
    - "Rejected by {reviewed_by}: {decision_reason}".
  - **Stale reviews:** `onError`, when the error is an `ApiError` with status 404, or 409 with a message other than `four_eyes`, calls `onStale`.
- [ ] **Step 4:** The tests pass, and lint and type-check pass.
- [ ] **Step 5:** Commit `feat(pharmacy): review a product's schedule class (#1636)`.

### Task 4: Wiring

**Files:**

- Modify: `apps/shell/components/pharmacy/catalogue-panel.tsx`:
  - add the props `canDeclareSchedule` and `canReviewSchedule`, with a doc comment like the category one;
  - pass them through `CataloguePanel` and then `OpenProduct`;
  - mount the section before `ProductTaxSection`, with `onStateChange={pane.formState("schedule")}` and `onReviewStateChange={pane.formState("schedule-review")}`.
- Modify: `apps/shell/components/pharmacy/pharmacy-workspace.tsx` near lines 485-487: `canDeclareSchedule={access.data?.level === "editor" && can("pharmacy.schedule.declare")}`, and the same for review.
- Modify: `catalogue-panel.tsx`, the `ProductForm` note at about line 547. It says drug schedules "require governed review". Point it at the Schedule class section below.
- Test: `apps/shell/components/pharmacy/pharmacy-workspace.test.tsx`

- [ ] **Step 1: Failing tests.**
  - `it("offers schedule declaration and review to an editor with both grants")`: open a product as an editor holding both grants. Expect "Declare schedule class".
  - `it("shows a reader the schedule class without forms")`: as a reader, expect the "Schedule class" heading and no form.
- [ ] **Step 2:** The tests fail.
- [ ] **Step 3:** Wire it.
- [ ] **Step 4:** Run `pnpm --filter @helivanta/shell test pharmacy` and lint and type-check. They pass.
- [ ] **Step 5:** Commit `feat(pharmacy): open the schedule class section from the catalogue (#1636)`.

### Task 5: Browser journey and docs

**Files:**

- Create: `e2e/tests/pharmacy-schedule-class.spec.ts`
- Modify: `docs/api/domains/pharmacy.md`, the "Declared schedule class (#1386 A2)" section. Add one line: the catalogue's open product declares and reviews it (#1636).
- Modify: `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md`:
  - G-4 is met by this PR, and F-1's action is "Done (#1636)".
  - The §7.2 heading drops "(API until G-4)".
  - CAT-11, 13, 14 and 16 become screen steps using the copy in Global Constraints. CAT-12 becomes the inline errors. The API refusals stay in the Expected column as what the screen shows.

- [ ] **Step 1: Write the spec** `test("a pharmacist declares a schedule class and a second person approves it in the catalogue")`.
  - Use `readyOutlet(page, browser)` (or `readyCounter`), which gives `admin`, `cashier`, `outlet`, `product` and `sku`.
  - As admin, open the catalogue product for `sku`. The route is in `pharmacy-workspace.tsx`.
  - Declare H, No, source "TEST pack label". Expect "No schedule class in force: the latest declaration awaits review by a second person".
  - As admin, click Approve with a reason. Expect the four-eyes message.
  - As cashier, open the same product and approve with "TEST pack checked". Expect "Schedule H · not NDPS, approved".
  - Clean up the context in `finally`, as `pharmacy-payment-return.spec.ts` does.
- [ ] **Step 2:** Run it locally against the dev stack (`make dev-infra`, seeded; see memory `iam-integration-tests-need-seeded-dev-stack`). It must pass. If the stack can't run locally, record that, and rely on the e2e CI job before merge.
- [ ] **Step 3:** Edit the docs. Run `pnpm exec prettier --write` on both.
- [ ] **Step 4: Run the full gate.**
  - `pnpm turbo lint type-check test build format:check --continue --concurrency=4` (30/30).
  - `make lint-shell`.
  - In `backend/`: `go run ./cmd/contractdocs -check`. It should be clean, because nothing in the backend changed.
- [ ] **Step 5:** Commit `test(pharmacy): browser journey for declaring and reviewing a schedule class (#1636)`.
