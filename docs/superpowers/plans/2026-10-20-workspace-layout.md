# Workspace layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render `HmsShell` once from an `app/(workspace)/layout.tsx` route group so the chrome survives client-side navigation, navigation stops sending session-activity requests, and the tenant switcher appears on every authenticated page.

**Architecture:** Every authenticated segment moves, URL-unchanged, under `apps/shell/app/(workspace)/`. Its server layout renders `<HmsShell tenantPicker={<TenantPicker />}>`. `HmsShell` drops its `active` prop and reads `usePathname()`, resolving the current panel link with a new `activePage()` in `zones.ts`. A lint rule bans `HmsShell` imports outside that layout.

**Tech Stack:** Next.js App Router (route groups), React 19, `@helivanta/ui`, Vitest + `renderWithProviders`, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-20-workspace-layout-design.md` (issues #1091, #1092; branch `feat/1091-workspace-layout`).

## Global Constraints

- No URL changes. `navigation.spec.ts` and `smoke.spec.ts` must pass unmodified, apart from the additions in Task 1.
- Public routes stay outside the group: `app/page.tsx`, `login`, `signup`, `logout`, `ambulance`, `api`.
- `apps/shell/middleware.ts` is not modified.
- The activity rate limit (`RATE_LIMIT_ACTIVITY_PER_MIN`, `Tight` rules) is not modified.
- No hardcoded colours. Design tokens only (frontend standards §7).
- Frontend gates use the `.nvmrc` Node (v24.21.0) plus the CI `NEXT_PUBLIC_*` env, per the repo memory.
- Commits carry no AI attribution.

## Review Focus

1. **Deep or unknown paths under a zone** (`/emergency/bases/b/cases/c`, `/workforce/setup`, a typo URL that 404s inside a zone). Expected: the longest matching panel link is current, and with no match nothing is current. Pinned by Task 2's `activePage` tests and Task 3's shell test.
2. **A page suspending or throwing after the move.** Expected: the fallback renders inside the chrome and the rail stays mounted. Pinned by Task 4's e2e step.
3. **The idle warning open while the user navigates.** Expected: the countdown keeps going and is not reset. Pinned by Task 3's shell test "keeps the idle warning across a pathname change".
4. **A user with one tenant.** Expected: no switcher anywhere (TenantPicker returns null below two memberships). This is existing behaviour, and Task 1's switcher test asserts the positive case for `specAdmin` only. It is called out so nobody "fixes" the null return.
5. **Hard load with a collapsed panel.** Expected: no visible open-then-close. Settled by Task 5's measurement.

---

### Task 1: Failing e2e controls (red on `main`)

**Files:**

- Modify: `e2e/tests/navigation.spec.ts` (append tests)

**Interfaces:**

- Consumes: `login(page, user?)` and `specAdmin()` from `e2e/tests/support/login.ts`; the rail's `navigation` landmark named `Zones`.
- Produces: three tests that Task 3 turns green.

- [ ] **Step 1: Write `test("the chrome is not remounted when moving between departments")`.**
      After `login(page)`, go to `/medicore/opd` through the rail. Tag the Zones nav DOM node with `page.getByRole("navigation", { name: "Zones" }).evaluate((el) => { (el as HTMLElement & { __chromeMarker?: string }).__chromeMarker = "same-node"; })`. Then click through every rail link (read their names from the nav at runtime, so a new zone is covered automatically) and back to MediCore. After each click, assert the URL changed and the nav node's `__chromeMarker` is `"same-node"`, with the failure message `` `moving to ${name} remounted the chrome` ``.
- [ ] **Step 2: Write `test("moving between departments sends no session-activity requests")`.**
      After `login(page)` and landing on `/medicore/opd`, wait for one `POST` response whose URL ends `/auth/session/activity` (the mount seed). Then register a `page.on("request")` counter for `POST` + that URL suffix. Do four rail hops (PharmaConnect, Lab, MediCore, PharmaConnect), each waiting for its URL, then wait 1 s and `expect(count).toBe(0)`. Do not move the mouse or scroll between hops; Playwright's `click` is the only interaction.
- [ ] **Step 3: Write `test("the tenant switcher is on every department page")`.**
      `login(page, specAdmin())`, then for each of `/medicore/opd`, `/pharmacy`, `/lab`: `page.goto(path)`, then `await expect(page.getByRole("combobox", { name: "Tenant" }), path).toBeVisible()`.
- [ ] **Step 4: Run against the local stack on `main`'s code.**
      Run: `make dev-infra && make seed`, start the API and shell as `e2e/playwright.config.ts` expects, then `pnpm --filter @helivanta/e2e exec playwright test tests/navigation.spec.ts --project=specs`.
      Expected: the three new tests FAIL: "moving to … remounted the chrome", count `> 0`, and the combobox not visible on `/medicore/opd`. The two existing tests PASS. Paste the failure lines into the PR body later.
- [ ] **Step 5: Commit.**
      `git add e2e/tests/navigation.spec.ts && git commit -m "test(e2e): chrome, activity and tenant switcher must survive navigation"`

### Task 2: `activePage()` in zones

**Files:**

- Modify: `packages/ui/src/zones.ts` (next to `activeZone`, ~line 297)
- Modify: `packages/ui/src/index.ts` (add `activePage` to the existing `./zones` export on line 2)
- Test: `packages/ui/src/zones.test.ts`

**Interfaces:**

- Produces: `export function activePage(zone: Zone, path: string): ZonePage | undefined`

- [ ] **Step 1: Write `describe("activePage")` with these cases** (build zones from `ZONES` by key):
  - `activePage(pharmacy, "/pharmacy/medications")?.href === "/pharmacy/medications"` (exact match)
  - `activePage(emergency, "/emergency/bases/b/cases/c")?.href === "/emergency"` (nested)
  - `activePage(workforce, "/workforce/setup")?.href === "/workforce/setup"` (longest prefix beats `/workforce`)
  - `activePage(pharmacy, "/pharmacyx")` is `undefined` (segment boundary)
  - `activePage(pharmacy, "/lab")` is `undefined` (no match)
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test -- zones`. Expected: FAIL, `activePage` is not exported.
- [ ] **Step 3: Implement.** Filter `zone.pages` to those where `path === href || path.startsWith(href + "/")`, and return the one with the longest `href`.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(ui): resolve the current panel page from a path`.

### Task 3: The workspace layout owns `HmsShell`

This is one task because removing `active` makes every remaining call site a type error. The move, the prop change and the lint rule land together.

**Files:**

- Modify: `packages/ui/src/hms-shell.tsx` (props ~108–131, zone/page resolution ~137–139 and ~413, `aria-current` checks ~531, `tenantPicker` doc comment ~99–107)
- Modify: `packages/ui/src/hms-shell.test.tsx`
- Create: `apps/shell/app/(workspace)/layout.tsx`
- Move (`git mv`, path otherwise identical): `apps/shell/app/{dashboard,emergency,lab,medicore,patients,pharmacy,settings,supply,workforce}` → `apps/shell/app/(workspace)/…`
- Modify: every file that renders `<HmsShell` today. Get the list with `git grep -l "<HmsShell" -- 'apps/shell/**/*.tsx'` (33 sites, including the workforce `*-workspace.tsx` components and `emergency/bases/[base]/cases/[case]/page.tsx`).
- Modify: `apps/shell/eslint.config.mjs`
- Modify: any test under `apps/shell` that imports a moved file by relative path or `@/app/...` (find with `git grep -n "app/\(dashboard\|emergency\|lab\|medicore\|patients\|pharmacy\|settings\|supply\|workforce\)" -- apps/shell e2e`)

**Interfaces:**

- Consumes: `activePage(zone, path)` from Task 2.
- Produces: `HmsShell({ tenantPicker?, onSignOut?, children })` with no `active` prop. `WorkspaceLayout` is the sole importer of `HmsShell` in `apps/shell`.

- [ ] **Step 1: Update `hms-shell.test.tsx` first.** Add `const pathname = vi.hoisted(() => ({ current: "/dashboard" }))` and `vi.mock("next/navigation", () => ({ usePathname: () => pathname.current }))`. Replace every `active="X"` with setting `pathname.current = "X"` before render, and keep every existing assertion. Add:
  - `it("marks the longest matching panel link current on a nested path")`: with `pathname.current = "/emergency/bases/b/cases/c"` and emergency permissions seeded, the panel link "Operating bases & reports" (`/emergency`) has `aria-current="page"`, and no other panel link does.
  - `it("keeps the idle warning across a pathname change")`: open the warning by driving `createdTrackers[0].noteDeadline(...)` the way the existing warning tests do. Then change `pathname.current` and `rerender`, and assert the warning dialog is still present and `createdTrackers.length` is still 1.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/ui test -- hms-shell`. Expected: type-check or test FAIL, because `active` is still required and `usePathname` is unused.
- [ ] **Step 3: Change `HmsShell`.** Remove `active` from the props type. `const pathname = usePathname()` (from `next/navigation`). Zone is `activeZone(pathname)` with the existing fallback. `const current = activePage(zone, pathname)`, and the panel `isActive` is `pageLink.href === current?.href`. Replace the `tenantPicker` doc comment's last two sentences with: the workspace layout always passes the picker, which renders nothing for fewer than two memberships.
- [ ] **Step 4: Run** the step 2 command. Expected: PASS.
- [ ] **Step 5: Create the route group.** `git mv` the nine segments into `apps/shell/app/(workspace)/`. Write `layout.tsx` as a server component (no `"use client"`):
  ```tsx
  import { HmsShell } from "@helivanta/ui";
  import { TenantPicker } from "@/components/tenant-picker";

  export default function WorkspaceLayout({
    children,
  }: {
    children: React.ReactNode;
  }) {
    return <HmsShell tenantPicker={<TenantPicker />}>{children}</HmsShell>;
  }
  ```
  In every listed call site, delete the `<HmsShell …>` wrapper (keep its children), the `HmsShell` import and a now-unused `TenantPicker` import. Where removing the wrapper leaves several siblings, return a fragment.
- [ ] **Step 6: Add the lint rule** to `apps/shell/eslint.config.mjs`. Export `[...hmsEslint(import.meta.dirname), { files: ["**/*.{ts,tsx}"], ignores: ["app/(workspace)/layout.tsx", "**/*.test.tsx"], rules: { "no-restricted-imports": ["error", { paths: [{ name: "@helivanta/ui", importNames: ["HmsShell"], message: "The chrome is rendered once by app/(workspace)/layout.tsx; pages render content only (#1091)." }] }] } }]`. If `hmsEslint` already sets `no-restricted-imports` for these files, merge its existing `paths`/`patterns` into this entry rather than replacing them.
- [ ] **Step 7: Prove the lint rule can fail.** Temporarily add `import { HmsShell } from "@helivanta/ui";` to `app/(workspace)/lab/page.tsx` and run `pnpm --filter @helivanta/shell lint`. Expected: the restricted-import error with the message above. Revert the line.
- [ ] **Step 8: Run the shell gates.** `pnpm turbo lint type-check test build --filter=@helivanta/shell --filter=@helivanta/ui`. Expected: all green, and `git grep -n "<HmsShell" -- apps/shell` lists only `app/(workspace)/layout.tsx`.
- [ ] **Step 9: Run Task 1's e2e tests** (same command as Task 1 step 4, plus `tests/smoke.spec.ts tests/journey.spec.ts tests/tenant-switch.spec.ts`). Expected: all PASS, including the three that failed in Task 1.
- [ ] **Step 10: Commit** `feat(shell): render the chrome once from a workspace layout`, with body `Closes #1091` and `Closes #1092` on separate lines.

### Task 4: Loading and error boundaries inside the chrome

**Files:**

- Create: `apps/shell/app/(workspace)/loading.tsx`, `apps/shell/app/(workspace)/error.tsx`
- Modify: `e2e/tests/navigation.spec.ts`

**Interfaces:**

- Consumes: the root `apps/shell/app/loading.tsx` / `error.tsx` as the pattern. Copy their content component, minus any full-screen wrapper, so it sits in the content area.

- [ ] **Step 1: Write `test("a page that is still loading keeps the chrome")`.** Tag the Zones nav as in Task 1. Delay `**/api/v1/pharmacy/**` responses by 1.5 s with `page.route`, click PharmaConnect, assert the marker is still `"same-node"` while the route is pending, then unroute.
- [ ] **Step 2: Run it.** Expected: it passes or fails depending on whether the page suspends. Record the result. If it already passes, the step still stands as a regression control.
- [ ] **Step 3: Create both files.** `error.tsx` is `"use client"` and takes `{ error, reset }`, as the root one does.
- [ ] **Step 4: Run** the navigation spec and `pnpm turbo build --filter=@helivanta/shell`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(shell): keep loading and error states inside the chrome`.

### Task 5: Panel flash on hard load (measure, then fix only if visible)

**Files (only if a fix is needed):**

- Modify: `packages/ui/src/theme.tsx` (add `CHROME_INIT_SCRIPT` beside `THEME_INIT_SCRIPT`), `packages/ui/src/index.ts`, `packages/ui/src/hms-shell.tsx` (export the two storage keys it uses), `packages/ui/styles.css`, `apps/shell/app/layout.tsx`
- Test: `e2e/tests/navigation.spec.ts`

- [ ] **Step 1: Measure.** `pnpm --filter @helivanta/shell build && pnpm --filter @helivanta/shell start` against the dev stack. Sign in, collapse the page panel, then hard-reload `/pharmacy` under Playwright with `page.addInitScript` installing a `MutationObserver` that records every `aria-hidden` value on the page-panel `<aside>` from first paint. Repeat with the rail expanded (`hms.navigation.open = "1"`).
- [ ] **Step 2: Decide.** If the recorded values never include an open panel (or a collapsed rail, in the second case), write the measurement into the PR body under "Panel flash", make no code change, and skip to Task 6.
- [ ] **Step 3 (only if visible): Write `test("a collapsed panel is never painted open on a hard load")`** using the step 1 observer. Expected now: FAIL.
- [ ] **Step 4 (only if visible): Implement.** `CHROME_INIT_SCRIPT` is a static string, like `THEME_INIT_SCRIPT`, that sets `document.documentElement.dataset.hmsPanel` and `dataset.hmsNavigation` to `"open"`/`"closed"` from `localStorage` keys `hms.panel.open` / `hms.navigation.open` (`"1"` means open; missing means the defaults, panel open and navigation closed). Wrap it in try/catch. Run it in the root layout next to the theme script, with the same eslint-disable and comment. In `styles.css`, before React hydrates, size the panel and rail from those attributes. Read the pitfall comments at the top of `styles.css` first. Run the test. Expected: PASS.
- [ ] **Step 5: Commit** `fix(ui): no panel flash on a hard load`, or nothing if step 2 skipped.

### Task 6: Correct the standards and the #933 spec

**Files:**

- Modify: `docs/standards/frontend.md` §2 (lines ~59–68), §6 (lines ~270–287)
- Modify: `docs/superpowers/specs/2026-10-13-one-next-app-design.md` (~line 94)

- [ ] **Step 1: Edit §2.** The example becomes: the workspace layout renders `HmsShell`, and `app/(workspace)/pharmacy/page.tsx` renders `<DispenseList />`.
- [ ] **Step 2: Edit §6.** Replace the `active` paragraph: `HmsShell` reads the path with `usePathname()`. The current zone comes from `activeZone()` and the current page from `activePage()` (the longest matching panel `href`, with nothing current when none matches). Authenticated routes live under `app/(workspace)/`, whose layout is the only file allowed to import `HmsShell` (lint-enforced). A new authenticated route placed outside the group gets no chrome and no idle tracking. In "Idle session tracking", change "Every page renders `HmsShell`" to "the workspace layout renders `HmsShell` once".
- [ ] **Step 3: Edit the #933 spec's "does not cover" bullet.** Append "Done in #1091; see `2026-10-20-workspace-layout-design.md`."
- [ ] **Step 4: Run** `pnpm turbo format:check`. Expected: green for the edited files.
- [ ] **Step 5: Commit** `docs(frontend): the workspace layout owns the chrome`.

### Task 7: Full gates and PR

- [ ] **Step 1: Run** `pnpm turbo lint type-check test build format:check` and `make lint-shell`. Expected: all green.
- [ ] **Step 2: Push and open the PR.** The body contains: what changed; the Task 1 red→green evidence; the Task 5 measurement; "Not covered: #1211 tenant names; `activeZone`'s missing segment boundary"; and `Closes #1091` and `Closes #1092`. No attribution lines.
- [ ] **Step 3: After CI's e2e job, check its API log** (`/tmp/hms-stack.log` as dumped by `.github/workflows/ci.yml`) for `rate limited` on `POST /v1/auth/session/activity`. Expected: none on ordinary navigation. Record the result in the PR.
