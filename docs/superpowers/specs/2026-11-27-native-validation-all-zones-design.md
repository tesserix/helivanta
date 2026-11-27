# No native form validation in any zone

**Issue:** [#1880](https://github.com/tesserix/helivanta/issues/1880), follow-up to
[#1855](https://github.com/tesserix/helivanta/issues/1855) and its design,
[`2026-11-26-auth-forms-design.md`](2026-11-26-auth-forms-design.md).

## Why

frontend.md §4 bans native browser validation: forms validate with `useZodForm` +
`Field`, set `noValidate`, and show inline zod errors. #1855 added the
`hmsNoNativeValidation` lint rule and applied it to the auth surfaces only. Widening
it to `apps/shell/app/**` and `components/**` on 2026-11-27 reports 73 findings in
40 files:

- 57 native `required`, `minLength`, `maxLength` or `pattern` attributes;
- 13 `<form>` elements without `noValidate`;
- 3 `type="email"` or `type="url"` inputs.

A native `maxLength` silently truncates a paste, and a native bubble bypasses the
inline error the page shows. Either way the person never sees the rule that
refused them.

## Decisions

- **D1: remove only what zod already enforces.** An attribute is removed when the
  form's schema (including a generated contract schema such as
  `StaffPostingDraftInputContract.shape.reason`) enforces the same rule. Follow
  imports to the schema; a matching label is not evidence.
- **D2: otherwise tighten, but only to the backend contract.** If the schema is
  looser, add the rule to the schema only when the backend contract (generated
  schema, handler validation or migration CHECK) already enforces it, and cite
  where. Use the error copy the field already shows. A new string goes to the
  product owner before merge.
- **D3: otherwise keep the gap visible.** A rule that neither zod nor the backend
  enforces is recorded on #1880. The attribute is removed only if keeping it would
  hide nothing the backend refuses, which the record says. Nothing is silently
  dropped.
- **D4: `type="email"`/`"url"`** become `type="text"` with `inputMode`, and the
  schema validates the format, as #1855 did for auth.
- **D5: every `<form>` sets `noValidate`.**
- **D6: the lint rule covers `app/**` and `components/**`**, so a regression is a
  lint error. Tests stay exempt, as the rule already defines.

- **D7: generated limits carry a message.** With the native `maxLength` gone, a
  long paste reaches zod, and a generated `.max(N)` without a message showed
  zod's default ("Too big: expected string to have <=500 characters"). Both
  contract generators (`scripts/lib/contract-codegen.mjs`,
  `scripts/lib/generate-contract.mjs`) now emit `"Use at most N characters."` for
  a string limit, the wording the pharmacy contract already used. An explicit
  `max_message` still wins. The staff-governance generator, which rewrites the
  emitted limit into a UTF-16 check, now fails if the text it rewrites is missing,
  instead of silently shipping zod's code-point limit.

## Tests

For each form an attribute was removed from, a Vitest test using
`renderWithProviders` enters the overlong (or empty) value. It asserts the inline
zod error and that no API request was sent. Each new test is shown to fail with the
schema rule removed.

## Not covered

- Native constraints outside forms (none were found).
- Pharmacy files in Sam123ben's open draft #1886 (`counter-sale.tsx`) are changed
  here too. The edits are attribute removals, so whichever merges second has a
  small, mechanical conflict.
