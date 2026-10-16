# Plan: staff salary assignment and tax-regime election

Spec: [staff salary assignment](../specs/2026-10-17-staff-salary-assignment-design.md). Issue: #1041 slice 4a.

1. **Migration `0024_workforce_salary_assignment`** (`salary_assignment.sql`): assignment, revision and amount tables; tax-regime tables; `workforce_salary_assignment_on`; `workforce_salary_assignment_check` run by deferred triggers on head, structure and assignment revisions; proposal, decision, approval and immutability triggers; forced RLS. Register it in `module.go`.
2. **Calculator overrides**: `salaryPreviewOn` takes per-head amounts that replace fixed line amounts; unit tests for override, fallback and an override on a non-fixed head refused.
3. **Handlers** (`salary_assignment.go`): list, get, propose, decide, withdraw, history, staff preview; `tax_regime.go`: list, record, history. Errors map to `respond.*`; audit and outbox in the business transaction; contract events in `pkg/contracts/workforce`.
4. **Routes and permissions** in `module.go`, every route through `*platform.Router` with the permission in the spec.
5. **Postgres tests** (`salary_assignment_test.go`, `tax_regime_test.go`) for every scenario in the spec, then mutation checks on each guard.
6. **Contract and TS client**: `contracts/workforce-payroll.json`, `scripts/generate-workforce-payroll.mjs`, `packages/api/src/payroll.ts` schemas and paths, unit tests.
7. **Shell panel** `staff-pay-panel.tsx` with assignment form, decision, history, regime elections and preview; page tests.
8. **Docs**: `docs/api/workforce.md`, `docs/hr-capability-coverage.md`, ADR 0029 note.
9. **Gates**: `make lint-go`, `go vet`, `go test -race`, coverage gate, `pnpm turbo lint type-check test build format:check`, `make lint-shell`.
