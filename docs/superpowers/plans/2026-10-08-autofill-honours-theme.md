# Plan: autofilled fields honour the theme (#858)

Spec: `docs/superpowers/specs/2026-10-08-autofill-honours-theme-design.md`

- [x] Reproduce Chrome's real autofill state through CDP `Autofill.trigger`.
  It measured `(232,240,254)` with black text, matching the issue.
- [x] `packages/ui/styles.css`: the `:-webkit-autofill` and `:autofill`
  rules (spec D1–D4).
- [x] Re-measure in both themes: pixels equal the tokens, and the focus ring
  is kept.
- [x] `packages/ui/src/autofill-styles.test.ts`, plus five mutations.
- [x] Gate: `pnpm turbo lint type-check test build format:check`.
