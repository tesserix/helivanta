# One set of web colour tokens for the home page and the app — design

Issue: #542 (web slice; follows #1255 one look and #1412 slate)
Date: 2026-10-28

## Why

#1255 decided on one palette for the public pages and the app. #1412 made that
palette slate. The values agree today, but two copies of them exist:

- `packages/ui/styles.css` declares the app's tokens (`--background`,
  `--foreground`, `--primary`, `--hms-*`, …). `contrast.test.ts` asserts them
  against WCAG 2.2 AA.
- `apps/shell/app/globals.css` `.home-slate` declares eleven `--home-*` tokens
  that the home page and its diagrams read 311 times across 14 files. No test
  reads them.

Measured on `9f2e851`:

| `--home-*`          | value     | shared token with the same value  |
| ------------------- | --------- | --------------------------------- |
| `--home-canvas`     | `#f8fafc` | `--background`                    |
| `--home-board`      | `#ffffff` | `--card`                          |
| `--home-surface`    | `#f1f5f9` | `--muted`                         |
| `--home-hub`        | `#e2e8f0` | `--accent`                        |
| `--home-line`       | `#cbd5e1` | `--border`                        |
| `--home-ink`        | `#0f172a` | `--foreground`                    |
| `--home-muted`      | `#475569` | `--muted-foreground`              |
| `--home-care`       | `#334155` | `--hms-accent`                    |
| `--home-care-hover` | `#1e293b` | `--hms-accent-strong`             |
| `--home-stock`      | `#92400e` | none: `--hms-amber` is `#a85503`  |
| `--home-oversight`  | `#4f46e5` | none: `--hms-violet` is `#5b5bd6` |

Nine are copies. Copies drift, and that is how the home page branch came to
render green after the app had moved to slate (#1413). The last two are a real
difference: the same hues mean different things in two places, and the app's
values are the weaker ones. `--hms-amber` measures 4.30:1 and `--hms-violet`
4.35:1 on `#e2e8f0`, which is below AA for text. `--hms-amber` marks
PharmaConnect and Supply Connect in the app rail, and the home page's stock
colour marks the same work. `--hms-violet` marks Lab and Settings, and the
oversight colour marks the same kind of review.

Nothing stops a third page from declaring its own palette, and no
`contrast.test.ts` check would notice if it did.

## Goals

- The home page and the app read colour only from the tokens in
  `packages/ui/styles.css`.
- One amber and one violet, each passing AA on every slate surface.
- A page-local colour palette fails CI.
- The home page's colours stay the same.

## Non-goals (owned elsewhere in #542 or its siblings)

- **The platform-neutral token source and Style Dictionary pipeline**,
  including mobile artefacts and the primitive tier. This slice changes no
  token names in the app, so the pipeline slice can generate this file later
  without consumers changing.
- **Border roles (WCAG 1.4.11).** `--border` `#cbd5e1` is 1.42:1 on the canvas.
  It is fine for decoration but fails where a border is the only cue on a
  control. Splitting it into decorative and interactive roles is the next slice
  (#548).
- **The type scale and the home page's display face** (slice 3, feeds #547).
- **One status system** (good/warn/crit, module status, diagram flows; slice 4).
- **Tenant theme storage, API and runtime resolution.** The three built-in
  `data-brand-theme` values are unchanged.
- **Dark mode.** The app stays light only (#1255 D1, `light-only.test.ts`).

## Decisions

### D1. The home page reads the shared tokens; `--home-*` is deleted

Every `--home-*` reference is replaced with the shared token from the table
above. For example, `text-(--home-muted)` becomes `text-muted-foreground`, and
`bg-(--home-care)/15` becomes `bg-(--hms-accent)/15`.

- Where a Tailwind utility exists for the token (`bg-background`, `bg-card`,
  `bg-muted`, `bg-accent`, `border-border`, `text-foreground`,
  `text-muted-foreground`), use the utility, as the app does.
- The `--hms-*` tokens have no utility, so they keep the arbitrary-property
  form, as the app does.

The `.home-slate` rule keeps only `color-scheme: light`. It also keeps the
`border-color` revert-layer fix, which addresses a `@tesserix/web` pitfall
rather than a palette. Its `--foreground`, `--muted-foreground` and `--border`
overrides are removed, because they assign the values the root already has.

`--home-care` maps to `--hms-accent` and not to `--primary`. On the home page,
"care" is the brand hue that marks a kind of exchange, not an action, and
`--primary` is the action colour. The home page never sits inside a
`data-brand-theme`, so its colours do not change today. If a branded public
page is ever added, its diagrams will follow the brand, which is the intended
meaning of the accent.

### D2. One amber and one violet, the stronger of each pair

`--hms-amber` becomes `#92400e` and `--hms-violet` becomes `#4f46e5`. These are
the home page's values. They pass AA on every slate surface, where the current
app values fail on `#e2e8f0`:

|           | `#ffffff` | `#f8fafc` | `#f1f5f9` | `#e2e8f0` | own tint          |
| --------- | --------- | --------- | --------- | --------- | ----------------- |
| `#92400e` | 7.09      | 6.78      | 6.47      | 5.75      | 6.43 on `#fbf3e4` |
| `#4f46e5` | 6.29      | 6.01      | 5.74      | 5.10      | 5.47 on `#eeeefb` |

The home page's stock and oversight colours then read `--hms-amber` and
`--hms-violet`, so one hue means one product area on both surfaces.

`--hms-warn` stays `#a85503`. It is a status colour, not a zone hue, and
merging the two is the status-system slice's decision.

This is the one visible change. Every place that reads either token gets the
darker hue:

- the zone hue of PharmaConnect and Supply Connect (amber) and of Lab and
  Settings (violet);
- the dashboard chart series and workforce bars that use them;
- the amber notice and badge text in pharmacy, payroll and the ambulance page;
- the amber and violet module tiles from `platform-catalog.ts` on
  `/workspace`.

The home page does not change. Its stock and oversight colours already had
these values.

### D3. The contrast gate covers what the home page draws

`contrast.test.ts` adds the pairs the home page uses that the app did not
already assert:

- `--muted-foreground` on `--accent`, and on `--muted` (already asserted).
- `--hms-accent`, `--hms-amber` and `--hms-violet` on `--background`, `--card`,
  `--muted` and `--accent`.
- The translucent text the diagrams draw: each `text-(--token)/NN` that occurs
  in source, blended over the surfaces it sits on, using the test's existing
  `blend()`. The list is read from source, not written by hand, so a new
  translucent text colour is checked when it is added.

Any pair that fails is fixed in the token or the usage in this change, not
excluded. One pair fails today: the empty-state icon's
`text-muted-foreground/60` measures 2.82:1 on the canvas, so it is drawn at the
full `text-muted-foreground` (7.24:1).

### D4. Structural enforcement: tokens are declared in one file

A new test, `packages/ui/src/token-source.test.ts`, fails CI when either of
these holds:

1. **A source file reads an undeclared custom property.** Every
   `(--name)` used in a class string under `apps/*/app`, `apps/*/components`
   and `packages/ui/src` must be declared in `packages/ui/styles.css`, in an
   app's `@theme` block, or by `@tesserix/web`. Its own CSS variables come
   from a list read from `node_modules/@tesserix/web/dist`, not hand-copied.
   This catches a reintroduced `--home-ink` with no matching declaration.
2. **A stylesheet other than `packages/ui/styles.css` declares a colour-valued
   custom property**, meaning one whose whole value is a hex, `rgb()`, `hsl()`
   or `oklch()` colour. Composite values such as the `--shadow-*` scale in
   `globals.css` are allowed, because they belong to the shape scale (#1255 D3).

Both checks must be shown to fail. The plan adds a temporary `--home-ink`
declaration and reference, sees both checks fail, then removes it.

The existing lint (`packages/config/eslint.config.mjs` `RAW_COLOUR`) already
bans hex and raw Tailwind palette colours in TSX. This test closes the gap that
lint cannot see: page-local CSS variables.

### D5. Verification: the home page's colours do not change

The token table shows the change is value-for-value. The claim is checked on
the rendered page as well, not argued from the table. Before and after the
change, a Playwright script records the computed `color`,
`background-color`, `border-*-color`, `fill`, `stroke` and `outline-color` of
every element on `/` at 1440px and 390px, and runs through every story step.
The two captures must be identical. The PR records the result.

In the signed-in app, the change is two token values. Their readers are listed
by `git grep`, and the contrast gate covers their pairs.

Committed screenshot baselines are not added. Font rasterisation differs
between macOS and the CI runner, so pixel baselines would be either flaky or
loosened until they prove little. Computed colours are the claim this slice
makes, and they are deterministic.

### D6. Docs corrected in the same change

- `CLAUDE.md` frontend rules: "one sage palette" becomes "one slate palette,
  declared only in `packages/ui/styles.css`".
- `docs/standards/frontend.md` §7: names the single source rule and the new
  test.
- `docs/superpowers/specs/2026-10-22-one-look-design.md` D2: a note that slate
  replaced sage (#1412) and that the public pages' own tokens were removed
  here.

## Acceptance

- `git grep -- '--home-'` under `apps/` returns nothing.
- `token-source.test.ts` passes, and fails on the injected regression.
- `contrast.test.ts` passes with the D3 pairs.
- The D5 capture for `/` is identical before and after the change.
- `pnpm turbo lint format:check type-check test build` is green, and
  `e2e/tests/smoke.spec.ts` and `login-appearance.spec.ts` pass.

## Left to the builder

- The exact utility for each replacement where several fit, such as
  `bg-muted` versus `bg-secondary` for `#f1f5f9`. Pick the role, not the value:
  a band behind content is `muted`, and a control fill is `secondary`.
