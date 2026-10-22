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
- **D3 Phones unchanged.** Below `md` the expanded rail and the page panel stay full-height
  overlays (#1270 D4, D5). A drawer is the right shape there, and the collapsed rail's
  spacer widens to the new rail width.
- **D4 A closed page panel takes no space.** When closed, the panel drops its border and
  inset as well as its width, both in React and in the pre-paint `data-hms-panel="closed"`
  rule, so no 1px seam is left behind.

## Not covered

- Rail grouping, order and the loading placeholders (#1270) are not changed.
- No new collapse behaviour, no drag to resize.

## Verification

- `hms-shell.test`: a collapsed rail renders each zone's `railLabel`, and the link's name is
  the full label.
- `zones.test`: every zone has a `railLabel` of 1–9 characters.
- e2e `navigation.spec.ts`: on a 1280×800 window, the rail and the page panel are each
  shorter than the window, and neither touches the top edge.
