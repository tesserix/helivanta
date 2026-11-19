# Staff MFA recovery session enforcement candidate

For #1703 / #1749. This is not a released reset or enrollment journey.

The application session signer now takes an explicit recovery generation argument.
Every production mint caller and test fixture was updated deliberately; no
variadic/default argument can silently discard a generation. The signed claim is
an optional canonical UUIDv7. Empty means unadmitted, not an exemption from recovery.
The verifier checks its shape and carries it through the private principal and
expired-cookie renewal evidence. It never becomes native signed-session proof.
Ordinary principal JSON omits both recovery generation and native session ID.

The private identity credential projection now opts into current account-wide
recovery evidence. Login, renewal and protected request admission fail closed if
that evidence is missing or invalid. Explicit `none` requires no admitted
generation; `completed` requires the exact matching signed generation. Pending,
uncertain and enrollment-required states refuse access. Existing password cutoff,
global revocation, membership, cookie and idle controls remain in place. Renewal,
activity and tenant switching preserve the generation already verified.

Existing positive fixtures now explicitly declare no recovery, or the matching
completed generation when testing preservation. Their original assertions remain.
New negative cases cover missing evidence, malformed evidence, pending/enrollment
states and stale/legacy cookies. Neither production adapter supplies a default
`none` when the owner cannot answer. Both hosted owner-schema fixture pins now
match infrastructure `7bd817e6fdfec553ee95499faf6076f155a4907d` and SHA256
`844877964546ef1c936298837123c6f528543d8ca609effb4aa7fe566ab6b0a2`.
No live migration has been applied.

## Evidence and source review

Observed red before green: missing signer/claim support, missing principal mapping,
renewal/activity dropping the generation, and renewal returning 200 for pending,
missing or stale recovery evidence. New HTTP projection tests first failed on
missing recovery support. These assertions now pass with actual signed application
cookies and actual handlers; native/owner HTTP dependencies are simulated locally.
A correctly signed `null` recovery claim was also observed being accepted, then
fixed with strict scalar decoding; absence remains distinct from malformed input.

Local commands executed:

- Full race suites: `go test -race ./pkg/session ./pkg/authn ./internal/modules/iam/loginclient -count=1`.
- Scoped IAM race tests: renewal, generation-preserving activity, credential freshness,
  password-recovery replay and password-only refusal for retained MFA recovery.
- Scoped customer-onboarding race tests: exact identity projection, recovery evidence
  and minimal assignment-candidate purpose.
- `go test ./... -run '^$'` compiled every backend test package without running
  database/native integration. It is not runtime test evidence.
- `go vet ./...`, `golangci-lint run ./...`, `go build ./...` and
  `go run ./cmd/contractdocs -check` passed.
- The operational inventory checker and its 15 tests, shellcheck of both fixture
  scripts and the three identity-owner fixture-control tests passed.
- The canonical remote fixture schema was fetched and its pinned digest checked.

Re-reviewed the changed authentication principal, renewal, activity and private
projection diffs before refreshing seven source pins across five existing review
records. Registration, authority classifications and outstanding acceptance
obligations are unchanged. Credential-only recovery opt-in does not expand the
assignment-candidate/profile/membership purposes or disclose employer source data.
The regenerated ledger remains source classification, not runtime acceptance.

New real database tests for generation-preserving tenant switch and protected
admission in two legacy tenant contexts run in hosted CI, not locally. No local
application, PostgreSQL or container was started. No live identity or factor was
changed. The new JWT claim and source checks are not proof of a deployed feature.

## Current candidate verification

At application revision `3d00c4a8`, native recovered-login admission is wired in
`backend/internal/modules/iam/login_recovery.go` through the onboarding owner's
`AdmitStaffMFASession` contract. It requires a native session ID, validates the
returned generation, then reads current credential state again so a concurrent
reset or password change wins. This supersedes the earlier missing-admission
status; it does not establish native runtime or deployed acceptance.

Workforce exposes reviewed Staff reset, read and cancellation operations through
`backend/internal/modules/workforce/staff_mfa_recovery.go`. The candidate includes
the IAM retained intent/fence and the shared Staff recovery UI. The hospital and
independent-pharmacy native cases are authored but require a successful hosted run.

Additional local commands passed on 19 November:

- `pnpm --filter @helivanta/api test`: 133 files, 1,492 tests.
- `pnpm --filter @helivanta/e2e type-check` and
  `pnpm --filter @helivanta/e2e lint`.
- `pnpm exec prettier --check e2e/tests/support/staff-mfa-native.ts`.
- From `backend/`, `go build ./...`, `go vet ./internal/integration`, and
  `go test ./internal/integration -run '^TestStaffMFAFixtureTenantsReachTheCanonicalOwnerClient$' -count=1`.
- From `backend/`, `go test -race ./internal/customeronboarding ./internal/modules/iam/loginclient ./pkg/session ./pkg/organizationidentity ./pkg/authn -run 'MFA|Recovery|Credential' -count=1`.

The focused integration-package command tests the owner HTTP client fixture only;
it does not run PostgreSQL or native recovery. Node reported version 26.13.1 against
the repository's required Node 24 range; hosted Node 24 results remain required.
No local application, database or container was started. `go build ./...` emitted
no application binaries into the worktree.

The earlier hosted run `44488449145` failed an independent Staff setup assertion:
creation returned the documented 201 while the test expected 200. The correction
preserves the API and now checks 201 creation followed by 200 identical replay for
both proposals and decisions. Hosted run `44508111060` tests the pushed correction;
its results must be recorded after completion. Earlier employer-setup and lockout
failures are not considered resolved without those runtime results.

## Remaining release gates

Require full hosted database and native-browser acceptance for both employer
models, password-reset-to-login, lockout guidance and forced replacement MFA
before enabling recovery. Resolve the password-recovery prerequisite's GitGuardian
incident 40266894 through the security dashboard. Its reported source at commit
`068c9bfa`, `apps/shell/components/password-recovery.tsx:66`, initializes empty form
values; it contains no credential. This source review is not a provider dismissal
or a passing security check.

Follow [the schema-first rollout and acceptance plan](../deployment/2026-11-19-staff-recovery-rollout.md)
for approved live migrations, owner/application Kargo releases, named QA factor
actions and deployed Chrome/API verification. Keep #1703 open until every issue
acceptance criterion and live release gate is evidenced.
