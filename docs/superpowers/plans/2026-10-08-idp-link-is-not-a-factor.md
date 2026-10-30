# Plan — A linked external IdP is not a second factor (#950)

Spec: `docs/superpowers/specs/2026-10-08-idp-link-is-not-a-factor-design.md`

## Task 1 — tests first, proven to fail

- [x] Unit (`sufficiency_test.go`): `PASSWORD`+`IDP` completes with finalize
      called; `PASSWORD`+`IDP`+`TOTP` asks for TOTP; `PASSWORD`+`IDP`+`OTP_EMAIL`
      still refused. `CompleteAfterFactor`: `PASSWORD`+`IDP`+`TOTP` verified
      completes; with `OTP_EMAIL` also enrolled, refused and not finalized.
- [x] Integration (`loginui_integration_test.go`): throwaway Google-shaped IdP
      + throwaway user + `POST /v2/users/{id}/links`; password login answers
      200 with `callback_url` carrying `code` and `state`. IdP deleted and
      verified gone in cleanup.
- [x] Run against unchanged code; record the failures.

## Task 2 — classification

- [x] `idpMethodType` constant in `client.go`, with the reasoning at the
      decision point.
- [x] `classifyEnrolledMethods`: `IDP` is neutral, like `PASSWORD`.
- [x] `totpMethodType` and `nonPasswordFactorPrefix` doc comments corrected.

## Task 3 — docs

- [x] 2026-08-16 spec: D3 IdP row and D4 "no external IdP is configured"
      premise corrected to point at the new spec.
- [x] 2026-08-17 spec: D1 IdP row and D6 premise corrected.
- [x] 2026-10-07 spec: `RefusalFactorUnsupported` row and "not covered" entry
      corrected.

## Mutations (each must make the suite fail, then be reverted)

- `IDP` put back in the refusal bucket.
- `OTP_EMAIL` made neutral alongside `IDP`.

## Gates

- `make lint-go`, `go test -race ./...`, `./scripts/coverage-gate.sh`.
