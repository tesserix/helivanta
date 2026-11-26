# EmergencyConnect: tidy pages to the one-look design — design

Issue: #1262 (follows #1255 one look; applies #1261's design)
Date: 2026-11-26

## Why

EmergencyConnect pages already open with `PageHeading`, and lint already blocks eyebrows, display type, raw colours and pills. Two problems remain:

- The last 10 files in `apps/shell/surface-legacy.mjs` are EmergencyConnect components (`components/emergency/`). They still paint their own cards.
- Native `required` and `maxLength` attributes remain in the crew, dispatch and receiving forms, although native browser validation is banned (frontend.md).

## Decisions

### D1 Reuse #1261's primitives, unchanged

D1–D3 of `2026-11-25-workforce-tidy-design.md` apply:

- `Panel` with insets for nested surfaces, and divided lists in place of boxed rows;
- `controlClass` for controls;
- `tableStyles` for tables.

Each native attribute is removed only after confirming that the form's zod schema enforces the same rule. A rule that zod does not enforce is reported, not invented.

### D2 Safety copy stays

This zone records dispatch, crew acknowledgement and physical handover. The tidy changes surfaces only.

- No status label, instruction, acknowledgement or confirmation text changes.
- The crew page's description has two sentences. Its second sentence states the departure rule ("Both crew members must acknowledge before departure."), so it stays, and the description is left as it is.
- The other descriptions are already one sentence.

### D3 The legacy list is retired

After this change no file paints its own surface outside the lint rule, so the list is no longer needed:

- `apps/shell/surface-legacy.mjs` and `surface-legacy.test.mjs` are deleted.
- The ESLint configuration stops reading the list.

From then on, a hand-painted card anywhere in the covered zones is a lint error, with no exemption list to add it to.

## Not covered

- No behaviour, API, permission or copy change.
- Not checked visually.
