# Plan: no unscoped MFA hint on the login form (#917)

Spec: `docs/superpowers/specs/2026-10-08-no-unscoped-mfa-hint-design.md`

- [x] `loginclient`:
  - `InstanceLoginPolicyForDisplay` deleted;
  - `loginPolicy(ctx, orgID)` always sends `withOrgID`;
  - `withOrgID`'s doc names both of its callers;
  - the test that pinned the unscoped read is removed.
- [x] `loginui.go`: `AuthRequest` makes no policy read, and
  `authPoliciesResponse` loses `RequireMFA`.
- [x] `archtest`: `TestSufficiencyNeverReferencesInstanceLoginPolicyForDisplay`
  is replaced by `TestLoginClientExposesOnlyAnOrgScopedPolicyRead`, a
  reflection check over `*loginclient.Client`. Re-adding an unscoped reader
  method was shown to fail it.
- [x] Tests:
  - `TestAuthRequestReadsNoLoginPolicy` replaces the two `require_mfa` tests.
    It fails if the policy route is reached, and was shown to fail when a read
    was put back.
  - AuthRequest-only fixtures no longer register a policy route.
- [x] Shell: `require_mfa` is removed from `AuthPoliciesInfo`,
  `toMethodPolicy` and the test fixture.
- [x] Docs: supersession notes on the org-scope spec (D3) and the native-MFA
  spec (D5).
- [x] Gates: `make lint-go`, `go test -race ./...`,
  `./scripts/coverage-gate.sh`, and
  `pnpm turbo lint type-check test build format:check`.
