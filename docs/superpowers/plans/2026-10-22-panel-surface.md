# Panel Surface Implementation Plan

**Goal:** One card surface (`Panel`, `PanelLink`), insets instead of nested cards, and a lint rule with a shrink-only legacy list.

**Spec:** `docs/superpowers/specs/2026-10-22-panel-surface-design.md` (#1275)

## Task 1: Panel and PanelLink

- `packages/ui/src/panel.tsx`:
  - `Panel({ as?, flush?, className?, children, ...HTMLAttributes })` and `PanelLink({ href, className?, children, ...anchor props })`;
  - a `PanelDepth` context makes a nested one an inset (spec D2).
- Export both from `index.ts`.
- `packages/ui/src/panel.test.tsx` covers:
  - the top-level card has `bg-card border shadow-xs` and padding;
  - `flush` has no padding and has `overflow-hidden`;
  - nested → `bg-muted`, with no `border` and no `bg-card`;
  - `as="li"` renders an `li`;
  - a `PanelLink` inside a `Panel` is an inset.
- `RequestFailure` renders a `Panel`. QueryState's failure wrapper `div.p-5` goes, because the panel is the padding.

## Task 2: Lint rule and ratchet

- `packages/config/eslint.config.mjs`:
  - `SURFACE` patterns (spec D3) in `WORKSPACE_SYNTAX`;
  - export `SURFACE_PATTERNS` (RegExp[]) for the ratchet test;
  - `hmsWorkspacePages(files, ignores, legacy = [])` returns two blocks, so that legacy files get the workspace rules without `SURFACE`.
- `eslint.config.test.mjs` covers:
  - rejection of `bg-card`, `rounded-2xl` and `border rounded-lg` (in both orders, and in a template);
  - acceptance of `rounded-md border`, `rounded-full border`, `border-b rounded-lg` and `rounded-lg bg-muted`.
- `apps/shell/surface-legacy.mjs` holds the file list, and `apps/shell/surface-legacy.test.mjs` checks that every listed file still matches a pattern.

## Task 3: Convert the D5 zones

- Replace the hand-painted surfaces with `Panel`/`PanelLink` in the D5 files, and drop the decorative icon tiles that D3 bans where they sit on a tile.
- Panel tests stay green, and `e2e/tests/smoke.spec.ts` selectors are unchanged.

## Verify

Run `pnpm turbo lint type-check test build format:check`. Then, on a production build, run the e2e smoke, navigation and csp specs, and visually check the Dashboard, Patients and Country policies.
