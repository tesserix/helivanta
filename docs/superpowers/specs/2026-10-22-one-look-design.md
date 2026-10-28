> Palette superseded on 2026-10-28: the approved theme-only change uses light slate (canvas `#f8fafc`, ink `#0f172a`, primary `#334155`) in the shared UI and homepage. The original layout, copy and animations remain unchanged. Historical sage values below describe the earlier decision. The home page's own `--home-*` tokens were removed later the same day; it reads the shared tokens (#542, `2026-10-28-shared-web-token-roles.md`).

# One look: the signed-in app takes the home page's design — design

Issue: #1255 (child of #542 Design tokens & theming; resolves #1088)
Date: 2026-10-22

## Why

Going from the home page (`/`) into the app (`/dashboard`) feels like moving
into a different product. Measured on 2026-10-21 against a production build:

|           | Home page (`.public-site`)                                     | Signed-in app (`packages/ui/styles.css`)                              |
| --------- | -------------------------------------------------------------- | --------------------------------------------------------------------- |
| Palette   | warm sage — canvas `#fbfcfa`, ink `#173d3a`, primary `#23685d` | cool blue-grey — `#f4f6f9`, navy `#0f1728`, teal `#0e7569`            |
| Dark mode | never                                                          | follows the OS (`THEME_INIT_SCRIPT`)                                  |
| Headline  | 72px, tracking −0.055em, serif italic accent line              | 36px, sans only                                                       |
| Shapes    | pill buttons, 2rem cards, soft shadow                          | 0.625rem radius, cards inside cards                                   |
| Density   | one idea per screen                                            | four tabs, seven filter controls, a caveat paragraph under every card |

The chrome adds clutter of its own:

- The Dashboard zone's page panel lists one link ("Departments").
- The header says "Departments" while the rail says "Dashboard".
- "Sign out" appears in both the rail and the header.

Two colour systems exist because the home page defines 31 raw hex values in
`apps/shell/app/globals.css` `.public-site` (#1088). Matching by eye would
drift again. The fix is structural: one token set that both surfaces read.

## Goals

1. One light palette, the home page's sage, defined once in
   `packages/ui/styles.css` and read by the public pages and the app alike.
2. The app reads as enterprise software, not a template, an open-source demo
   or generated UI (user, 2026-10-22). It shares the home page's palette and
   calm, not its marketing devices (D3).
3. The chrome carries no dead weight:
   - no page panel for a zone with one page;
   - one "Sign out";
   - the header names the zone the rail marks current.
4. The existing dashboard keeps its data and its permission gating, but
   reads simply:
   - its filters sit behind one control;
   - its caveats sit behind an info hint.

"Zone" in this document means a route section of the one Next app,
`apps/shell` (#933, ADR 0010). Multi-zones are gone, so every change here
lands once, in one app.

## Non-goals

- **The role-based "Today" landing page.** It belongs to #573's shared
  dashboard framework (Sam123ben), with tile sets in #574–#582. Building role
  cards here would be the parallel tile system #573 exists to prevent. A
  comment on #573 records what was decided: everyone lands on Today and
  managers reach Insights from it.
- **Tidying each zone's own pages** (forms, nested cards, explanatory text):
  - patients, pharmacy, lab, supply, workforce, emergency and settings get
    one follow-up issue each;
  - they still inherit the new palette and shapes through the tokens in this
    change.
- **New dashboard metrics.** That is #1234.
- **Readable tenant names in the tenant picker.** `/v1/iam/me/tenants`
  returns no name, so this needs an IAM change; it gets a follow-up issue.
- **New typefaces.** The home page uses the system sans and `ui-serif`, so no
  web font is added.

## Anti-goals

These would make the change a failure even if it looks right:

- Any text/background pair below WCAG 2.2 AA (4.5:1 body, 3:1 large text and
  UI components).
- Clinical status colours (`--hms-good`, `--hms-warn`, `--hms-crit`) shifting
  in meaning or losing contrast.
- Tenant brand themes (`data-brand-theme` teal/blue/indigo) no longer
  recolouring primary actions.
- A new CSP violation, a page reload between zones, or a broken
  `smoke.spec.ts` selector.

## Decisions

### D1. Light only

- The app stops following the OS dark setting and drops the theme toggle.
  Taken by the user on 2026-10-22.
- Remove:
  - `THEME_INIT_SCRIPT` and its nonce'd `<script>` in `apps/shell/app/layout.tsx`;
  - `ThemeToggle` and `useThemeAttribute`;
  - the `hms.theme` storage key;
  - every `[data-theme="dark"]` token block in `packages/ui/styles.css`;
  - the two `dark:` utilities;
  - the copy of `useThemeAttribute` in `packages/api/src/providers.tsx`.
- `<html>` keeps `data-theme="default"`. @tesserix/web's light rules key off
  it.
- A browser that still holds `hms.theme=dark` from before is ignored: nothing
  reads the key any more.
- Fail-closed direction: with no script there is no path to a dark paint, so
  the remaining risk is a stray dark token. A unit test asserts that
  `styles.css` contains no `data-theme="dark"` selector.

### D2. One palette, sage, in the shared tokens

- The `:root[data-theme="default"]` block in `packages/ui/styles.css` takes
  the home page's values. Proposed mapping, final values set in the plan
  after the contrast check:

  | token                                                       | from (app)          | to (home page)                           |
  | ----------------------------------------------------------- | ------------------- | ---------------------------------------- |
  | `--background`                                              | `#f4f6f9`           | `#fbfcfa` (`--site-canvas`)              |
  | `--foreground`, `--card-foreground`, `--popover-foreground` | `#0f1728`           | `#173d3a` (`--site-ink`)                 |
  | `--primary`, `--ring`, `--sidebar-primary`, `--hms-accent`  | `#0e7569`           | `#23685d` (`--site-primary`)             |
  | `--hms-accent-strong`                                       | `#0a5a50`           | `#194e46` (`--site-primary-hover`)       |
  | `--accent`, `--hms-accent-dim`                              | `#e3f2ef`           | `#eaf1e9` (`--site-tint`)                |
  | `--muted-foreground`                                        | `#647082`           | `#60796e` (`--site-muted`)               |
  | `--border`, `--input`                                       | `#e5eaf1`           | `#dce5df` (`--site-line`)                |
  | `--muted`, `--secondary`                                    | `#f6f8fb`/`#f0f3f7` | `#f1f5ef` (`--site-note`)                |
  | `--sidebar`                                                 | `#fafbfd`           | `#f4f7f2` (new, between canvas and note) |

- The home page's tokens are needed only there: the dark "Trust" band and the
  numbered preview rows. They move into `styles.css` as named tokens
  (`--hms-band`, `--hms-band-ink`, …).
- `.public-site` then reads the shared tokens. It keeps no hex values, and
  the raw palette classes on the public pages (`bg-teal-600`, `text-white`,
  `bg-white/90`) become token utilities. This resolves #1088 minus its
  dark-mode ask, which D1 overrides; the issue gets a comment saying so.
- The `[data-brand-theme="teal"]` default moves to the sage primary. Blue and
  indigo stay as they are, because a tenant's brand is the tenant's choice.
- Status colours keep their hues. Each is re-checked against the new canvas
  and card, and darkened only if it fails AA.
- **Enforced:** a contrast test computes the ratio for every
  foreground/background pair the UI renders, against AA:
  - the token pairs;
  - each brand theme's primary on its foreground;
  - each status colour on the canvas and on a card.

  §7 of `docs/standards/frontend.md` bans hardcoded colours, but nothing
  enforces it: no lint rule exists. This change adds one to
  `apps/shell/eslint.config.mjs` and `packages/ui/eslint.config.mjs`. It
  fails on raw Tailwind palette classes (`bg-teal-600`, `text-white`, …) and
  hex arbitrary values in class strings. Today there are 14 such uses in the
  workspace pages, plus the public pages; this change removes them all.

### D3. Enterprise restraint: shapes, type and surfaces

The user's bar (2026-10-22): a _modern_ enterprise product (the register of
Linear, Stripe or Vercel's dashboards), not an open-source demo, generated UI
or an old-school dense admin console. The two share the palette; the app does not borrow the
landing page's marketing devices, which are what read as templated inside a
working tool.

**Banned inside the app** (the public pages may keep their own marketing
headline):

- the serif or italic accent line;
- the small uppercase, letter-spaced eyebrow over a large headline;
- slogans as page titles ("Your care, in perspective.") and decorative ledes
  that restate the page's purpose;
- gradients, glows, decorative tinted icon tiles beside every heading, and
  emoji;
- pill-shaped buttons;
- cards inside cards.

**Shapes.**

- Raising `--radius` alone changes nothing. @tesserix/web's components use
  Tailwind's `rounded-md`/`rounded-lg`, which read Tailwind's own
  `--radius-*` scale, and its Button exposes no hook to restyle (123 files
  import it directly).
- The scale is set once in an `@theme` block in `apps/shell/app/globals.css`:
  - `--radius-sm: 0.375rem` (badges, small controls);
  - `--radius-md: 0.5rem` (buttons, inputs);
  - `--radius-lg: 0.75rem` (cards, menus, popovers);
  - `--radius-xl: 1rem` (dialogs, the largest surfaces).

  Rounded enough to feel current, short of the landing page's 2rem cards.

- `rounded-full` is only for status badges, avatars and filter chips.

**Surfaces.**

- Cards are `bg-card` on the `--background` canvas, with a 1px `--border`
  and a near-invisible `--shadow-xs`, so they lift slightly off the page.
  Floating surfaces (menus, popovers, dialogs, toasts) take a stronger
  `--shadow-overlay`. Nothing else casts a shadow.
- A grouping inside a card is a divider or a `--muted` region, never another
  bordered card.
- Every interactive element has hover, active and `focus-visible` states:
  a subtle `--accent` fill on hover, and a 2px `--ring` focus ring with an
  offset. Transitions are 150ms and respect `prefers-reduced-motion`.
  Loading uses skeletons, not spinners on blank pages.

**Type.**

- The system sans throughout. The weight scale is 400/500/600, with no 700+
  except numbers.
- `PageHeading` in `packages/ui` is one component for every page:
  - a title (`text-xl font-semibold`, normal tracking);
  - an optional one-line description in `text-sm text-muted-foreground`;
  - an `actions` slot on the right.

  It has no eyebrow and no accent.

- Numbers in stat tiles and tables use `tabular-nums`, so columns align.
- Sentence case for every label, button and heading.

**Spacing and density.**

- A 4px grid with generous but purposeful space: page padding 32px on
  desktop and 16px on mobile, card padding 20–24px, and 24–32px between
  sections.
- Tables are comfortable, not cramped: a 44px row height, sticky headers,
  hover rows, aligned columns and right-aligned numbers. Modern means calm
  and legible, not dense.

**Copy.** Plain and specific: a label says what a thing is, a button says
what it does. No marketing voice inside the app.

**Enforced.** A test over `PageHeading` asserts that no eyebrow or serif
prop exists. A lint rule bans `font-serif`, `italic`, `bg-gradient-*`,
`bg-linear-*` and `uppercase tracking-*` combinations in `apps/shell`
outside `app/page.tsx`, `app/signup/` and `app/ambulance/`, the public
pages.

### D4. Chrome

- **Page panel.** It renders only when the current zone has more than one
  page. With one page, the panel and its collapse button are absent, and the
  content takes the space. This changes behaviour for Dashboard and any other
  single-page zone. `hms-shell.test.tsx` covers both counts.
- **Header title.** It shows the current page's label when the zone has
  several pages, and the zone's label when it has one. The dashboard's single
  page is renamed from "Departments" to "Overview" in `zones.ts`, so nothing
  says "Departments" for a page about the whole hospital.
- **Sign out** stays in the header, which is visible at every width; the
  rail's copy is removed. The idle-timeout sign-out is unaffected.
- **Theme toggle** is gone (D1).
- The tenant picker keeps its current label until the follow-up issue. It
  already renders nothing for single-tenant users.

### D5. The existing dashboard, simplified

The data, the queries and the `can(...)` gating stay exactly as they are. The
presentation changes:

- **Heading:** `PageHeading` with the title "Hospital overview", no
  description, and the reporting-period control in its `actions` slot. The
  "Hospital intelligence" eyebrow, the slogan title and the lede go (D3), and
  so does the "Current tenant · authorised data" pill: it states what every
  page already guarantees.
- **Tabs:** the four tabs stay, as @tesserix/web `Tabs` with an underline
  indicator, not a segmented pill.
- **Filters:**
  - The quick ranges (Today … Last 5 years) become one compact segmented
    control.
  - From/To, Group by and Timezone move into a "Custom range" popover with
    its own Apply button.
  - Lines/Bars moves onto each chart.
- **Caveats:** each card's caveat paragraph ("Completed does not mean
  resulted…") moves behind an info button next to the card title. It is
  keyboard reachable and announced, and the text is unchanged: these
  definitions are #1234's measurement contract, so they are hidden from view,
  not deleted.
- **Cards:**
  - Each section card drops its decorative tinted icon tile; the section
    title stands alone, with "Open workspace" as a plain link on the right.
  - Stat tiles inside a section become one row of figures separated by
    dividers, not a row of bordered cards.

### D6. No native dropdowns

- Every dropdown is @tesserix/web's `Select`, used through the existing
  wrappers in `packages/ui/src/select.tsx`:
  - `SelectControl` for plain state;
  - `FormSelectField` inside a `useZodForm` form.
- The native `<select>` popup ignores the design tokens: it is drawn by the
  OS, in OS colours. The wrappers already handle brand themes and the
  empty-value case. Requested by the user on 2026-10-22.
- Four native `<select>` elements remain, all in emergency code:

  | file                                                               | control                  |
  | ------------------------------------------------------------------ | ------------------------ |
  | `apps/shell/app/ambulance/page.tsx`                                | "Where are you?" country |
  | `apps/shell/components/emergency/emergency-dispatch.tsx`           | "Ready vehicle"          |
  | `apps/shell/components/emergency/emergency-receiving.tsx`          | receiving-side choice    |
  | `apps/shell/components/emergency/emergency-receiving-dispatch.tsx` | dispatch choice          |

  Each one moves to `SelectControl`, or to `FormSelectField` where it already
  sits in a zod form.

- `/ambulance` is the public emergency page. It is already a client
  component, so the change does not add a JavaScript dependency it lacked.
  The `Select` trigger must still:
  - keep the `min-h-12`/`text-base` touch target;
  - keep the label association (`aria-labelledby` the visible label);
  - open with Enter, Space and the arrow keys.
- `e2e/tests/emergency-access.spec.ts` drives that control with
  `selectOption`, which works only on a native select. The two calls become
  a combobox click and an option click by role and name.
- **Enforced:** a `no-restricted-syntax` rule on JSX `select` elements in
  `apps/shell`, `packages/ui` and `packages/api`. Its message names
  `SelectControl` and `FormSelectField`.
- The Expo app `apps/ambulanceconnect` is React Native and has no DOM
  `select`, so the rule does not cover it.

### D7. Proof

- **Unit tests:**
  - token contrast (D2);
  - no dark selector (D1);
  - the page panel with one and with two pages, the header title, a single
    Sign out (D4);
  - the dashboard's popover filter applying, and the caveats reachable
    through the info control (D5).
- **e2e:** `csp.spec.ts`, `navigation.spec.ts`, `smoke.spec.ts`,
  `journey.spec.ts`, `login-appearance.spec.ts` and
  `emergency-access.spec.ts` pass on a production build.
- **Unit tests for D6:** each converted control selects a value through its
  combobox. The lint rule is shown to fail on a fixture `<select>`.
- **Visual check:**
  - before and after screenshots of `/`, `/login`, `/dashboard` and one page
    from every zone, in the PR body;
  - an emulated `prefers-color-scheme: dark` run showing the light theme.

## Follow-up issues opened with this change

- One per zone for page-level tidying: patients, MediCore, PharmaConnect,
  Lab, Supply Connect, Workforce, EmergencyConnect, Settings.
- The tenant picker shows a readable tenant name (needs `name` from
  `/v1/iam/me/tenants`).

## Left to the builder

- The final hex values after the contrast test, staying within the sage
  family.
- The exact shadow, and whether stat tiles use `--muted` or `--accent`.
- The popover component; reuse what @tesserix/web ships.

## As built

- D2: `--hms-good` #12915e → #0f7a4f, `--hms-warn` #b95e04 → #a85503 and
  `--hms-amber` #a16207 → #a85503 all failed AA on main and were darkened
  within their hue. `--muted-foreground` is the home page's body colour
  #57716a, because its lighter #60796e fails on tinted tiles. New tokens:
  `--hms-band*` (the Trust band), `--hms-shadow`, and `--hms-sky*` (a
  platform-catalogue category).
- D3: shadows live in the same `@theme` block as the radii, as Tailwind
  shadow variables (alpha is fine there; the no-alpha rule concerns
  @tesserix/web colour tokens). In-app eyebrows lost their uppercase and
  tracking; the per-zone issues remove them. The login surface opts out of
  the marketing rule as a public page.
- D4: "single-page" counts pages after permission filtering. Without a
  panel, the header reads "<hospital> / <zone>", so the tenant name never
  leaves the screen.
- D5: definitions open in a click popover, not a hover tooltip, so they
  work on touch. @tesserix/web Tabs render filled, not underlined. The
  range popover ignores close requests while its Group by menu is open.
- D6: the campus authority select had no test; one now proves the choice
  reaches the request body.
