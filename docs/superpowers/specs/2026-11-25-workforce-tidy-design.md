# Workforce: tidy pages to the one-look design — design

Issue: #1261 (follows #1255 one look, #1275 one panel surface, #1834 tokens)
Date: 2026-11-25

## Why

An audit on 2026-11-25 found the following:

- Every Workforce page already opens with `PageHeading`.
- Lint already blocks eyebrows, display type, raw colours and pills.
- What remains is surfaces: 68 Workforce files are listed in `apps/shell/surface-legacy.mjs` and still paint their own cards, often with cards inside cards.
- A copied input class, `inputClass` (`w-full rounded-lg border …` with an accent focus ring), is itself a card under the surface rule. 71 files use it, so no file can leave the legacy list while it remains.
- Tables have about 36px rows, left-aligned numbers, uppercase headers, and no sticky header or row hover.
- 13 page descriptions run to several sentences.

## Decisions

### D1 One control style, from @helivanta/ui

`controlClass` is exported from `@helivanta/ui` and is the only class string for a text-like input, select trigger or textarea that is not a `@tesserix/web` component:

- **Shape:** `rounded-md`, a control radius (D3 of #1255), so the surface rule does not read it as a card.
- **Border:** `border-input`.
- **Height:** `min-h-11`, a 44px target.
- **Focus:** the 2px `--ring` with an offset, the same ring as the design system's own controls. Not the tenant accent.

The Workforce copies of `inputClass` become imports of it. A test pins it against the lint's `SURFACE_PATTERNS` and requires the `--ring` focus, so the copies cannot drift back. Copies in other zones move when their own tidy issue runs (#1256, #1257, #1259, #1260, #1262, #1263).

### D2 One table style

`tableStyles` is exported beside it. It is used by each slice's tables:

| Part   | Classes                                                                                                 |
| ------ | ------------------------------------------------------------------------------------------------------- |
| header | `sticky top-0 z-10 bg-background`, sentence case, `text-left text-xs font-medium text-muted-foreground` |
| row    | `h-11 border-b border-border hover:bg-muted/50`                                                         |
| number | `text-right tabular-nums`                                                                               |

### D3 Slices

Each slice is one PR under #1261. It removes its files from `SURFACE_LEGACY`, after which lint and `surface-legacy.test.mjs` keep them out. In every slice:

- cards inside cards become `Panel` with dividers or a `--muted` inset;
- tables use D2;
- each description is cut to one line;
- `noValidate` and native `required`/`maxLength` are fixed;
- the stale `scroll-mt-24` offsets are dropped.

The slices, in order:

0. Shared primitives (this document's D1 and D2).
1. Directory and credentials.
2. Leave.
3. Rosters.
4. Attendance, overtime and duty exchanges.
5. Payroll.
6. Benefits.
7. Access, reviews and postings.

#1261 closes with slice 7.

## Not covered

- Copy changes beyond shortening descriptions. Button and toast verbs already match.
- Other zones' copies of `inputClass`.
