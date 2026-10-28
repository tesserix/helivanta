# Staff identity review source preparation

This is a bounded preparation slice for #1399 and #417. It does not complete
identity/background/photo reviews, evidence release, cards, employment exits,
payroll runs or generated employment documents. Keep the full
[Staff acceptance ledger](staff-management-acceptance.md) open.

## Authority and ownership

Workforce returns its existing Staff/profile identity source. IAM owns reviewed
hospital assignments, current membership/credential authority, scope epochs and
OpenFGA projection. Either `workforce.identity_manager.v1` or
`workforce.identity_reviewer.v1` permits the source read, with an active reviewed
Staff/login binding. Tenant administration, directory access, a job category or a
Staff UUID alone confers no identity-source authority. An unavailable check fails
closed; only an authoritative denial permits trying the other HR template.
The chosen grant and actor binding remain guarded until the sensitive-read audit
commits. The handler responds after that commit.

The fingerprint binds tenant, hospital, Staff ID, employee code, display/given/
family names and DOB. Contact changes do not invalidate it. The database-owned
employment generation increments for every employment-status transition, including
inactive-to-active. Initial generation 1 describes the observed record and does
not invent an employment period or hire history. A source read performs no review
and issues no identity card. Missing profile fields remain explicitly unrecorded.

The canonical contracts are `staff-access.json` and
`workforce-identity-checks.json`; generators produce Go/Zod and IAM template
metadata/snapshot constraints. The existing assignment list remains leave-only;
the additive identity list preserves independent pagination. See
[ADR-0048](../adr/0048-staff-identity-review-authority.md),
[Workforce API](../api/workforce.md), and ORG-AC-01/02/04/05/06/07/08.

## Local evidence

Synthetic UI checks use the existing local Staff QA hospital and three distinct
accounts: maker, independent approver and recipient. Credentials and screenshots
remain outside Git in the authorized private fixture directory.

- Staff Admin without a scoped HR assignment: source disclosure denied.
- Maker proposes a hospital identity-manager assignment in Staff access; maker
  cannot approve its own proposal. Independent approver approves; hospital
  verification and OpenFGA activation complete before Active is displayed.
- Recipient has a directory-reader setup fixture, with zero profile Edit buttons.
  This fixture establishes directory visibility only; business assignment and
  account-link commands use the UI.
- An active HR assignment with a revoked Staff/login binding still cannot read the
  source. The two administrators propose and independently approve a new link in
  the UI; the old revoked link and its history remain retained.
- Fresh recipient login can read the current source, refresh and reload it, with
  no fabricated identity/background/photo approval. Maker withdraws the assignment
  in the UI; fresh recipient login is denied the identity read again.
- A separate reviewer-template proposal/independent approval exercises the same
  source permission without granting management, payroll or clinical authority.

Real-database tests cover fingerprint stability/change, employment generations,
counter forgery rejection, missing/foreign targets, unavailable authority, inactive
actors, both scoped templates, metadata-only read audit, no-store responses and
rollback when authority is denied after the product read. IAM's real
PostgreSQL/OpenFGA/NATS tests cover both templates, unrelated permission denial,
legacy leave-list isolation, current membership/scope epochs, expiry, revocation,
projection failures and guarded commands through the Global API.

Commands run during this slice include:

```sh
node scripts/generate-staff-access.mjs --check
node scripts/generate-workforce-identity.mjs --check
# backend/
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
go test -race ./internal/modules/workforce -run '^TestIdentitySource' -count=1
go test -race ./internal/modules/iam -run '^Test(IdentityTemplateScope|AssignmentProjectionAcknowledgesOnlyCurrentScope)$' -count=1
go test -race -count=1 -cover ./internal/modules/workforce ./internal/globalapi
golangci-lint run ./...
# repository root, Node 24 and configured local Global API origin
pnpm turbo lint format:check type-check test build
```

Full-suite attempts and isolated reruns are retained in local logs. An initial
parallel backend run lost host-to-VM SSH forwarding; containers remained healthy
and forwarding was restored without container restart or data deletion. A bounded
full run subsequently reported the existing leave near-expiry wait and attendance
history ordering tests; both owner packages passed when rerun alone. Do not
represent those earlier failed commands as successful runs. Final candidate CI
and release checks remain independently required.

## Rollout boundaries

The additive migrations are `0014_iam_staff_templates` and
`0048_workforce_identity_source`. Retain existing versioned template permission
sets, source generations and audit history on rollback. Stop the consumer or
forward-fix; do not edit applied migrations or reset employment generations.
Neither this document nor a successful source read approves production rollout,
case-policy activation, government identity verification or card issuance.

The case state machine, protected evidence/photo consumer, independent case
review, card issuance/reissue/revocation, offboarding/rehire and payroll/letter
workflows remain required. No full Staff or #1399 end-to-end completion label is
justified by this preparation slice.
