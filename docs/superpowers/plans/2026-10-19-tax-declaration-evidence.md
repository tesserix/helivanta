# Tax declaration evidence — plan

Spec: [2026-10-19-tax-declaration-evidence-design.md](../specs/2026-10-19-tax-declaration-evidence-design.md). Issue: #1041.

1. **Contract.** Add `taxEvidenceInput` to `contracts/workforce-payroll.json` and regenerate.
2. **Migration.** Add `tax_declaration_evidence.sql` as `0029_workforce_tax_declaration_evidence`. It adds the `tax_declaration` record kind, the `frozen` column, the creation, freeze and decision triggers, and the payroll branch of `document_isolation`. Register it in `module.go`.
3. **Gate refactor.** Move the HR document handlers behind `documentGate` so the shared register, upload, retry and download code takes its authorisation and record binding from the route family.
4. **Handlers.** Add the five evidence routes in `tax_declaration_evidence.go`. Call `settleDeclarationEvidence` before every revision decision. Map `evidence_scanning`, `evidence_limit` and `not_pending`.
5. **Integration tests** in `tax_declaration_evidence_test.go`. They upload and scan through the real worker, then download. They also cover separation from the HR routes, tenant, hospital and staff denials, the limit and settlement, and direct-SQL guards.
6. **Client** (`packages/api`): evidence schema, upload, retry, download and a polling list hook, with tests.
7. **Shell.** Add the evidence section under the in-force declaration and the pending proposal, with uploads and inline errors. Hold decisions while files scan, with tests.
8. **Docs:** API reference, coverage row, spec.
9. **Gates**, then the PR, the CI cycle, merge and deploy verification.
