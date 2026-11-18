# Account-name UI verification — 18 November 2026

The reported Yes Hospital membership table exposed identity subjects and internal
role keys. Account identifiers remain command bindings; user-facing account labels
use verified human names and readable organization roles.

## Scope and authority

Identity service owns names and email; IAM owns memberships and role labels. The
new account presentation read uses the authenticated Global API platform route,
`iam.member.manage` and current tenant-admin membership. It verifies native tenant
ownership through the existing identity-owner assignment-candidate projection.
It creates no identity, membership, reviewer appointment or operating grant.
The strict current-member presentation API remains separate.

The shell batches rendered accounts in tenant/actor-bound queries. Lookup refusal
withdraws names. Unknown accounts show an explicit unavailable state. Named
choices preserve qualified internal values only in commands; an editable choice
is cleared when it is no longer verifiable. Retained submitted commands remain
frozen for reconciliation. Initial enrollment is bound to verified Staff account
setup rather than a free-text identity-provider identifier.

The review includes membership/history/confirmation rows, account links,
assignment selectors, pharmacy and supply operations, declarations, workforce
history, accessible labels and shared fact fields. The ESLint rule rejects direct
raw-account text, tooltips, accessible labels and Fact values. Indirect aliases
and source-qualified option mappings were also inspected. The ambulance app has
no matching account-identity display expressions in its TSX sources.

## Local evidence

No local application, database, Docker or Colima was started. Validation uses
unit/component tests, compilation, static checks and production builds only.

Commands run from the repository root unless otherwise stated:

- `pnpm --filter @helivanta/api test` — 1,471 tests passed.
- `pnpm --filter @helivanta/ui test` — 330 tests passed, including stale editable
  selection withdrawal, lookup failure, batching and tenant/actor switching.
- `pnpm --filter @helivanta/config test` — 95 tests passed.
- `pnpm --filter @helivanta/shell exec vitest run --maxWorkers=4` — all 2,104
  tests passed across 350 suites. Named choices retain assertions for internal
  command identities, scopes, retry bodies and refusal behavior.
- API/UI/Shell type checks and canonical generators were run. Shell/E2E type
  checks passed; E2E scenarios are not executed on the local machine.
- ESLint and package Prettier checks were run. The existing signup effect
  dependency warning is outside this change.
- The Staff operational-access inventory's fifteen tests passed. Source reviews
  for both presentation routes were updated and the generated inventory checked.
- From `backend/`: the isolated authentication unit passed; `go build ./...`, IAM
  vet, contractdocs generation and `go run ./cmd/contractdocs -check` were run.
  Real-database integration cases are reserved for hosted CI.
- The production Shell build uses non-secret CI-shaped API/Zitadel configuration.
  The final build passed. Generated `.next` and task-created local TypeScript
  build-info artifacts were removed before merge; dependencies and caches were
  preserved.

## Hosted and live acceptance

The E2E scenario checks pre-enrollment native names without invented roles,
foreign-organization concealment and rejection, and named enrollment history.
Hosted CI must pass before merge; source-ledger classification is not runtime
acceptance. Production deployment and browser evidence are recorded on
[PR #1740](https://github.com/tesserix/helivanta/pull/1740) after release. No additional Staff permissions or memberships are created as part of
this display correction.

## CI follow-up

The first hosted browser run passed 99 scenarios, including the revised Staff
account-name enrollment scenario. Hospital pharmacy setup created its outlet but
exposed a save-navigation race: outlet URL normalization could run before the new
detail address committed. Successful-save navigation now participates in the
existing pending-navigation context. Its regression failed before the fix; the
60 focused list/detail and pharmacy workspace tests and all 330 shared UI tests
passed afterward. The production build, type checks and lint passed again.

The repository owner approved a temporary exception for tooling advisory
GHSA-fc5q-39xc-vjpf. See the vulnerability-management runbook for the finding
paths and remaining risk. The audit passes with that exception; three deterministic
boundary tests cover its expiry gate, which rejects the exception from
26 November 2026 at 00:00 UTC onward.

## Exhaustive display and fixture follow-up

The browser fixtures now select verified synthetic staff by name instead of typing
subject identifiers. Their IAM display metadata is isolated from Emergency domain
commands; those commands still run through the real Go service fixture. The
extended lint regression rejects visible raw identity-entry inputs and actor
fields named `recorded_by`, `authorised_by` and `requested_by`. Hidden command
bindings remain allowed. The full Shell suite passed 2,104 tests and configuration
suite passed 95 tests after this sweep; type checks, lint and production build
passed again.

The independent-provider grant-draft screen has no published named hospital
recipient projection. It now explicitly blocks fresh named-account proposals
instead of asking a supplier for a private hospital account identifier or querying
a foreign private directory. Retained-command reconciliation remains available.
This advanced screen is behind the ineligible `supplier_collaboration` capability;
it is not represented as part of the released owner/PharmaConnect MVP.

The complete hosted Go run found a new assertion expecting `Staff member` rather
than the existing canonical label `Staff member (membership only)`. The assertion
was corrected without changing the role definition. The early presentation test
step now includes account-presentation tests as well as member-presentation tests;
the full race/coverage gate remains required. That run's browser failure was an
identity-service timeout during pharmacy fixture setup; it is not counted as a
passing browser run.

## Staff membership reference follow-up

The authenticated owner check confirmed named membership rows, enrollment actors
and reviewer choices. It also exposed technical receipt and membership references
in the enrollment cards. Those references are now omitted from enrollment and
membership-change history and confirmations. Membership generation is omitted from
the confirmation summary, and recovery describes ordinary staff membership using
readable wording.

The API models, receipt validation, command IDs, recovery basis and membership
epochs remain intact. Regression tests assert that names, roles, reasons and dates
remain visible while identifiers stay out of rendered text. Existing tests still
verify exact retry bodies, tenant binding and recovery concurrency values. The
hosted enrollment browser scenario now compares returned receipt and membership
IDs with the history region to ensure neither is displayed.

Both regression cycles failed against the previous rendering, then the 100 affected
enrollment, membership, access-page and account-initiation tests passed. Shell and
E2E type checks and the production Shell build passed on Node 24. Formatting and
the operational-access inventory check passed; lint retains only the existing
signup effect dependency warning. The full Shell suite passed 2,105 tests on Node 24. Generated build and TypeScript build-info files were removed after validation.
No local application, database or container was started.
