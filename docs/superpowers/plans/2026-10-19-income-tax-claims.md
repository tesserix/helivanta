# Income-tax claims — plan

Spec: [2026-10-19-income-tax-claims-design.md](../specs/2026-10-19-income-tax-claims-design.md). Issue: #1041.

1. **Contract.** Add `incomeTaxClaimGroup`, `incomeTaxClaim`, `incomeTaxClaimInput`, `incomeTaxHraInput` and `incomeTaxClaimResult` to `contracts/workforce-payroll.json`. Add `claim_groups`/`claims` to `incomeTaxParams`, `claims`/`hra` to `incomeTaxPreviewInput`, and the new totals and lines to `incomeTaxResult`. Regenerate.
2. **Calculator (TDD).** Write failing tests in `income_tax_calc_test.go` for HRA, interest, caps, groups, the income limit and `not_allowed`. Then extend `calculateIncomeTax`, and add claim validation to `normalizeIncomeTax`.
3. **Migration.** Add `income_tax_claims.sql` as `0027_workforce_income_tax_claims`, with the tables, kind CHECK, indexes, RLS, immutability and the unused-group trigger. Register it in `module.go`.
4. **Handler.** Insert and load claims in `income_tax.go`, and map the new constraints in `respondStatutoryError`. Pass the preview claims through.
5. **Integration tests** in `income_tax_test.go`: refusals through the API and by direct insert, read-back, replay, and a preview under both regimes.
6. **Contract and form** (`packages/api`): claim form schema, conversions, summary and preview input.
7. **Shell:** claims editor in `income-tax-fields.tsx`, and preview inputs and lines in `income-tax-preview.tsx`, with tests.
8. **Docs:** coverage row and the income-tax spec's deferral note.
9. **Gates**, then the PR, the CI cycle, merge and deploy verification.
