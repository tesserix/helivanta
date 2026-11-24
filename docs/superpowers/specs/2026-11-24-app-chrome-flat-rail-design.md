# App look, slice 2: flat full-height rail, header row, panel column, sign-in ink panel — design

Issue: #1836 (child of #542; follows #1834)
Date: 2026-11-24
Amends: `2026-10-23-floating-rail-design.md` (#1286). D1 and D6 there are
reversed by this document; D2, D3, D4 and D5 stand.

## Why

After the tokens change (#1834) the app has colour and a typeface, and it is
still four cards before any content: the rail card, the header card, the
page-panel card and the page card, each with an inset, a 16 px radius, a
border and a shadow. #1286 chose that so the rail would not be "an empty band
of colour down the window" when a zone has few pages. The proof board the
user accepted on 2026-11-24 solved the same problem the other way: the rail
is a narrow ink column, so it is a frame rather than an empty band, and
everything else sits on the canvas with hairlines. That board is the design
this slice implements. The user's words on the floating version: it is the
thing that reads as a template.

This is the second of three slices. It touches the shell and the sign-in
surface only; panels inside pages are slice 3 (#1256 to #1263).

## Goals

1. From `md` up, one flat ink rail the full height of the window; a header
   that is a row, not a card; a page panel that is a column, not a card; page
   content on the canvas.
2. The current zone is unmistakable: a 3 px accent bar on the rail's left
   edge plus white icon and label, against muted icons for the rest.
3. Sign-in entry pages share the device: ink panel left, form on the canvas
   right.
4. Phones unchanged in behaviour (#1286 D3).
5. Every `navigation.spec.ts` guarantee that is not about floating survives:
   first-paint of the stored chrome, no page reload between zones, the
   `Zones` navigation name and link names.

## Non-goals

- Panels inside pages, tables, forms: #1256 to #1263.
- The mark: #1835. Until it lands the rail shows the current `icon.svg`.
- Zone order, grouping, permissions, the collapse control's behaviour.
- A dark mode. The rail is ink; the content stays light (#1255).
- Task screens on sign-in (code entry, password reset, account picker): they
  stay one centred card (#1767 D1, D2).

## Anti-goals

- A rail whose icons or labels fall below AA on ink.
- A header that becomes sticky with a backdrop; it stays outside the scroll
  container as #1286 D6 arranged, only without the card.
- A page panel that can be collapsed again (#1286 D4).
- A phone layout wider than the screen (`dashboard-phone.spec`).
- A first paint in a state the user did not choose (`navigation.spec`
  stored-chrome test).

## Constraints found in the code

- The three cards are class strings on three elements in
  `packages/ui/src/hms-shell.tsx`: the rail `<aside class="hms-rail …">`
  (`md:m-2 md:max-h-[calc(100dvh-1rem)] md:self-start md:rounded-xl
  md:border md:shadow-sm`), the `<header>` (`md:rounded-xl md:border
  md:bg-(--sidebar-rail) md:shadow-sm`), and the page panel `<aside
  class="hms-page-panel …">` (`md:mr-2 md:self-start md:rounded-xl md:border
  md:shadow-sm`). The page's own card is the content wrapper below them.
- `html[data-hms-navigation="open"] .hms-rail { width: 13rem }` in
  `packages/ui/styles.css` sizes the open rail before hydration (#1091 D7).
  The stored-chrome test expects `rail=208px panel=224px` from the first
  mutation; those widths stay.
- `.hms-sidebar-border` exists because `@tesserix/web`'s unlayered
  `border-color` rule beats Tailwind's layered utilities. The hairlines in
  this design use it where they need the sidebar border token.
- Sidebar tokens in `packages/ui/styles.css`: `--sidebar`, `--sidebar-rail`,
  `--sidebar-foreground`, `--sidebar-primary`, `--sidebar-accent`,
  `--sidebar-accent-foreground`, `--sidebar-border`, `--sidebar-ring`.
  `contrast.test.ts` asserts `--sidebar-foreground` on `--sidebar`,
  `--sidebar-accent-foreground` on `--sidebar-accent`, and the `/70` and
  `/60` blends the rail uses for muted items.
- `navigation.spec.ts` carries two #1286 D1/D6 tests that assert the rail and
  panel do not touch the top edge and are shorter than the window, and that
  the header's top meets the rail's. Both must be rewritten, not deleted.
- `LoginSurface` in `packages/ui/src/branding.tsx` already has an `entry`
  prop and a `login-story` aside shown only when `branding.login_layout ===
  "split"`; the ink panel replaces that aside's styling, not its placement.
  `.login-story` reads `--hms-accent-dim` today.

## Decisions

### D1 Rail: flat, full height, ink

From `md` up the rail is `position: static`, full window height, no margin,
no radius, no border, no shadow, background `--sidebar-rail`. Collapsed width
stays `md:w-20` (the labels under icons need it); open width stays 13 rem.
Rail tokens become:

| Token | Slice 1 value | This slice |
| --- | --- | --- |
| `--sidebar`, `--sidebar-rail` | `#ffffff` | `#0b1324` |
| `--sidebar-foreground` | `#0b1324` | `#f6f8fb` |
| `--sidebar-primary` | `#0b1324` | `#7aa0ff` |
| `--sidebar-primary-foreground` | `#ffffff` | `#0b1324` |
| `--sidebar-accent` | `#e4eaff` | `#1a2645` |
| `--sidebar-accent-foreground` | `#1f3ab8` | `#ffffff` |
| `--sidebar-border` | `#dfe4ec` | `#1a2645` |
| `--sidebar-ring` | `#2b4ee6` | `#7aa0ff` |

Muted rail items are `text-sidebar-foreground/70` as today; on `#0b1324` that
blend is `#9aa7bf`, 7.6:1. The current zone gets `aria-current="page"` as
now, white icon and label, and a 3 px `--sidebar-primary` bar on the left
edge drawn with a pseudo-element, so the marker is not a box. Hover is
`--sidebar-accent`. Focus ring `--sidebar-ring`.

The rail's head is the mark (`BrandLogo` compact, which after #1835 renders
the inline mark in white with the on-ink accent pulse); its foot is the admin
zones, the collapse control and the account avatar, as today.

### D2 Header: a row

The header loses `md:rounded-xl md:border md:shadow-sm` and its sidebar-rail
background; it is `bg-background` with a bottom hairline `border-b
border-border`. Height and contents unchanged: hospital name, zone, the
tenant picker slot, the header menu. It stays outside the scroll container.
On phones it is unchanged.

### D3 Page panel: a column

The page panel loses `md:mr-2 md:self-start md:rounded-xl md:border
md:shadow-sm` and its background; it is a `md:w-56` column on the canvas with
a right hairline, full height of the content area, list top-aligned with the
page heading. It still never collapses and has no header. The current page
is marked the same way as the rail: accent bar on the left edge, ink text;
other pages are muted.

### D4 Page content: on the canvas

The content wrapper loses its card (`rounded-xl border bg-card shadow-sm` or
equivalent). Padding stays so headings line up with the panel. Pages that
render their own `Panel`s are visually unchanged inside; that is slice 3.

### D5 Sign-in entry pages

When `entry` is true and `login_layout` is `split`, the left aside is an ink
panel: `--sidebar-rail` background, the lockup in white at the top, the
hospital's `display_name` and `welcome_text` in `--sidebar-foreground`, and a
short definition list at the foot (hospital, status line, help desk) only
where the branding payload has those fields; otherwise the foot is empty. No
illustration card. The right side is the existing form on the canvas. Below
`lg` the aside is hidden as now. The tenant theme shows through the focus
ring and links as #1834 D2 arranged; the ink panel does not take the tenant
accent, so a tenant's accent never has to pass contrast on ink.

Task screens (`entry` false) are unchanged: one centred card.

### D6 Phones

Below `md` nothing in this document applies except the token change: the
collapsed rail is the flush icon column, now ink; the expanded rail and the
panel are overlays, now ink and canvas respectively. `dashboard-phone.spec`
holds the width.

### D7 The floating-rail spec is amended, not rewritten

`2026-10-23-floating-rail-design.md` gets a note under its title: "Amended
2026-11-24 by `2026-11-24-app-chrome-flat-rail-design.md` (#1836): D1 and D6
reversed; D2 to D5 stand." Its own text is left as the record of why the
floating version was built.

## What is left to the builder

- Whether the accent bar is `::before` on the link or a sibling span; it must
  not change the link's accessible name or hit area.
- The exact hairline tokens on the header and panel (`--border` versus
  `--sidebar-border`), as long as they pass the 3:1 UI contrast on the canvas.
- How the content wrapper's padding is expressed once the card goes.

## Verification

Run through GitHub Actions on the branch.

- `contrast.test.ts` green with the D1 tokens, including the `/70` and `/60`
  blend assertions on the new rail.
- `hms-shell.test`: the current zone link has `aria-current="page"` and the
  marker; the rail has no `rounded`, `border` or `shadow` class from `md`;
  the panel renders without a header and cannot collapse.
- `navigation.spec.ts`: the two #1286 D1/D6 tests are replaced by "the rail
  spans the window height and starts at the top edge" and "the header's top
  is the window's top and its left edge meets the rail's right edge"; both
  are run once against the current chrome and fail before the change. The
  stored-chrome first-paint test passes unchanged (`rail=208px
  panel=224px`). The no-reload-between-zones test passes unchanged.
- `dashboard-phone.spec` and `login-appearance.spec` green.
- `smoke.spec.ts` selectors unchanged.
- Screenshots on the PR: dashboard and a pharmacy page at 1280 and at 390
  wide, sign-in under the platform brand and under the `blue` tenant theme.
