# HMS Light/Dark Theme — "Frost / Night" Implementation Plan

**Date:** 2026-08-11
**Status:** Approved direction — locked by Mahesh
**Approved mockup:** https://claude.ai/code/artifact/77cce949-2769-40c4-a9d1-ea71b1a0bad9 (merged two-rail, light+dark)
**Issue:** Refs #542 (Design tokens & theming — this is the web light/dark scheme slice only)
**Branch:** feat/542-design-tokens-theming

## Summary

Re-skin the HMS web consoles with a two-theme token system: light = "Clinical Frost"
(cool near-white, deep teal accent), dark = "Graphite Night" (slate, brighter teal).
The two-rail shell structure is unchanged. Theme follows the OS by default; a
header toggle overrides it, persisted in localStorage. Department dashboard cards
get per-zone tinted icon chips.

## Global Constraints (binding)

- **All token values must be opaque hex.** The theme pipeline drops alpha channels
  (see the NOTE comment at the top of `packages/ui/styles.css`) — an rgba() value
  renders full-strength. Every value in this plan is already opaque; use them verbatim.
- **Do not restructure the shell.** `packages/ui/src/hms-shell.tsx` keeps its
  two-rail layout, all aria-labels (`aria-label="Zones"` especially — the e2e smoke
  test `e2e/tests/smoke.spec.ts` selects on it), the collapse behaviour, and the
  `tenantPicker` slot. Styling changes only, plus the new theme toggle button.
- **No hardcoded colors in components.** Every color in tsx goes through a CSS
  token (Tailwind arbitrary value `bg-(--token)` / `text-(--token)` or an existing
  semantic utility). This is a lint rule (docs/standards/frontend.md).
- **No fake data.** Do NOT add the mockup's vitals strip / live counts / on-shift
  card — there are no APIs for them yet. Dashboard shows the department cards only.
- **The `!important` + specificity strategy in styles.css is deliberate** — read the
  file's header comment before editing. The dark block must use
  `:root[data-theme="dark"], [data-theme="dark"]` selectors with `!important` on
  every declaration, placed AFTER the light block.
- Tests use Vitest + `renderWithProviders` from `@hms/api/testing`.
- Existing behaviour to preserve: `visibleZones(can)` filtering, plain `<a>`
  navigation, localStorage rail/panel persistence.
- Gates per task: `pnpm turbo lint type-check test` green for the touched packages;
  full `pnpm turbo lint type-check test build` at the end.
- Commits: conventional, single-line, no signatures, no attribution.

---

## Task 1 — Token system in `packages/ui/styles.css`

Replace the current light values and add a dark block. Keep the file's header
comment (update its first paragraph: the palette is now HMS's own "Frost/Night"
system, no longer mirrored from tesserix-home; keep the specificity/alpha NOTE
paragraphs verbatim). Keep the `.hms-sidebar-border` rule at the bottom unchanged.

### Light block (existing selector group `:root, :root[data-theme="default"], [data-theme="default"]`, all `!important`)

Content surface:
```
--background: #F4F6F9;      --foreground: #0F1728;
--card: #FFFFFF;            --card-foreground: #0F1728;
--popover: #FFFFFF;         --popover-foreground: #0F1728;
--primary: #0E7569;         --primary-foreground: #FFFFFF;
--secondary: #F0F3F7;       --secondary-foreground: #3A4557;
--muted: #F6F8FB;           --muted-foreground: #647082;
--accent: #E3F2EF;          --accent-foreground: #0A5A50;
--destructive: #C93B3B;     --destructive-foreground: #FFFFFF;
--border: #E5EAF1;          --input: #E5EAF1;          --ring: #0E7569;
```
Sidebar (light rails — this flips the current dark-slate rails to light):
```
--sidebar: #FAFBFD;             --sidebar-foreground: #0F1728;
--sidebar-primary: #0E7569;     --sidebar-primary-foreground: #FFFFFF;
--sidebar-accent: #F0F3F7;      --sidebar-accent-foreground: #0F1728;
--sidebar-border: #EBEFF5;      --sidebar-ring: #0E7569;
--sidebar-rail: #FFFFFF;
```
HMS semantic tokens (new, same block):
```
--hms-accent: #0E7569;       --hms-accent-strong: #0A5A50;   --hms-accent-dim: #E3F2EF;
--hms-good: #12915E;         --hms-warn: #B95E04;            --hms-crit: #C93B3B;
--hms-rose: #C13B5E;         --hms-rose-tint: #FBEDF1;
--hms-amber: #A16207;        --hms-amber-tint: #FBF3E4;
--hms-violet: #5B5BD6;       --hms-violet-tint: #EEEEFB;
--hms-track: #E7EBF1;
```

### Dark block (NEW, after the light block): `:root[data-theme="dark"], [data-theme="dark"]`, all `!important`

```
--background: #0F1317;      --foreground: #E8EDF2;
--card: #161C22;            --card-foreground: #E8EDF2;
--popover: #161C22;         --popover-foreground: #E8EDF2;
--primary: #45C4B4;         --primary-foreground: #06211D;
--secondary: #1B222A;       --secondary-foreground: #E8EDF2;
--muted: #1B222A;           --muted-foreground: #8B96A3;
--accent: #182B2D;          --accent-foreground: #45C4B4;
--destructive: #F87171;     --destructive-foreground: #0F1317;
--border: #232B34;          --input: #232B34;          --ring: #45C4B4;
--sidebar: #12161B;             --sidebar-foreground: #E8EDF2;
--sidebar-primary: #45C4B4;     --sidebar-primary-foreground: #06211D;
--sidebar-accent: #1B222A;      --sidebar-accent-foreground: #E8EDF2;
--sidebar-border: #1C232B;      --sidebar-ring: #45C4B4;
--sidebar-rail: #0B0E11;
--hms-accent: #45C4B4;       --hms-accent-strong: #5FD8C8;   --hms-accent-dim: #182B2D;
--hms-good: #4ADE80;         --hms-warn: #FBBF24;            --hms-crit: #F87171;
--hms-rose: #F17E9C;         --hms-rose-tint: #302831;
--hms-amber: #F2C464;        --hms-amber-tint: #30302A;
--hms-violet: #9D9DF0;       --hms-violet-tint: #26273B;
--hms-track: #232B34;
```

(The `*-tint` dark values are the light-theme 12%-alpha tints pre-composited
over the dark card `#161C22` — that is why they look muddy as raw hex; correct.)

**Verify:** `pnpm turbo lint build --filter=@hms/ui` (or the package's own scripts)
passes; grep confirms zero `rgba(` in styles.css.

---

## Task 2 — Theme switching: init script, toggle, shell restyle

All in `packages/ui` plus one-line wiring in each app layout.

### 2a. `packages/ui/src/theme.ts` (new)

- Export `const THEME_STORAGE_KEY = "hms.theme"` (values `"light" | "dark"`; absent = follow system).
- Export `THEME_INIT_SCRIPT: string` — a minified inline IIFE that runs before
  hydration: read localStorage(THEME_STORAGE_KEY); if `"dark"`, or (no stored value
  and `matchMedia("(prefers-color-scheme: dark)").matches`), set
  `document.documentElement.dataset.theme = "dark"`; else set `"default"`.
  Wrap in try/catch (localStorage can throw). Keep it dependency-free.
- Export `ThemeToggle` client component: a 32px icon button (sun icon when dark → click
  switches to light; moon icon when light → click switches to dark). On click:
  set `document.documentElement.dataset.theme` to `"dark"`/`"default"` and persist
  `"dark"`/`"light"` to localStorage. Initialise its state from
  `document.documentElement.dataset.theme` on mount (matches what the init script
  stamped). `aria-label="Switch to dark theme"` / `"Switch to light theme"`
  accordingly. Inline SVG icons (16px, stroke currentColor) — do not add an icon
  dependency beyond lucide-react which the package already uses (lucide's `Sun`
  and `Moon` are fine and preferred).
- Export both from `packages/ui/src/index.ts`.

### 2b. Wire the init script into every app layout

In each of `apps/shell/app/layout.tsx`, `apps/medicore/app/layout.tsx`,
`apps/pharmacy/app/layout.tsx`, `apps/lab/app/layout.tsx`: keep
`<html lang="en" data-theme="default">` as the SSR default and add as the first
child of `<body>` (or in `<head>`):
`<script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />`
importing `THEME_INIT_SCRIPT` from `@hms/ui`. This prevents a light flash for
dark users. A brief comment on why it must run before paint.

### 2c. Shell restyle in `packages/ui/src/hms-shell.tsx` (styling only)

- Header: add `<ThemeToggle />` next to the "Sign out" link.
- Zone rail active state: replace `bg-sidebar-accent text-sidebar-accent-foreground`
  on the active zone link with accent treatment:
  `bg-(--hms-accent-dim) text-(--hms-accent)` plus a 3px rounded left indicator bar
  (absolutely positioned span, `bg-(--hms-accent)`, `aria-hidden`).
- Logo mark: `bg-linear-to-br from-(--hms-accent) to-(--hms-accent-strong)`
  with `text-(--sidebar-primary-foreground)`, rounded-lg — replaces the plain "H".
- Page-panel active link: keep current classes (they now resolve to the new tokens).
- Everything else (widths, collapse logic, aria, tenantPicker slot) unchanged.

### 2d. Tests (`packages/ui/src/theme.test.tsx`, new)

Using `renderWithProviders`:
1. ThemeToggle renders with the correct aria-label for the current
   `document.documentElement.dataset.theme`.
2. Clicking it flips `dataset.theme` `"default"` ↔ `"dark"` and writes
   `"light"`/`"dark"` to `localStorage["hms.theme"]`.
3. THEME_INIT_SCRIPT: evaluate it (e.g. `new Function(...)`) with a stubbed
   matchMedia/localStorage and assert the stamped attribute for: stored dark,
   stored light, nothing stored + system dark, nothing stored + system light.

**Verify:** ui package lint/type-check/test green; shell app builds.

---

## Task 3 — Dashboard department cards (`apps/shell/app/page.tsx` + `packages/ui/src/zones.ts`)

### 3a. Zone hue in the registry

In `packages/ui/src/zones.ts`, add to the `Zone` type: `hue: "rose" | "amber" | "violet"`,
with values: medicore → `"rose"`, pharmacy → `"amber"`, lab → `"violet"`,
dashboard → any (use `"rose"`; the dashboard zone never renders a card). Export
type `ZoneHue` if useful. Update `zones.test.ts` only if it asserts shape.

### 3b. Card restyle in `apps/shell/app/page.tsx`

Keep `cardsFor`/`visibleZones` logic and `DESCRIPTIONS` exactly as-is, but carry
the zone's `hue` into each card entry. Render:

- Section header above the grid: small heading "Jump into a department" (left,
  `text-sm font-semibold text-muted-foreground`) and the existing helper sentence
  right-aligned or below (`text-xs text-muted-foreground`), replacing the current
  lone paragraph.
- Card: same `<a>` grid, `rounded-xl border bg-card p-4`, hover
  `hover:border-(--hms-accent)`-free — instead `hover:bg-(--muted)` and keep
  shadow-sm hover; focus ring `focus-visible:outline-(--hms-accent)`.
- Icon chip: `h-9 w-9 rounded-lg` with per-hue tint:
  hue rose → `bg-(--hms-rose-tint) text-(--hms-rose)`, amber →
  `bg-(--hms-amber-tint) text-(--hms-amber)`, violet →
  `bg-(--hms-violet-tint) text-(--hms-violet)`. (Static class map keyed by hue —
  full class strings in the map, no string interpolation, so Tailwind sees them.)
- Trailing `→` affordance (`text-muted-foreground`, on hover `text-(--hms-accent)`),
  `aria-hidden`.
- The card's accessible name must still start with the title then description
  (smoke test matches `a[href="/medicore/opd"]` and Lab card name substring).

### 3c. Tests

Update `apps/shell/app/page.test.tsx`: cards still render per visible zone/page
with correct hrefs; add an assertion that the OPD card's chip carries the rose
tint class and Lab's the violet tint class.

**Verify:** shell app lint/type-check/test/build green.

---

## Out of scope (explicitly)

- Vitals strip, live counts, on-shift card, breadcrumb tenant name, ⌘K search —
  all need APIs that don't exist yet; separate issues.
- Density/contrast/tenant-brand dimensions of #542.
- Mobile apps (apps/mobile is a stub README).
