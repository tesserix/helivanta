# Home page care board implementation plan

**Goal:** Implement [the home page care board design](../specs/2026-10-26-home-care-board-design.md) (#1354).

**Architecture:** `apps/shell/app/page.tsx` stays a Server Component and renders client boards from `apps/shell/components/landing/`. Board data lives in `*-data.ts` files typed against the catalogue's module names. One `useStoryPlayer` hook and one `PlayToggle` button drive every board's step timer, pause and reduced-motion fallback.

- [x] Self-host Anek Latin through `next/font/local` with its licence.
- [x] Add the night palette tokens to `apps/shell/app/globals.css`; remove landing-only `--site-*` tokens the redesign leaves unused.
- [x] Derive a module-name type from `platform-catalog.ts`.
- [x] Tests first: story data integrity (nodes exist, statuses resolve), board interactions (story selection, narration, pause, reduced motion), page content (headline, calls to action, honest statuses, trust placeholders, emergency link).
- [x] Build `care-board-data.ts`, `care-board.tsx` and the new `page.tsx`.
- [x] Run the shell locally and review screenshots at desktop and mobile widths, with and without reduced motion.
- [x] Gate on Node 24: `pnpm turbo lint type-check test build format:check`.

## Second pass: patient journey, audit trail, AI, platform

- [x] Tests first for the shared player: icon-only pause button named "Pause animation" and "Play animation", no text pill, absent under reduced motion.
- [x] Extract `useStoryPlayer` and `PlayToggle` from `care-board.tsx`; the care board keeps its existing tests green.
- [x] Correct the pharmacy catalogue description and the ward-indent story note: batches, quarantine, stock register, payables and settlements exist today.
- [x] Tests first, then `patient-journey-data.ts` and `patient-journey.tsx`: eight stations in order, each resolving a catalogue status, narration marks the current station, record entries accumulate.
- [x] Tests first, then `audit-trail-data.ts` and `audit-trail.tsx`: stock and money lanes, the correction keeps the original entry, the auditor step is read-only.
- [x] Tests first, then `ai-board-data.ts` and `ai-board.tsx`: five scenarios, every scenario ends with a named person approving, every module shown is Planned or Future.
- [x] Platform section lists each catalogue group's modules with their statuses. The per-product tour is not in this slice.
- [x] Global framing in the hero and sectors section. The security and documents section is not in this slice.
- [x] Review locally at desktop and mobile widths, with and without reduced motion; check CSP on `/`.

## Third pass: stock, maintenance, card style, width

- [x] Tests first, then `stock-watch-data.ts` and `stock-watch.tsx`: findings derive from the numbers (D8), counts and routes agree with the shelf.
- [x] Extract the shared check flow (`check-flow.tsx`, `check-flow-model.ts`) used by the stock and maintenance boards.
- [x] Tests first, then `maintenance-watch-data.ts` and `maintenance-watch.tsx`: a failed result outranks every date, each finding has one route, a failed check has no agent.
- [x] Bezel cards (`board-card.tsx`) for every board; status pills reserve room for every label so boards keep their height mid-story.
- [x] Fluid page width (`page-width.ts`) shared by every section.
- [x] Move the previous landing page to `/workspace`; middleware and session renewal treat it as public.
- [x] Gate on Node 24: `pnpm turbo lint type-check test build format:check`.
