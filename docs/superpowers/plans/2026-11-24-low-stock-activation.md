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
