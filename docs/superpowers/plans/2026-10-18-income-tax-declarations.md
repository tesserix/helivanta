# Staff income-tax declarations — plan

Spec: [2026-10-18-income-tax-declarations-design.md](../specs/2026-10-18-income-tax-declarations-design.md). Issue: #1041.

1. **Contract.** Add `taxDeclarationClaim`, `taxDeclarationPanChange`, `taxDeclarationLandlordInput`, `taxDeclarationLenderInput`, `taxDeclarationHraInput`, `taxDeclarationHomeLoanInput`, `taxDeclarationInput`, `taxDeclarationParty`, `taxDeclarationRevision`, `taxDeclaration`, `taxDeclarationRevealInput` and `taxDeclarationReveal` to `contracts/workforce-payroll.json`. Add the two event payloads to the workforce contract package. Regenerate.
2. **Migration.** Add `tax_declarations.sql` as `0028_workforce_tax_declarations`: tables, checks, the lifecycle and line triggers, the shape constraint trigger and RLS. Register it in `module.go`.
3. **Database guard tests (TDD)** in `tax_declarations_db_test.go`: each constraint and trigger refuses a direct write.
4. **Handlers (TDD)** in `tax_declarations.go`: normalise, threshold lookup, seal, propose, decide, withdraw, history, reveal and error mapping. Routes and permissions in `module.go`; subjects in `Publishes()`.
5. **Integration tests** in `tax_declarations_test.go` against PostgreSQL and OpenBao, per the spec's list.
6. **API package** (`packages/api`): types re-exported, form schema and conversions.
7. **Shell:** a "Tax declarations" section in the Staff pay panel with tests.
8. **Docs:** `docs/api/workforce.md` routes, coverage row, and the claims spec's deferral note.
9. **Gates**, then the PR, the CI cycle, merge and deploy verification.
