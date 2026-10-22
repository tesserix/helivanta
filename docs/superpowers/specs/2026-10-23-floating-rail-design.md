# Floating rail and page panel

Resolves #1286. Builds on the sidebar spec (#1270, `2026-10-22-sidebar-design.md`), whose
D1–D5 stand unchanged.

## Problem

The zone rail and the page panel are full-height columns. With a short list (PharmaConnect
has eight pages, most zones fewer) most of each column is an empty band of colour down the
window. Icon-only rail items also make a zone's name a hover away.

## Decisions

- **D1 Floating, content height.** From `md` up, the rail and the open page panel are
  cards: an inset from the window edge, rounded, bordered, `--sidebar-rail` background. Each
  is only as tall as its items, capped at the window height less the inset. Past the cap,
  the list scrolls inside the card. The rail's foot (admin zones, the collapse control) stays
  in the card, below the list.
- **D2 Labels under rail icons.** A collapsed rail shows each zone's icon with a short label
  underneath. `Zone.railLabel` is required, so a new zone without one fails type-check, and
  `zones.test` caps it at 9 characters so it fits under the icon. The link's accessible
  name stays the full `label`, which every e2e selector and `aria-current` already use. The
  expanded rail is unchanged: icon and full label on one row.
- **D3 Phones unchanged.** Below `md` the collapsed rail stays the flush 64px icon column with
  no labels, and the expanded rail and the page panel stay full-height overlays (#1270 D4,
  D5). There is no room for a floating, labelled rail on a 390px phone; `dashboard-phone.spec`
  holds the page to the screen width.
- **D4 The page panel does not collapse.** From `md` it is always shown. The stored
  `hms.panel.open` flag is no longer read, by React or by the pre-paint script, so a collapse
  saved earlier cannot hide the pages with no way to reopen them. On a phone the header button
  still opens the panel as an overlay.
- **D5 No page panel header.** The panel's header row, which held the hospital name and the
  collapse button, is removed, so the panel is only its page list.
- **D6 The header is a card across the panel and the page.** Right of the rail, the header is
  a card in the same style as the rail and panel: `--sidebar-rail` background, border, 2xl
  radius, an 8px gap. Its top is level with the rail's top, and it runs from the panel's left
  edge to the window's right edge. The panel and the page sit below it, and the page's first
  card starts level with the panel. The header is outside the scroll container, so it stays
  put without a sticky backdrop. It names the hospital on every page.

## Not covered

- Rail grouping, order and the loading placeholders (#1270) are not changed.
- No new collapse behaviour, no drag to resize.

## Verification

- `hms-shell.test`: a collapsed rail renders each zone's `railLabel`, and the link's name is
  the full label.
- `zones.test`: every zone has a `railLabel` of 1–9 characters.
- e2e `navigation.spec.ts`: on a 1280×800 window, the rail and the page panel are each
  shorter than the window, and neither touches the top edge. The header's top meets the
  rail's, its left edge meets the panel's, it reaches the right edge, and the panel sits
  below it. A stored panel collapse paints the panel open from the first frame.
