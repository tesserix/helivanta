# Plan: the log redactor masks Aadhaar numbers, not every 12-digit number (#840)

Spec: `docs/superpowers/specs/2026-10-08-redaction-bounds-design.md`

- [x] `logging.go`:
  - `durationsAsText` as the JSON handler's `ReplaceAttr` (D1);
  - the package doc states that PHI is screened and secrets are not (D4).
- [x] `redact.go`:
  - Aadhaar core `[2-9]…`, plus `verhoeffValid` (D2);
  - `scanPattern`, built on the `first`/`rest` forms of `bounded()`,
    replaces the fixpoint loop (D3).
- [x] `cmd/api/main.go`: the "everything is screened" sentence now says
  PHI, not secrets.
- [x] Fixtures: `123456789012` (never an Aadhaar) becomes `234567890124`
  (synthetic, Verhoeff-valid), in all forms.
- [x] Tests per spec §Tests: `redact_bounds_test.go` and
  `verhoeff_internal_test.go`.
- [x] Mutations, each shown failing and then reverted:
  - no `ReplaceAttr`;
  - no checksum;
  - checksum rejects everything;
  - any leading digit;
  - a declined candidate consumes the separator.

  Dropping `^` from `rest` is an equivalent mutant. A candidate always ends
  on a digit followed by a non-digit, and every pattern starts with four
  digits or `+91`, so no match can start at the resume point. The `rest`
  form is defence in depth.
- [x] Gates:
  - `golangci-lint`: 0 issues;
  - `./scripts/coverage-gate.sh`: green, with `pkg/logging` at 90.6%.
