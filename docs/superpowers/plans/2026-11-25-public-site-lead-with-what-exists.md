# Public site: lead with what exists Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/` shows only what a hospital can use today and ends with one clear action. Chapter 5, the full platform grid and the trust placeholders move unchanged to a new public `/roadmap`, which is linked from the nav, the platform section and a links row above the footer.

**Architecture:**

- The parts both pages share are pulled out of `apps/shell/app/page.tsx` into `components/landing/`: the nav, the links row, the platform grid, the trust section and the Anek font module.
- `/roadmap` is a route at `app/roadmap/page.tsx` that reuses them.
- The platform grid takes a filter: on `/` it shows only available modules.

**Tech Stack:** Next 16 app router, React, Vitest with `renderWithProviders`, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-24-public-site-lead-with-what-exists-design.md` (#1837). D2 (screenshots) is #1838 and not in this plan. D7 shipped in #1835.

## Global Constraints

- **No badge text on `/`:** no element whose text is exactly `Planned` or `Future` renders on `/` (spec anti-goal).
- **No copy rewrites:** text is only re-ordered or trimmed. New text is limited to the strings the spec names:
  - the roadmap link line "The full platform and its status, module by module.";
  - the nav and footer links "Roadmap", "Trust" and "Emergency help";
  - the roadmap page title and description.
- **Content inventory:** no string disappears from the public site. The inventory renders `/` and `/roadmap` and asserts their union (D9). Only the reviews strings and the chapter bar's second group leave, and they are removed by name.
- **No dashes on `/`:** nothing on `/` is dashed (D6). Tokens only (`token-source.test.ts`).
- **Public route:** `/roadmap` is in the middleware's public allowlist and in `hmsPublicPages`.

## Review Focus

1. **Chapter 2 on `/`.** The patient journey badges each station from the catalogue, and some are `Planned`. Expected: on `/` such a station shows an "On the roadmap" link instead of a badge, which keeps it honest without a badge. Pinned in Task 1.
2. **A catalogue group with no available modules.** On `/` it must not render an empty heading. Pinned in Task 1 by the filter test.
3. **Nav anchors from `/roadmap`.** "How it works" and the other anchors must go to `/#how` and so on, not to `#how` on the roadmap page. Pinned in Task 2.
4. **A signed-out visit to `/roadmap`.** It must return 200 and not redirect to `/login`. Pinned in Task 3 (middleware test and e2e).
5. **Keyboard users at the close.** The single action is reachable and labelled the same as the hero's. Pinned in Task 2 by role and name.

---

### Task 1: Shared pieces and the catalogue filter

**Files:**

- Create:
  - `components/landing/platform-grid.tsx` (`PlatformGrid({ scope }: { scope: "available" | "all" })`);
  - `components/landing/trust-section.tsx` (`TrustSection()`);
  - `components/landing/public-chrome.tsx` (`PublicNav({ home }: { home: boolean })`, `PublicLinks()`);
  - `app/anek.ts` (`export const anek = localFont(...)`).
- Modify:
  - `app/platform-catalog.ts`: add `isAvailable(status)` and `availableGroups`;
  - `components/landing/patient-journey.tsx`: add `roadmapLinks?: boolean`.
- Test:
  - `app/platform-catalog.test.ts` (new);
  - `components/landing/patient-journey.test.tsx`.

- [ ] Write the failing tests:
  - `availableGroups` contains no module whose status is `Planned` or `Future`;
  - every group in it has at least one module;
  - `Patient management` is included;
  - `Inventory & procurement` is excluded.
- [ ] Write the failing patient-journey test: with `roadmapLinks`, station 7 (billing) has no `Planned` text and has a link named "On the roadmap" to `/roadmap#platform`. Without the prop it shows `Planned` as today.
- [ ] Run them. Expected: FAIL.
- [ ] Implement, moving JSX out of `page.tsx` unchanged.
- [ ] Run the shell vitest. Expected: green.
- [ ] Commit.

### Task 2: The home page and the roadmap page

**Files:**

- Modify: `app/page.tsx`, `app/landing.test.tsx`.
- Create: `app/roadmap/page.tsx`, `app/roadmap/roadmap.test.tsx`.

- [ ] Write the failing tests on `/`:
  - nothing with exact text `Planned` or `Future`;
  - no `#trust` or `#reviews` section;
  - the chapter bar has one group;
  - the nav has a "Roadmap" link to `/roadmap`;
  - the platform section has a link to `/roadmap`;
  - the last section before the footer is the close, with one "Start your workspace" link and no `.border-dashed` anywhere;
  - the "Today:" reasons have no `border-l-2`.
- [ ] Write the failing tests on `/roadmap`:
  - chapter 5's maintenance and AI regions;
  - the full grid, with `Inventory & procurement` showing `Planned`;
  - `StatusGuide`;
  - the trust placeholders;
  - nav anchors start with `/#`;
  - `metadata.title` contains "Roadmap".
- [ ] Run them. Expected: FAIL.
- [ ] Implement:
  - `page.tsx` composes the hero, chapters 1 to 4 with `PatientJourney roadmapLinks`, why, sectors, `PlatformGrid scope="available"` plus the roadmap line, the close, `PublicLinks` and `TesserixFooter`;
  - the roadmap page composes `PublicNav home={false}`, a heading, the upcoming `StoryAct` and chapter 5 (moved verbatim), `PlatformGrid scope="all"` with the intro paragraph and `StatusGuide`, `TrustSection`, `PublicLinks` and `TesserixFooter`.
  - Move the maintenance, AI and full-grid assertions from `landing.test.tsx` to `roadmap.test.tsx`.
- [ ] Run the shell vitest, lint and tsc. Expected: green.
- [ ] Commit.

### Task 3: Public route, inventory and docs

**Files:**

- Modify:
  - `middleware.ts` and `middleware.test.ts` (`/roadmap` exact);
  - `eslint.config.mjs` (`app/roadmap/**`);
  - `app/landing-content.test.tsx` and its fixture (the union of both pages);
  - `docs/superpowers/specs/2026-10-28-home-story-chapters.md` (a D6 note).
- Create: `e2e/tests/roadmap.spec.ts` (signed out, `/roadmap` returns 200 and shows "What comes next"; the home nav's Roadmap link navigates there).

- [ ] Write the failing middleware test (`/roadmap` returns 200 and `/roadmap/x` returns 307). Run it and expect FAIL. Implement, then run and expect PASS.
- [ ] Inventory: render both pages and collect the union. Remove only the reviews strings and the chapter bar's second-group strings from the fixture, by name. Expected: PASS.
- [ ] Spec note. Run the shell vitest, lint, tsc and prettier. Push, and expect CI green.
- [ ] Commit.
