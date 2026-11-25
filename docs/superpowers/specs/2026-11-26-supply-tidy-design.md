# Supply Connect: tidy pages to the one-look design — design

Issue: #1260 (follows #1255 one look; applies #1261's design)
Date: 2026-11-26

## Why

Supply Connect (`/supply`: parties and staff) already opens every page with `PageHeading`, and lint already blocks eyebrows, display type, raw colours and pills. What remains is the same as Workforce had before #1261:

- 13 files in `components/supply/` are listed in `apps/shell/surface-legacy.mjs` and still paint their own cards, often cards inside cards.
- Several inputs carry a hand-written `rounded-lg border` class instead of the shared control style.
- The warehouse stock table has neither the 44px rows nor the right-aligned numbers.
- Five native `required`/`maxLength` attributes remain, although native browser validation is banned (frontend.md).
- The parties page description runs to two sentences.

## Decisions

### D1 Reuse #1261's primitives

There are no new primitives. The decisions D1–D3 of `2026-11-25-workforce-tidy-design.md` apply unchanged:

- `controlClass` is used for text-like controls.
- `tableStyles` is used for tables (header, row, number).
- `Panel` draws surfaces. A section inside a panel renders as an inset; boxed list items become divided lists.

### D2 One PR

The zone is small: 19 component files and two route pages. It ships as one slice, which:

- removes its 13 files from `SURFACE_LEGACY` and lowers the cap in `surface-legacy.test.mjs` from 23 to 10;
- applies D1 to every Supply Connect file, not only the exempt ones;
- removes each native `required`/`maxLength` only after confirming that the form's zod schema enforces the same rule. A rule zod does not enforce is reported, not invented;
- cuts the parties description to one sentence. A cut sentence that states a real rule moves beside the control it governs.

Tests assert:

- that no hand-painted card class survives on each changed render path;
- the table row, header and number classes;
- that each form without its native attributes still shows the inline zod error and does not call the API.

Each new assertion is shown to fail against the old source.

## Not covered

- No behaviour, API, permission or copy change beyond the description.
- EmergencyConnect's 10 exempt files are #1262.
