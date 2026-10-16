# Salary heads and structures — plan

Spec: [salary heads and structures](../specs/2026-10-16-salary-heads-structures-design.md). Issue: #1041 slice 1.

1. Migration `salary_config.sql` (`0020_workforce_salary_config`):
   - Head, head-revision and head-base tables.
   - Structure, structure-revision and line tables.
   - `workforce_salary_head_on`, plus `workforce_salary_config_check` behind deferred constraint triggers.
   - RLS.
2. Contract: create `contracts/workforce-payroll.json` and `scripts/generate-workforce-payroll.mjs`, which emit `payroll_types.go` and `packages/api/src/payroll-contract.ts`. Wire the `--check` into `packages/api` type-check.
3. Calculator `payroll_calc.go`: pure, integer-only, with table-driven tests in `payroll_calc_test.go`.
4. Backend `salary_config.go`:
   - Head and structure put, list and history handlers, plus preview.
   - `PermPayrollConfigure` and `PermPayrollView`, the routes, and the event subject with its payload allow-list.
   - The archtest matrix, and the unpaginated allow-list for preview.
5. Tests `salary_config_test.go`, covering the spec's database and API list.
6. `packages/api` `payroll.ts`: paths, form schemas (decimal rupee and percent parsing to paise and ppm), labels, and tests.
7. Shell `/workforce/payroll-setup`:
   - Heads panel, head form and structures panel.
   - Preview and history.
   - Zone registration, plus page tests.
8. `scripts/verify-salary-config.mjs`, plus docs: `docs/api/workforce.md` and `docs/hr-capability-coverage.md`.
9. Gates: `make lint-go`, the coverage gate, `go test -race`, turbo, `make lint-shell` and the generator `--check`.
10. PR referencing #1041. CI green, admin merge, then verify the rollout read-only.
