# Credential validity reviews — plan

Spec: [2026-10-20-credential-validity-design.md](../specs/2026-10-20-credential-validity-design.md). Issue: #1108 (slice A).

1. **Contract.** Add `contracts/workforce-credentials.json` and `scripts/generate-workforce-credentials.mjs`; chain its `--check` into the `@helivanta/api` type-check.
2. **Migration.** Add `credential_validity.sql` as `0030_workforce_credential_validity`: reviews and validity tables, proposal, decision, write and reviewed-field triggers, forced RLS and no-delete. Register it in `module.go`.
3. **Handlers.** Add the six routes in `credential_validity.go` under the enrollment advisory lock, the `workforce.credentials.review` permission, and map the enrollment 409 `credential_reviewed`.
4. **Tests.** Pure status tests across time zones and leap days; real PostgreSQL tests for the workflow, evidence, enrollment bypass, scoping, register order and direct-SQL guards.
5. **Archtest.** Matrix, unpaginated allowlist and event-field classification.
6. **Client** (`packages/api/src/credentials.ts`): paths, form schemas, evidence filter and receipt-checked writes, with tests.
7. **Shell.** Credentials panel on the staff directory and the `/workforce/credentials` register page, with tests.
8. **Docs:** API reference and coverage row.
9. **Gates**, then the PR, the CI cycle, merge and deploy verification.
