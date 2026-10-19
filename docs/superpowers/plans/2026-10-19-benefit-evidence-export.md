# Benefit evidence and coverage export — plan

Spec: [2026-10-19-benefit-evidence-export-design.md](../specs/2026-10-19-benefit-evidence-export-design.md). Issue: #1106 (slice C).

1. **Shared CSV encoder.** Move pharmacy's formula-safe text and bounded writer to `backend/pkg/csvsafe`, with its hazard and overflow tests written there first. Switch `encodeStockCSV` to it, with pharmacy's tests unchanged in behaviour.
2. **Contract.** Add `benefitEvidenceInput` to `contracts/workforce-benefits.json` and regenerate the Go and TypeScript types.
3. **Migration.** Add `benefit_evidence.sql` as `0034_workforce_benefit_evidence`. It adds the `benefit_proposal` record kind and category, the creation, freeze and decision triggers, and the benefits branch of `document_isolation`. Register it in `module.go`. Database guard tests first.
4. **Evidence gate and routes.** `benefit_evidence.go` adds a `documentGate` bound to the path proposal: `benefitsWrite` for attach and retry, `benefitsRead` for list and download. Add the five routes. Call `settleBenefitEvidence` in `transitionBenefitProposal` before the update, and map `evidence_scanning`. Integration tests first, through the real scanner worker, in `benefit_evidence_test.go`.
5. **Export.** Add `workforce.benefits.export` and `GET …/hr/benefits/coverage.csv` in `benefit_export.go`: strict filters, a 10-second deadline, 1,001-row probe, `csvsafe` encoding, and an audit row with count and digest in the same transaction. Tests assert the actual bytes, the boundaries, the 413 and the denials first.
6. **Client** (`packages/api`). Generalise the tax evidence client and polling hook into `workforce-evidence.ts` keyed by record kind, keeping the tax tests green. Add benefit evidence scope helpers and `fetchBenefitCoverageCSV` with file name and content-type checks. Tests first.
7. **Shell.** Generalise `DeclarationEvidence` into a shared evidence panel. Add the "Evidence" view to enrollments, evidence in the review and withdraw forms with decisions held while scanning, and the "Export coverage" form. Extend `apps/shell/test/benefits.ts`. Component tests first.
8. **Docs.** API reference, coverage row, and the "Not in slice" lines of the slice A and B specs.
9. **Gates:** `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `go test -race ./...`, and `pnpm turbo lint type-check test build format:check` on Node 24. Then the PR, the CI cycle, merge and deploy verification.
