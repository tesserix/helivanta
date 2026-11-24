# Delivery plan — low-stock activation #1685

1. Add and observe failing release-boundary tests, then canonical narrow capability
   and route/page classifications; eligibility remains false.
2. Add failing owner tests for product choices, exact reload and overview parity,
   permission/scope/query boundaries and disclosure audit failure. Implement the
   narrow projections using existing scoped transactions and lowStockSource.
3. Add canonical models/SDK validation and failing UI tests. Reuse the minimum
   editor with its narrow product source, add named overview/list entry and ensure
   scope/authorization failures hide stale details and pending commands recover.
4. Prove the real built-browser → Global API → owner workflow with broad pharmacy
   capabilities off, plus 10,000-product list/summary capacity and original stock
   semantics/replay/isolation regressions on hosted infrastructure.
5. Generate/check contracts, run affected local tests/lints/builds and remove build
   outputs. Record actual red/green evidence. Commit and push incremental work.
6. After focused gates pass, prepare reviewed eligibility and matching chart
   catalogue. Merge passing protected checks; deploy signed application revision
   through normal Release/Kargo. Sync the chart and enable only low stock through
   Console; verify scheduled preservation and authenticated browser acceptance.
7. Update #1685 with exact evidence and close only when its acceptance is met.

7. **Measured performance correction (pending hosted acceptance).** Run 45381432835
   failed the original 500 ms gate (page p99 2.997 s, count 3.223 s); the hospital
   case exhausted the overall test timeout. Diagnostic 45383663086 retains actual
   EXPLAIN plans (count 2.789 s, first page 2.874 s). Migration 0071 preserves the
   row tenant/outlet boundary and same grant function while evaluating that
   statement-local grant in a scalar SELECT. Verify predecessor/new visibility,
   unchanged writes/purposes/other read branches, unexpected-predecessor refusal,
   both ownership regressions and unchanged capacity target. Request explicit
   production migration approval only with concrete passing evidence.

Observed candidate evidence as of 24 November:
- Full local API 2,012, shell 2,378 and shared UI 333 tests passed.
- Built-browser and Global API acceptance passed both ownership models on
  `527dcfe4`, hosted run `45386011073` (before the read-policy migration).
- The dedicated outlet-access read now belongs to Setup; an observed browser 404
  and local release-classifier regression proved the prior Catalogue lock.
- Local affected builds, Go vet/lint, generators, source-review and contract checks
  passed; generated build artifacts were removed. No local app/DB/container ran.
- Performance, migration acceptance, exact-head protected checks, normal merge,
  signed release, Console activation and deployed acceptance remain pending.

8. **Remaining measured cost (pending acceptance).** First policy optimization
   reduced the catalogue scan to 41 ms but count/page plans remained 1.830/1.840 s.
   The same unapplied migration now centralizes relational lot reasons and bulk
   canonical buckets; legacy functions delegate with compatible signatures,
   null-day/missing-lot behavior, rule priority, holds and arithmetic. The warning
   source aggregates once per outlet. Run all affected pharmacy regressions,
   explicit bulk/product/blocked-lot parity and the unchanged capacity gate.
   Enable nothing until these pass; production approval must include the
   function delegations as well as policy evaluation.

9. **Wider gate corrections (pending acceptance).** On 0347e7ff, browser and
   bulk/product/rule-parity tests pass. Count p99 is 373/377 ms, but pages
   933/942 ms still fail. Use a materialized query-local keyed numeric aggregate
   to remove the observed 9.5-million-row nested-loop rescan; preserve 500 ms.
   Full pharmacy and compound-admission regressions detected the hospital
   stocktake seal's old movement-policy fingerprint. Validate the exact previous
   seals/choice structure, compare all unrelated metadata, then re-pin the one
   controlled read-clause change. Prove unrelated evidence, choice, index and
   guard-property drift is refused atomically; retain all runtime restrictions.

10. **Prepared-plan correction (pending acceptance).** Diagnostic 96feb933
    recorded page p99 534 ms at page 7 (count 244 ms), 194 generic/five custom
    page executions, and 285 ms custom versus 542 ms generic execution at that
    cursor. Generic rescanned the materialized JSONB tuple 9,650 times. Use
    uncorrelated scalar InitPlans to read that one statement-local aggregate
    once per expression. Keep both models, all pages/counts and 500 ms unchanged.
