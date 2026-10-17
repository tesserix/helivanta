# Distributor structure and access implementation plan

Issue #1137; design: ../specs/2026-10-17-distributor-structure-and-access.md.

1. Audit existing hierarchy, permissions, UI and legacy without copying customer records; reproduce the organisation-grant authorization defect.
2. Implement secure grant administration and central effective grants, with additive schema, immutable evidence and current membership validation.
3. Implement two-level owned corporate relationships, scoped territories and representative assignments with database constraints and existing transaction/outbox helpers.
4. Generate the shared Go/TypeScript boundary, wire routes/permissions/events, and integrate modern reusable owned-organisation UI with a dedicated restricted sales view.
5. Merge agent changes in the shared isolated worktree; run focused and full Go race/coverage, static/architecture checks, contract drift, web suites, typecheck/lint/format and production build.
6. Compare authenticated local UI/API/SQL behavior with legacy capabilities. Record gaps honestly and continue the remaining operational role, delivery-assignment and approved support workflows before issue closure.
7. Commit/push a reviewable change after successful local validation; obtain explicit approval for the concrete new migration before automatic merge/release, then use signed images/Kargo/Argo and retest production. Record exact evidence and close only fully completed acceptance.
