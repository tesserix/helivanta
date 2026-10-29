# Implementation plan — pharmacy stock overview #1372

Worktree: `/private/tmp/helivanta-pharmacy-1372`.
Branch: `codex/pharmacy-1372-stock-overview`, based on `fe5f61c6`.
Design: [stock overview](../specs/2026-10-29-pharmacy-stock-overview-1372.md).

- [x] Verify issue scope against stock handlers, catalogue identity, grants,
      RLS, batch price fields, Global API routing and existing pharmacy UI.
- [x] Add canonical additional response fields and generate Go/Zod outputs.
- [x] Share outlet stock/expiry source; add bounded pharmacy-level query,
      per-product/currency valuation, scope metadata and complete bounded CSV.
- [x] Preserve single-outlet writes with a scoped inventory-read RLS extension.
      Use `0024_pharmacy` to avoid Claude's committed affiliation migration ID.
- [x] Add a pharmacy-level workspace view, navigation, filters, pages,
      explicit coverage/missing-cost states and CSV download.
- [x] Verify new and existing pharmacy behavior, frontend state and navigation,
      type checks, lint, generator parity and architecture gates.
- [x] Update API contracts, generated route/migration inventory and scope limits.

Validation evidence is reported with the delivered change. Production rollout,
real stock activity and issue closure are separate from this local implementation.
Whole-repository race/coverage gates and deployed browser acceptance are not
claimed by the package-level checks.

## Local validation

Passed:

- `go test ./internal/modules/pharmacy ./cmd/contractdocs ./internal/globalapi -count=1`
  (full pharmacy, contract generator and Global API routing suites).
- `go test -race ./internal/modules/pharmacy -run 'TestStockOverview|TestStockExportIncludesUnloadedPagesAndRefusesRowOverflow' -count=1`.
- `go test ./internal/archtest -run 'TestAllMigrationsPassRLSLint|TestEveryCollectionGETIsPaginated|TestModulesDoNotImportEachOther|TestWithAdminIsOnlyCalledFromTheAllowlist' -count=1`.
- `golangci-lint run ./internal/modules/pharmacy/...` (zero issues).
- Shell Vitest: stock-overview, stock-export and pharmacy-workspace (50 tests).
  API stock-export tests (5) and UI navigation tests (47).
- TypeScript checks for shell, API and UI; ESLint on changed shell/API/UI files.
- `node scripts/generate-pharmacy-contract.mjs --check`;
  `go run ./cmd/contractdocs`; `go run ./cmd/contractdocs -check`.
- `API_URL=http://localhost:8081 pnpm --filter @helivanta/shell exec next build --webpack`.
- Prettier on authored changed web/contract/documentation files and `git diff --check`.

Environment and remaining gates:

- Default Turbopack production build fails because its CSS worker cannot bind a
  local port in this environment, including after an escalated retry. The
  Webpack production build passes. No build configuration was changed.
- The local Node is v26.10.0; the repository declares Node 24. The successful
  checks above do not establish Node 24 CI acceptance.
- The generator's reference Markdown/JSON fail Prettier's generic check;
  `contractdocs -check` passes. They retain the canonical generator output.
- Whole-repository race/coverage gates, the complete Turbo matrix, a live
  authenticated browser journey, production rollout and qualified product
  review of cross-outlet identity linkage have not been claimed as complete.
