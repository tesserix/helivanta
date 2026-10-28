# One panel surface

Resolves #1275. Follows #1255 (one look) spec D3: "Cards are `bg-card` on the `--background` canvas, with a 1px `--border` and a near-invisible `--shadow-xs`", and "A grouping inside a card is a divider or a `--muted` region, never another bordered card."

## Problem

The workspace paints about 320 surfaces by hand, in 111 files:

- radii of `rounded-2xl` (109 uses), `rounded-xl` (187) and `rounded-lg`;
- paddings from `p-2` to `p-12`, with 20 different combinations;
- a `shadow-sm` on some cards and not on others.

So moving from Dispenses to Stock looks like moving to a different product. Cards also sit inside cards. A same-file scan finds 53, and more arise across files: for example `RequestFailure` (a bordered card) renders inside the list panels. Nothing stops a new page from adding a fourth style.

## Decisions

- **D1 One component.** `Panel` in `@helivanta/ui` is the only card surface: `rounded-lg border bg-card shadow-xs`, padded `p-5 sm:p-6` (20–24px, D3).
  - `flush` drops the padding and clips the content (`overflow-hidden`), for panels whose content is a table or a divided list running edge to edge.
  - `as` picks the element: `section` (the default), `div`, `article`, `aside` or `li`.
  - `className` carries layout only (spacing, width, grid). The lint rule (D3) rejects surface classes there, as anywhere else.
- **D2 A panel inside a panel is an inset, not a card.** `Panel` provides a context. A `Panel` rendered inside another renders as a `--muted` region: `rounded-md bg-muted p-4`, with no border or shadow (`flush` keeps it unpadded). This is structural: a component written to stand alone, such as `RequestFailure`, can be placed in a panel and cannot produce a card inside a card. `PanelLink` is a `next/link` with the panel surface plus D3's hover and `focus-visible` states. It follows the same rule: a card on the page, an inset inside a panel.
- **D3 Lint, so the surfaces cannot drift again.** In workspace files (the `hmsWorkspacePages` globs, tests excepted), `no-restricted-syntax` rejects any className, literal or template, that contains:
  - `bg-card`;
  - `rounded-xl`, `rounded-2xl` or `rounded-3xl`;
  - a full `border` together with `rounded-lg`, `rounded-xl`, `rounded-2xl` or `rounded-3xl`.

  `rounded-md` with a border remains for small controls, and `rounded-full` for badges and chips. Bordered destructive callouts become unbordered `bg-destructive/5` regions (an inset).

- **D4 A ratchet for the zones not yet converted.** The files still painting their own surfaces are listed in `apps/shell/surface-legacy.mjs`. They get the workspace rules without D3. A Vitest test fails if a listed file no longer matches a D3 pattern, so the list can only shrink. A converted file must leave the list and is then held to the rule. Each zone's tidy-up removes its files: Pharmacy #1258, Supply #1260, Workforce #1261 and Emergency #1262. New files are never listed, so new code is held to D3 from this change on.
- **D5 This change converts:**
  - `@helivanta/ui` (`RequestFailure`, `QueryState`'s failure wrapper);
  - Dashboard (the zone tiles and its insight cards);
  - Lab, MediCore and Patients;
  - Settings: Branding and Country policies;
  - `workspace-connections`.

## Not covered

- Converting Pharmacy, Supply, Workforce and Emergency (listed in D4; their zone issues own it).
- Dialogs, menus and popovers: they come from `@tesserix/web` and the `Popover` in `@helivanta/ui`, and keep `--radius-xl` and `--shadow-overlay`.
- The public pages (`hmsPublicPages`) and the login branding keep their own surfaces.
