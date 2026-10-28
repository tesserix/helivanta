# PharmaConnect pages share one width

Resolves #1366. Supersedes D8 of the [detail-pane spec](2026-10-23-detail-pane-design.md)
and E0 of the [all-pages spec](2026-10-24-detail-pane-all-pages-design.md).

## Why

E0 made each page with a detail pane full width (up to 1760px) and left the rest at the
1150px cap. In PharmaConnect, that left Overview, Dispense queue and Medication list
narrow, so on a large screen the content jumped about 600px when moving between pages.
The user asked for the pharmacy pages to be consistent.

## Decisions

- **Z1 Width is a zone's choice, not a page's.** `width?: "full"` moves from `ZonePage` to
  `Zone`. A page cannot differ from its zone; that is a type error, not a convention.
- **Z2 PharmaConnect is full width,** all nine pages. Other zones keep the cap: no zone
  other than PharmaConnect was asked about.
- **Z3 Lists and panels fill the width.** Medication list dropped its own 672px panel cap.
  Prose (about 70ch) and forms (about 640px) keep D8's reading limits.
- **Z4 Enforcement.** `zones.test.ts` pins which zones are full width. `hms-shell.test.tsx`
  checks that a full-width zone's Overview, a page with no pane, is full width too.
  `navigation.spec.ts` loads every PharmaConnect page at 1920px. It checks that the content
  width is the same on each page and that a block on each page fills it.

## Not covered

- The other zones' widths. A zone moves to full width when someone asks for it.
- The layout inside each page (headings, pickers, spacing), which the user did not raise.
