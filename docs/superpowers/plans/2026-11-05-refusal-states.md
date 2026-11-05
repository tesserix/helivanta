# Refusal States Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every screen shows a 403/404 as a refusal and a 401 as an ended session, never as an outage with **Try again**, and a new screen cannot forget to.

**Architecture:** `RequestFailure` (`packages/ui`) decides the state from the request's error, which becomes a required prop. `QueryState` and `StaffDirectory` pass their query's error through. Every direct call site passes its query's error; a one-off codemod does the single-query sites and the rest are done by hand.

**Tech Stack:** React 19, TypeScript, React Query via `@helivanta/api`, Vitest + Testing Library, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-05-refusal-states-design.md` (#1591)

## Global Constraints

- Default refusal sentence: `This wasn't found, or you don't have access to it.`
- Session sentence: `Your session has ended. Sign in again to continue.`
- 403 and 404 → the caller's `refused`, else the default; no **Try again**.
- 401 → the session sentence, even when `refused` is given; no **Try again**.
- Anything else (5xx, 409, network, abort, `null`, non-`ApiError`) → `message` with **Try again**.
- `error` is a required prop typed `unknown`; there is no way to omit it.
- Screens that give a 404 their own meaning before reaching `RequestFailure` keep that branch.
- No backend change, no migration, no new dependency.
- Node per `.nvmrc` (v24.21.0) with the two `NEXT_PUBLIC_*` vars for the shell build.
- Gate: `pnpm turbo lint type-check test build format:check` green.

## Review Focus

1. **A paged list whose Load more is refused.** `isError` is set with rows on screen; the person expects the rows gone and the refusal shown, not "may be out of date" with a retry. Task 2 tests it.
2. **A multi-query panel where one query is refused and another fails with a 500.** The person expects the refusal, since retrying can't fix it. Hand-written sites use `a.error ?? b.error` in the order the queries render; Task 3 rules on which order and records it.
3. **The pharmacy report screens getting a 401.** They pass a `refused` sentence; the person expects to be told to sign in again, not that they lack access. Task 1 tests it.
4. **`error` is a plain object with a `status` field, not an `ApiError`.** It counts as a failure with a retry (fail closed: a retry discloses nothing). Task 1 tests it.
5. **A screen that already treats 404 as "not set up yet"** (GST, bed charging, daily collection, cash desk). The person expects its setup prompt, not the refusal. Task 3 checks one such screen's existing test still passes unchanged.

---

### Task 1: `RequestFailure` decides the state from a required error

**Files:**

- Modify: `packages/ui/src/request-failure.tsx`
- Modify: every non-test `.tsx` under `apps/` and `packages/` that renders `<RequestFailure` (59 files)
- Test: `packages/ui/src/request-failure.test.tsx`
- Modify: tests that asserted a 401 shows a `refused` sentence (`apps/shell/components/pharmacy/*.test.tsx`, if any)

**Interfaces:**

- Produces: `RequestFailure({ message: string; retry: () => unknown; error: unknown; refused?: string })`; exported `refusedDefault` and `sessionEnded` string constants from `packages/ui/src/request-failure.tsx`, re-exported from `@helivanta/ui`.

- [ ] **Step 1: Write the failing tests** in `request-failure.test.tsx`, replacing the #1589 cases:
  - `it.each([403, 404])("shows a %i as the caller's refusal with no retry")`: alert has `refused`; no "Could not load."; no **Try again**.
  - `it.each([403, 404])("shows a %i without a sentence as the default refusal")`: alert text is `This wasn't found, or you don't have access to it.`; no **Try again**.
  - `it("shows a 401 as an ended session, even with a refusal sentence")`: alert text is `Your session has ended. Sign in again to continue.`; neither `refused` nor `message`; no **Try again**.
  - `it.each([["a server error", new ApiError("internal","boom",500)], ["a conflict", new ApiError("conflict","x",409)], ["a network failure", new TypeError("Failed to fetch")], ["no error", null], ["a status-shaped object", { status: 403 }]])("offers a retry for %s")`: alert has `message`; clicking **Try again** calls `retry` once.
  - A compile fixture at the end of the file:
    ```tsx
    // The structural control (#1591): a call site that passes no error must not compile.
    // @ts-expect-error error is required
    void (<RequestFailure message="x" retry={() => {}} />);
    ```

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui test -- request-failure && pnpm --filter @helivanta/ui type-check`
Expected: the default-refusal and 401 tests FAIL; type-check FAILS with "Unused '@ts-expect-error' directive".

- [ ] **Step 3: Implement** in `request-failure.tsx`: `error: unknown` required; branch on `error instanceof ApiError` → 401 session sentence; 403/404 → `refused ?? refusedDefault`; otherwise the message with the retry. Keep the fail-closed comment, extended to say why an unreadable status keeps the retry.

- [ ] **Step 4: Codemod the single-query call sites.** Write a throwaway script in the session scratchpad (not committed) that, for each `<RequestFailure` element without `error=`, reads its `retry` expression and inserts `error={X.error}` after it when the expression is one of `X.retry`, `X.refetch`, `() => void X.refetch()`, `() => X.refetch()` (`X` a dotted identifier).

Run: `pnpm --filter @helivanta/shell type-check 2>&1 | grep -c "Property 'error' is missing"`
Expected: about 12 — the multi-query, `onReload` and async sites the codemod left. Their file:line list is the input to Step 5.

- [ ] **Step 5: Hand-write the remaining sites.** Multi-query: `error={a.error ?? b.error}` in the order the queries render. `onReload` / async: pass the error of the query the callback refetches; if the component receives only a callback, add an `error: unknown` prop alongside it and pass it from the caller.

- [ ] **Step 6: Run the gate on the touched packages**

Run: `pnpm turbo lint type-check test --filter=@helivanta/ui --filter=@helivanta/shell`
Expected: all green. Tests that asserted a 401 shows a screen's `refused` sentence fail; change their status to 403 (the 401 case is now owned by `request-failure.test.tsx`) and re-run to green.

- [ ] **Step 7: Mutation check.** Make `error` optional (`error?: unknown`), run `pnpm --filter @helivanta/ui type-check`; expected FAIL on the `@ts-expect-error` line. Restore.

- [ ] **Step 8: Commit**

```bash
git add packages/ui apps/shell
git commit -m "feat(ui): show refusals and ended sessions from every request failure (#1591)"
```

### Task 2: `QueryState` and `StaffDirectory` pass the error through

**Files:**

- Modify: `packages/ui/src/query-state.tsx`, `packages/ui/src/staff-directory.tsx`
- Modify: `apps/shell/app/(workspace)/workforce/page.tsx:348`, `apps/shell/components/pharmacy/staff-source.tsx:195`
- Test: `packages/ui/src/query-state.test.tsx`, `packages/ui/src/staff-directory.test.tsx` (create if absent)

**Interfaces:**

- Consumes: `RequestFailure` from Task 1.
- Produces: `QueryState`'s `query: Pick<UseApiPagedQueryResult<T>, "items" | "isPending" | "isError" | "error" | "retry">`; `StaffDirectoryProps.error: unknown` (required).

- [ ] **Step 1: Write the failing tests** in `query-state.test.tsx` (the `renderState` helper gains `error?: unknown`, default `null`):
  - `it("shows a refused first load as a refusal with no retry")`: `{items: [], isError: true, error: new ApiError("forbidden","no",403)}` → alert is the default refusal; no **Try again**; no empty state.
  - `it("hides the rows when a refresh is refused")`: `{items: [visit], isError: true, error: new ApiError("not_found","no",404)}` → the default refusal; `queryByText("Asha Rao")` is null; no "may be out of date".
  - `it("hides the rows when a refresh finds the session ended")`: same with 401 → the session sentence; no rows.
  - The existing "keeps the loaded rows under a warning" case passes `error: new ApiError("internal","boom",500)` and still passes.
  - In `staff-directory.test.tsx`: `it("shows a refused directory as a refusal with no retry")` rendering `StaffDirectory` with `isError: true, error: 403 ApiError` → default refusal, no **Try again**.

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui test -- query-state staff-directory`
Expected: the refusal cases FAIL (rows still shown / "Could not load visits." with a retry).

- [ ] **Step 3: Implement.** `QueryState`: pass `error={query.error}` to `RequestFailure`; when `isError` and the error is a 401/403/404 `ApiError`, render the `RequestFailure` branch whatever `items.length` is. Export a predicate `isRefusal(error: unknown): boolean` from `request-failure.tsx` and use it in both components so the status set lives in one place. Update the doc comment: a failed refresh keeps rows under a warning, a refused one hides them, and why (rows the server now refuses aren't left on screen as if current). `StaffDirectory`: required `error: unknown`, passed into the query; its two callers pass `query.error`.

- [ ] **Step 4: Run to verify they pass**

Run: `pnpm turbo type-check test --filter=@helivanta/ui --filter=@helivanta/shell`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add packages/ui apps/shell
git commit -m "feat(ui): hide rows the server refuses on refresh (#1591)"
```

### Task 3: One screen per product area shows a refusal

**Files (tests only, plus any fixture serve mode they need):**

- `apps/shell/components/medicore/admission-bills.test.tsx`
- `apps/shell/components/supply/licence-review.test.tsx`
- `apps/shell/components/organization/structure.test.tsx`
- `apps/shell/components/patients/pharmacy-lookup-access.test.tsx`
- `apps/shell/components/country-policy/workspace.test.tsx`
- `apps/shell/components/lab/order-list.test.tsx`
- `apps/shell/app/(workspace)/workforce/directory-editor.test.tsx`
- Pharmacy is covered by `sales-worklist.test.tsx` from #1589.

**Interfaces:**

- Consumes: Tasks 1 and 2.

- [ ] **Step 1: Write one test per file**, named `it("shows a 403 as a refusal with no retry")`: make the screen's first read answer 403 using the file's existing mock style; assert the alert shows the default refusal (or the screen's own `refused` sentence if it passes one) and that no **Try again** button exists.

- [ ] **Step 2: Prove each can fail.** Temporarily remove `error={…}` from that screen's call site (or, for `QueryState` screens, the `error` pass-through) and add `// @ts-expect-error` so it compiles; run the file; expected FAIL. Restore. Record each in the PR's validation section.

- [ ] **Step 3: Rule on multi-query order (Review Focus 2).** For each hand-written `a.error ?? b.error` site from Task 1 Step 5, check the order matches the queries' render order; record the ruling in the ledger.

- [ ] **Step 4: Check Review Focus 5.** Run `pnpm --filter @helivanta/shell test -- gst bed-charging daily-collection` with no edits; expected PASS: their "not set up" 404 branches are untouched.

- [ ] **Step 5: Run and commit**

Run: `pnpm --filter @helivanta/shell test`
Expected: green.

```bash
git add apps/shell
git commit -m "test(shell): a refusal in each product area shows no retry (#1591)"
```

### Task 4: The three #1589 minors and the docs

**Files:**

- Modify: `e2e/tests/support/pharmacy.ts` (`newOutlet` returns `timeZone`), `e2e/tests/support/pharmacy-counter.ts` (`readyOutlet` returns it), `e2e/tests/pharmacy-reports.spec.ts:139,309`
- Modify: `apps/shell/components/pharmacy/daily-report.tsx:36` (section labelled by its heading), `apps/shell/components/pharmacy/daily-report.test.tsx:169-187`
- Modify: `docs/standards/frontend.md` (states section), `docs/api/domains/pharmacy.md` ("Report screens and the legacy reports (#1060)": 401 wording, the 176 sites no longer outstanding)

**Interfaces:**

- Produces: `newOutlet(...)` → `{ outlet, stamp, timeZone: string }`; `readyOutlet(...)` adds `timeZone`.

- [ ] **Step 1: Daily test first.** Change the refusal assertion to `within(screen.getByRole("region", { name: "Daily sales" })).getByRole("alert")` having the reports' refused sentence. Run `pnpm --filter @helivanta/shell test -- daily-report`; expected FAIL (no such region).

- [ ] **Step 2:** Give the daily section's `Panel` an `aria-labelledby` pointing at its `<h3>` (via `useId`). Re-run; expected PASS.

- [ ] **Step 3: e2e "today".** `newOutlet` returns the `timezone` it sends (`"Asia/Kolkata"`) as `timeZone`; both `today` computations in `pharmacy-reports.spec.ts` use the fixture's `timeZone`. Run `pnpm --filter @helivanta/e2e type-check`; expected green.

- [ ] **Step 4: Run the walkthrough** against the local API (8080), Global API (8181) and shell (4301):

Run: `pnpm --filter @helivanta/e2e exec playwright test pharmacy-reports --project=specs`
Expected: 2 passed. Its "access withdrawn" check still finds no **Try again**.

- [ ] **Step 5: Docs.** `frontend.md` states section: `RequestFailure` takes the query's `error`; 403/404 refuse, 401 asks to sign in again, others retry; `QueryState` hides rows on a refused refresh; screens with their own 404 meaning branch before it. `pharmacy.md`: replace the #1589 note that other screens still show refusals as failures, and state the 401 wording.

- [ ] **Step 6: Full gate and commit**

Run: `pnpm turbo lint type-check test build format:check`
Expected: all tasks green.

```bash
git add e2e apps/shell docs
git commit -m "test(pharmacy): outlet time zone, daily section's own alert; document refusal states (#1591)"
```
