# App look, slice 1: one hue in the tokens and a UI typeface — design

Issue: #1834 (child of #542; follows #1255 one look, #1412 slate and
`2026-10-28-shared-web-token-roles.md`)
Date: 2026-11-24

## Why

Every colour token in `packages/ui/styles.css` is a grey. Primary, ring, the
accent surface and the active rail item all come from the same slate scale, and
`--primary` is `#334155`. The result, measured on 2026-11-24 against sign-in,
the MFA step, the hospital overview and the pharmacy counter sale:

- the primary button and a disabled button are the same colour;
- links are body-text colour and only an underline tells them apart;
- focus rings, selected tabs and the current zone share one slate tint with
  hover states, so state changes are hard to see;
- the only hue on any page comes from the four clinical status colours, so a
  warning or a critical value does not stand out against an otherwise grey
  page as much as it should.

The user's words: the app looks dull and boring. Three recolourings of the
present layout (teal, indigo, navy) were prototyped and rejected; a structural
proof board (flat rail, no chrome cards, ink buttons, one accent) was accepted
in a cool-white material. Mark8ly, the sibling product, already owns the
warm-paper, ink and moss editorial look, so Helivanta deliberately takes the
cool, clinical register.

This document covers the first of three slices. It changes token values only.
Every page reads the tokens, so every page changes at once, and nothing here
is undone by the chrome change that follows.

## Goals

1. One accent hue, used for exactly four jobs: links, selection, focus and the
   current navigation item. Primary actions are ink, not accent, so the
   darkest thing on a page is the thing to press.
2. A cool white canvas and a blue-black ink that read as clinical software,
   distinct from Mark8ly's warm paper.
3. Clinical status colours keep their values and therefore their meaning.
4. Tenant brand themes keep working and keep recolouring one thing: the
   accent trio.
5. Chart hues distinguishable from each other, from the accent and from the
   status colours.
6. A UI typeface with tabular numerals, loaded without a new browser origin.
7. Every pair `contrast.test.ts` asserts today still passes; no pair removed.

## Non-goals

- **The chrome.** The floating rail, header and page-panel cards (#1286 D1,
  D6) stay in this slice. Slice 2 makes the rail flat and full height, drops
  the header card and gives the sign-in page its ink panel. Slice 2 gets its
  own issue and spec, and it is the slice that amends #1286.
- **Panel de-boxing.** Borders only on tables and forms is a per-zone pass
  that belongs with #1256 to #1263.
- **Tenant-uploaded palettes and logos.** #24.
- **The brand mark, favicon, PWA and social assets.** A separate issue once
  the mark is chosen; the current `apps/shell/app/icon.svg` stays.
- **Dark mode.** The app stays light only (#1255).
- **Mobile layout, density or spacing changes.** None.

## Anti-goals

These would make the slice a failure even if it looked right:

- Any text/background pair below WCAG 2.2 AA (4.5:1 body, 3:1 UI).
- `--hms-good`, `--hms-warn` or `--hms-crit` changing value, or any new hue
  sitting close enough to one of them to be mistaken for it.
- A tenant theme that no longer recolours primary actions' accent, or one
  whose accent fails AA on the canvas or on its own tint.
- A new CSP violation (`e2e/tests/csp.spec.ts`), a reload between zones, or a
  broken `smoke.spec.ts` selector.
- A colour declared anywhere other than `packages/ui/styles.css`
  (`token-source.test.ts`).
- A chart where two series are the same hue.

## Constraints found in the code

- `@tesserix/web` 2.4.2 is token-driven for every primitive Helivanta mounts
  (Button, Input, Textarea, Select, Tabs, Skeleton, the auth OTP input, submit
  button and error line). Checked 2026-11-24 by grepping the package's `dist`
  for palette classes and hex values. Three exceptions exist and none is
  reached: `Input`'s `valid` prop paints Tailwind green; `Badge`'s `neutral`
  variant uses non-token classes; `AuthLayout` uses a black wash. Helivanta
  never passes `valid`, uses only `outline` and `secondary` badges, and
  renders its own `LoginSurface`.
- Token overrides need `!important` and opaque hex (pitfalls documented at
  the top of `packages/ui/styles.css`). Nothing here changes that.
- `contrast.test.ts` pins three values by name (`--background`,
  `--foreground`, `--primary`) and asserts about forty pairs, including the
  accent, amber and violet hues on every surface, the ring on the canvas at
  3:1, and each tenant theme's accent on the canvas and on its own tint.
- `--hms-violet` is the IPD and "completed" chart series in
  `operational-dashboard.tsx`; the four chart hues are read in fifteen
  component files, seven of them the public home page's landing boards.
- The root layout loads no font; the public home page alone loads Anek Latin
  with `next/font/local` from committed OFL files in `apps/shell/app/fonts/`
  (`page.tsx`). That is the repository's precedent for fonts: committed files,
  never a build-time fetch, because `ci.yml` and `Dockerfile.shell` build on
  runners whose egress has needed DNS-recovery and mirror steps before.
  `next/font` self-hosts under `/_next/static`, so the CSP's `font-src 'self'`
  already covers it.
- `e2e/tests/login-appearance.spec.ts` pins the sign-in button's colour per
  tenant theme (slate, blue, indigo) and asserts it idle and busy. D2 changes
  that premise and the test changes with it (see Verification).
- The three tenant theme blocks appear twice: once in the app scope and once
  under `.login-page`. Both must change together.

## Decisions

### D1 Token values

Every value below was checked for contrast on 2026-11-24. The lowest body
pair is critical red on the muted surface at 4.55:1.

| Token                                       | Today                 | New                   | Role                                                                                     |
| ------------------------------------------- | --------------------- | --------------------- | ---------------------------------------------------------------------------------------- |
| `--background`                              | `#f8fafc`             | `#f6f8fb`             | canvas                                                                                   |
| `--foreground`                              | `#0f172a`             | `#0b1324`             | ink                                                                                      |
| `--card`, `--popover`                       | `#ffffff`             | `#ffffff`             | surfaces                                                                                 |
| `--card-foreground`, `--popover-foreground` | `#0f172a`             | `#0b1324`             |                                                                                          |
| `--primary`                                 | `#334155`             | `#0b1324`             | filled buttons                                                                           |
| `--primary-foreground`                      | `#ffffff`             | `#ffffff`             |                                                                                          |
| `--secondary`, `--muted`                    | `#f1f5f9`             | `#f0f4f8`             | quiet surfaces                                                                           |
| `--secondary-foreground`                    | `#334155`             | `#0b1324`             |                                                                                          |
| `--muted-foreground`                        | `#475569`             | `#4e5a6e`             | secondary text                                                                           |
| `--accent`                                  | `#e2e8f0`             | `#e4eaff`             | selected / hover tint                                                                    |
| `--accent-foreground`                       | `#1e293b`             | `#1f3ab8`             | text on the tint                                                                         |
| `--destructive`                             | `#c93b3b`             | `#c93b3b`             | unchanged                                                                                |
| `--border`                                  | `#cbd5e1`             | `#dfe4ec`             | hairlines                                                                                |
| `--input`                                   | `#cbd5e1`             | `#c7cfdc`             | field borders                                                                            |
| `--ring`                                    | `#334155`             | `#2b4ee6`             | focus                                                                                    |
| `--sidebar`, `--sidebar-rail`               | `#f1f5f9`, `#ffffff`  | `#ffffff`             | rail card (slice 2 makes it ink)                                                         |
| `--sidebar-foreground`                      | `#0f172a`             | `#0b1324`             |                                                                                          |
| `--sidebar-primary`                         | `#334155`             | `#0b1324`             |                                                                                          |
| `--sidebar-accent`                          | `#e2e8f0`             | `#e4eaff`             | current item                                                                             |
| `--sidebar-accent-foreground`               | `#0f172a`             | `#1f3ab8`             |                                                                                          |
| `--sidebar-border`                          | `#cbd5e1`             | `#dfe4ec`             |                                                                                          |
| `--sidebar-ring`                            | `#334155`             | `#2b4ee6`             |                                                                                          |
| `--hms-accent`                              | `#334155`             | `#2b4ee6`             | links, selection                                                                         |
| `--hms-accent-strong`                       | `#1e293b`             | `#1f3ab8`             | link hover, text on tint                                                                 |
| `--hms-accent-dim`                          | `#e2e8f0`             | `#e4eaff`             | selection tint                                                                           |
| `--hms-good` / `--hms-warn` / `--hms-crit`  |                       | unchanged             | clinical status                                                                          |
| `--hms-rose` / tint                         | `#c13b5e` / `#fbedf1` | unchanged             | chart                                                                                    |
| `--hms-amber` / tint                        | `#92400e` / `#fbf3e4` | unchanged             | chart                                                                                    |
| `--hms-violet` / tint                       | `#4f46e5` / `#eeeefb` | `#661565` / `#f7e9f9` | chart; moved off the accent's hue and darkened to separate in lightness from every theme accent (at least 1.68:1) |
| `--hms-sky` / tint                          | `#1d5da8` / `#e7effa` | `#0e7490` / `#e3f3f8` | chart; moved off blue                                                                    |
| `--hms-track`                               | `#e7ebf1`             | `#e3e8ef`             | progress track                                                                           |
| `--hms-band`                                | `#f1f5f9`             | `#f0f4f8`             | home-page bands                                                                          |
| `--hms-band-ink`                            | `#0f172a`             | `#0b1324`             |                                                                                          |
| `--hms-band-muted`, `--hms-band-line`       | `#475569`             | `#4e5a6e`             |                                                                                          |
| `--hms-shadow`                              | `#334155`             | `#0b1324`             |                                                                                          |

Why these: the canvas is a cool white rather than pure white so cards still
read as surfaces; the ink is blue-black rather than grey-black so the page has
a temperature; ultramarine is far in hue from all three status colours and
from Mark8ly's moss; the two chart hues that were blue or indigo move to plum
and cyan so no series is confused with a link or a selected row.

### D2 Primary is ink; the accent is not a button colour

Today the tenant theme blocks set `--primary` and the accent trio together.
After this change they set the accent trio only, in both scopes. Primary
actions stay ink under every tenant theme; what a tenant recolours is links,
focus, selection and the current navigation item. The login submit button
follows: `--auth-button` reads `var(--primary)` instead of
`var(--hms-accent)`, so sign-in has an ink button under the platform brand and
under every tenant brand. `contrast.test.ts`'s per-brand assertions keep
passing unchanged, since they check primary-foreground on primary and the
accent on the canvas and on its tint.

### D3 Tenant theme values

| Theme                | `--hms-accent` | `--hms-accent-strong` | `--hms-accent-dim` |
| -------------------- | -------------- | --------------------- | ------------------ |
| `teal` (the default) | `#2b4ee6`      | `#1f3ab8`             | `#e4eaff`          |
| `blue`               | `#1d5da8`      | `#17497f`             | `#e7effa`          |
| `indigo`             | `#5352a6`      | `#403f83`             | `#eeedf9`          |

Amended during implementation: `teal` is the theme every workspace stores
by default (`DEFAULT_BRANDING` and the Go default), so it carries the
platform accent rather than a real teal; a real teal would have turned every
default workspace teal. Its settings label becomes "Helivanta default". The
values above apply to `blue` and `indigo`; `teal` reads `#2b4ee6`, `#1f3ab8`,
`#e4eaff`. A tenant-selectable teal would need a new enum value and is not in
this slice. The enum in `packages/api/src/workspace.ts` is unchanged.

### D4 Typeface

IBM Plex Sans, weights 400, 500, 600 and 700 (Tailwind's `font-bold` is 700
and headings use it; a missing weight would be synthesised), committed as
OFL woff2 files beside the Anek files in `apps/shell/app/fonts/` and loaded
with `next/font/local` in `apps/shell/app/layout.tsx`, exposed as a CSS
variable the body reads. Local, not `next/font/google`: the repo's precedent
and its CI egress history both argue against a build-time fetch.
Chosen for its numerals (tabular figures for bills, counts and stock), its
Devanagari sibling for Hindi locales later, and because it is not the default
face of every other dashboard. `font-variant-numeric: tabular-nums` is applied
to nothing globally in this slice; Tailwind's `tabular-nums` utility is
available for the per-page passes. No serif, no display face: #1255's "no
marketing type in the app" stands.

`next/font` self-hosts the files under `/_next/static`, so the CSP's font and
style sources do not change and `csp.spec.ts` must stay green without edits.

The public home page already ships a face of its own: Anek Latin, loaded with
`next/font/local` from `apps/shell/app/fonts/` and applied only on `/`
(`page.tsx`). Anek is an Ek Type family with Devanagari, Bangla, Gujarati,
Gurmukhi, Kannada, Malayalam, Odia, Tamil and Telugu siblings, which matters
for the same reason Plex's Devanagari does. Two options were weighed:

- **Plex in the app, Anek on the public headlines** (this document's
  choice). Anek is a display grotesque with a width axis; at 13 to 15 px in
  a stock table its narrow counters and tight spacing cost legibility, and
  its figures are not tabular by default. Plex is built for exactly that.
  The public page keeps Anek for headlines and gets Plex for body through
  the shared root layout, so the two surfaces share a body face.
- **Anek everywhere**, which would add no font file. Rejected for the table
  and numeral reasons above, but it is the cheaper call if the user prefers
  one family; the font file is already licensed (OFL) and in the repo.

Confirmed 2026-11-24: Plex in the app, Anek stays on the public headlines.

### D5 Ban the one non-token escape hatch

A `no-restricted-syntax` entry in `packages/config/eslint.config.mjs`, beside
the existing design rules, fails any `<Input valid …>`. The message names the
reason: the prop paints a non-token green beside the clinical good colour.
This is enforcement by CI failure, the strongest form available for a prop on
a third-party component.

### D6 Chart series stay attributable

`operational-dashboard.tsx` keeps reading `--hms-accent` for OPD and
`--hms-violet` for IPD and "completed". Only the values move. The legend and
tooltips are unchanged, so no test that names a series changes.

## What is left to the builder

- The exact `next/font/local` wiring (variable name, `display` strategy)
  and which subset files to commit; Latin and Latin Extended are enough for
  this slice. The test mock follows `landing.test.tsx`'s mock of
  `next/font/local`.
- Whether `contrast.test.ts`'s three pinned values are updated in place or
  the pin is loosened to "is a hex"; updating in place is simpler and keeps
  the guard.
- The lint rule's selector shape, following the neighbouring `DESIGN_SYNTAX`
  entries, and a test case in `eslint.config.test.mjs` proving it fails.
- The order of the two stylesheet edits (app scope and `.login-page` scope).

## Verification

Run through GitHub Actions on the branch, not locally (AGENTS.md).

- `packages/ui` Vitest: `contrast.test.ts` green with the new values and no
  pair removed; `token-source.test.ts` green.
- `packages/config` `eslint.config.test.mjs`: the new rule rejects
  `<Input valid />` and accepts `<Input />`.
- `pnpm turbo lint type-check test build format:check` green.
- Playwright `csp.spec.ts`, `smoke.spec.ts`, `navigation.spec.ts` green.
- `login-appearance.spec.ts` rewritten for D2: under every tenant theme the
  submit button is ink (`rgb(11,19,36)`) idle and busy, and the theme is
  still visible on the page through the story panel's tint
  (`--hms-accent-dim`) and the focused field's ring (`--auth-accent`), which
  differ per theme. The old per-theme button colours go; the new assertions
  are proven to fail by running them once against the current values.
- By eye on the hosted build, with screenshots attached to the PR: the
  dashboard overview and patient-activity tabs (series distinguishable), the
  pharmacy counter sale and stock pages, the sign-in page under the platform
  brand and under each of the three tenant themes, and the public home page's
  landing boards (chart hues changed there too).

## Follow-ups this slice creates

- Slice 2: flat full-height ink rail, header as a row, page panel as a plain
  column, sign-in ink panel; amends #1286 D1 and D6 and restates the sidebar
  tokens. Issue to be filed when slice 1 merges.
- Mark, favicon and asset set: issue to be filed when the mark is chosen.
