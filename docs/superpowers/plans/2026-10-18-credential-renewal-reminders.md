# Credential renewal reminders — plan

Spec: [2026-10-18-credential-renewal-reminders-design.md](../specs/2026-10-18-credential-renewal-reminders-design.md). Issue: #1108 (slice B).

1. **Contract.** Add the policy and reminder shapes to `contracts/workforce-credentials.json` and regenerate the Go and TypeScript contracts.
2. **Migration.** Add `credential_reminders.sql` as `0031_workforce_credential_reminders`: append-only policies, reminders unique per revision and threshold, current-revision and close-once triggers, forced RLS, audit and no-delete, and per-credential worker policies with the widened outbox boundary.
3. **Workflow.** Add the advance activity, the sleeping workflow with continue-as-new and bounded retries, and the `credential_validity_changed` consumer; register the task queue on the document worker.
4. **Handlers.** Policy GET/PUT, the paginated inbox and acknowledgement, and supersession of older revisions inside the approval transaction.
5. **Tests.** Pure due-date and wake tests across zones and leap days; workflow tests on the test clock; real PostgreSQL tests for the API and for the worker role's confinement.
6. **Archtest.** Matrix, unpaginated allowlist and event-field classification.
7. **Client** (`packages/api/src/credentials.ts`): paths, the days form schema and receipt-checked writes, with tests.
8. **Shell.** `/workforce/credential-reminders` with the policy form and the inbox, its zone entry, and tests.
9. **Docs:** API reference and coverage row.
10. **Gates**, then the PR, the CI cycle, merge and deploy verification, including the replay check in the spec.
