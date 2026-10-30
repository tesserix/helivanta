# Autofilled fields honour the theme

**Issue:** [#858](https://github.com/tesserix/helivanta/issues/858)
**Amends:** `packages/ui/styles.css`, the token layer every zone app loads.

## The problem, stated precisely

When Chrome autofills a field, a user-agent rule paints it with Chrome's own
background and text colour, with `!important`. On the dark login card that
gives near-white fields with black text, measured at `rgb(232,240,254)` and
`rgb(0,0,0)`. Anyone with saved credentials sees it on every sign-in. Safari
shares the `-webkit-autofill` mechanism. Firefox applies a `filter` tint to
`:autofill` fields instead.

## Decisions

### D1: Fix it at the token layer, so every form inherits it

The rules live in `packages/ui/styles.css`, beside the tokens, and select on
`input`, `textarea` and `select`. That covers every form in every zone, not
just the login page.

### D2: Every colour is a token

`@tesserix/web`'s Input is `bg-background` with inherited foreground text,
so the autofill state uses the same two tokens:
- `-webkit-text-fill-color` and `caret-color` are `var(--foreground)`;
- `inset 0 0 0 1000px var(--background)` paints over the UA background,
  which cannot be overridden directly.

A token change therefore moves autofill with it.

### D3: The focus ring survives

The Input draws its focus ring (`focus-visible:ring-4`) and `shadow-sm` with
`box-shadow`. The inset is **appended** to Tailwind's
`--tw-ring-offset-shadow`, `--tw-ring-shadow` and `--tw-shadow` stack, so an
autofilled field still shows the ring when tabbed into. A bare
`box-shadow: inset …` would erase it.

### D4: Two rules, not one selector list

A browser drops a whole rule whose selector list contains a pseudo-class it
does not know. So `:-webkit-autofill` (Chrome, Safari) and `:autofill`
(Firefox, standard) each get their own rule. The `:autofill` rule also sets
`filter: none`, which removes Firefox's tint.

## Not covered

- **`color-scheme`.** Declaring `color-scheme: dark` for the dark theme would
  also restyle native scrollbars and pickers. That is a separate, wider
  change, and this fix does not depend on it.
- **Firefox and Safari** were not run here (CI and this environment are
  Chromium). Safari takes the same `-webkit-autofill` path. Firefox's path is
  the standard `:autofill` plus `filter: none`, per its UA stylesheet.

## Verification

**Real Chrome autofill state**, not a simulated class. Chromium 1194 filled a
field through the DevTools protocol's `Autofill.trigger`, and
`:-webkit-autofill` matched. The page used the real `styles.css` and an
input styled like `@tesserix/web`'s.

| | Field background (pixel) | Text fill | Focus ring |
|---|---|---|---|
| dark, before | `(232,240,254)` | `rgb(0,0,0)` | present |
| dark, after | `(15,19,23)` = `--background` | `rgb(232,237,242)` = `--foreground` | present |
| light, before | `(232,240,254)` | `rgb(0,0,0)` | present |
| light, after | `(244,246,249)` = `--background` | `rgb(15,23,40)` = `--foreground` | present |

The contrast of the applied token pairs is 15.8:1 (dark) and 16.5:1 (light),
both well above WCAG AA's 4.5:1.

**Regression test, `packages/ui/src/autofill-styles.test.ts`.** For both
rules it asserts:
- token-only colours;
- no literal colour, with Tailwind's transparent fallbacks set aside;
- the three Tailwind shadow variables;
- separate rules;
- Firefox's `filter: none`.

Five mutations each fail it:
- a hardcoded background;
- an added literal text colour;
- the ring variable dropped;
- the selector lists merged;
- the filter removed.
