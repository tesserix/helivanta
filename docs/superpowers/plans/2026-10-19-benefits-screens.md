# Benefits screens — plan

Spec: [2026-10-19-benefits-screens-design.md](../specs/2026-10-19-benefits-screens-design.md). Issue: #1106 (slice B).

1. **Zone entry.** Add "Benefits" (`/workforce/benefits`, `workforce.benefits.view`) to the Staff zone in `packages/ui/src/zones.ts`.
2. **Workspace** (`apps/shell/app/workforce/benefits/`). Page, hospital picker and the no-permission notice, with the write and approve flags derived from the grant and permissions.
3. **Plans.** List, inline editor with relationship choices, retirement confirmation and revision history. Tests first.
4. **Dependents.** List, editor with reason and status, and history. Tests first.
5. **Enrollments.** List with coverage and pending state, the proposal form with covered-person choices, the decision and withdraw panel, and history. Tests first.
6. **Docs.** The API reference note, the coverage row and the slice A spec's "Not in this slice".
7. **Gates.** `pnpm turbo lint type-check test build format:check`, then the PR, the CI cycle, merge and a check of the deployed page.
