# Income-tax rule sets: plan

Spec: [income-tax rule sets](../specs/2026-10-17-income-tax-rule-sets-design.md). Issue: #1041 slice 3.

1. Migration `income_tax.sql` (`0023_workforce_income_tax`). It adds:
   - the widened kind CHECK and `workforce_tax_year(date)`
   - the params, age band, slab and surcharge tables
   - the replaced parameter-presence trigger
   - the deferred completeness trigger: tax year matches the effective date, an age 0 band, complete slabs, rising surcharges
   - immutability, creation-transaction and RLS loops for the new tables
2. Contract `contracts/workforce-payroll.json`: income-tax params, the preview input and result. Then regenerate.
3. Calculator `income_tax_calc.go`:
   - Deduction, rounding, slabs, rebate with marginal relief, surcharge with marginal relief, cess, final rounding.
   - Age band choice that never guesses an unknown age.
   - Boundary tests on a synthetic ladder.
4. Backend: normalise, insert and load income-tax revisions; `POST /hr/payroll/income-tax-preview`; keep both regimes out of the monthly preview.
5. Tests: the real-PostgreSQL lifecycle, API 400 and trigger refusals, 403/404 isolation, and mutation checks of every guard.
6. `packages/api` `payroll.ts`: income-tax form and preview schemas mirroring the server, with tests.
7. Shell: income-tax fields in the statutory editor and an annual preview, with page tests.
8. `scripts/verify-statutory-rules.mjs` gains both regimes.
9. Docs: `docs/api/workforce.md`, the coverage doc and ADR 0029.
10. Gates, then a PR "Part of #1041". Once CI is green, admin merge and verify the rollout read-only.
