# Professional tax periods — plan

Spec: [2026-10-18-professional-tax-periods-design.md](../specs/2026-10-18-professional-tax-periods-design.md). Issue: #1041.

1. **Calculator (TDD).** Write failing tests in `statutory_pt_lwf_calc_test.go` for `ptPeriod` (every start month, wrapping the year end) and `ptPeriodsValid` (starts per cadence, one deduction month per period). Then add both to `statutory_calc.go`.
2. **Migration.** Add `pt_periods.sql` as `0026_workforce_pt_periods`, with the cadence and mask columns, their check, and the deferred `workforce_statutory_pt_periods` trigger. Embed and register it in `module.go`.
3. **Handler.** Add `Cadence` and `PeriodStartMonths` to `statutoryPtParams`. Validate and sort them in `normalizeStatutorySlabs`. Insert and load them in `statutory_rules.go`, and map the new constraint in `respondStatutoryError`.
4. **Preview.** Add `statutoryPeriod` to `statutoryResult`, and project period wages in `statutoryOn` for periodic PT.
5. **Integration tests** in `statutory_pt_lwf_test.go`: rejection cases, a direct-insert trigger case, replay, and both previews.
6. **Contract and form** (`packages/api`): add cadence and period starts to the PT params contract, the form schema, validation, conversions and the summary. Add `period` to the result contract.
7. **Shell:** add cadence and period inputs to `PtFields`, relabel the slabs, and show the period line in the preview. Add tests in `statutory.test.tsx` and `payroll-statutory.test.ts`.
8. **Docs:** update the PT spec's deferral note, the coverage row in `docs/hr-capability-coverage.md`, and the statutory reference.
9. **Gates:** `make lint-go`, coverage gate, `go test -race` and the pnpm turbo gate. Then the PR, the CI cycle, merge and deploy verification.
