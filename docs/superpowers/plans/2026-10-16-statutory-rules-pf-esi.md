# Statutory rule sets (PF and ESI) — plan

Spec: [statutory rule sets](../specs/2026-10-16-statutory-rules-pf-esi-design.md). Issue: #1041 slice 2a.

1. Migration `statutory_rules.sql` (`0021_workforce_statutory_rules`):
   - Rule set, revision, and PF/ESI parameter tables.
   - The separation CHECK, the transition trigger, the parameter-presence deferred trigger, and `workforce_statutory_rule_on`.
   - RLS.
2. Contract `contracts/workforce-payroll.json`: add the statutory input, output, decision and preview models, then regenerate.
3. Calculator `statutory_calc.go`: pure and integer-only, with table-driven boundary tests in `statutory_calc_test.go`.
4. Backend `statutory_rules.go`:
   - Propose, decide, withdraw, list and history handlers.
   - Preview, reusing the structure computation extracted from `previewSalaryStructure`.
   - `PermPayrollApprove`, the routes, and the event subject with its payload allow-list.
   - The archtest matrix and the `perm !=` chain.
5. Tests `statutory_rules_test.go`, covering the spec's database and API list.
6. `packages/api` `payroll.ts`: statutory paths, form schemas (rupees and percent to paise and ppm), status labels, and tests.
7. Shell: a statutory section on `/workforce/payroll-setup`, with rule cards, the proposal form, decision and withdraw actions, history and the statutory preview. Page tests.
8. `scripts/verify-statutory-rules.mjs` (proposer and approver cookies), plus docs: `docs/api/workforce.md`, `docs/hr-capability-coverage.md` and ADR 0029 consequences.
9. Gates: `make lint-go`, the coverage gate, `go test -race`, turbo, `make lint-shell` and the generator `--check`.
10. PR "Part of #1041". CI green, admin merge, then verify the rollout read-only.
