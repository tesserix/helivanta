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
- `pnpm --filter @helivanta/config test` — 91 tests passed.
- Shell component suites exercise named choices while asserting internal command
  identities, scopes, retained retry bodies and refusal behavior. Final full-run
  evidence is recorded below after completion.
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
  Generated `.next` and local TypeScript build-info artifacts are removed after
  the final build succeeds, before merge.

## Hosted and live acceptance

The E2E scenario checks pre-enrollment native names without invented roles,
foreign-organization concealment and rejection, and named enrollment history.
Hosted CI must pass before merge; source-ledger classification is not runtime
acceptance. Production deployment and browser evidence are recorded below after
release. No additional Staff permissions or memberships are created as part of
this display correction.
