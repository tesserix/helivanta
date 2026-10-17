# Branch premise delivery plan

Story #1137; spec: ../specs/2026-10-17-distributor-branch-premises.md.

1. Observe a failing real PostgreSQL API assertion for a linked owned warehouse.
2. Add the generated optional UUID contract and additive guarded migration. Preserve
   omitted assignments while allowing explicit null; retain existing revision/outbox path.
3. Prove IN/PK, unknown/foreign/counterparty/non-branch/self links, concurrency, backwards
   compatibility, stale updates, exact replay, role denial/revocation and DB constraints.
4. Add the modern branch selector and paged selected-record resolution to the existing
   premise form. Show branch context in lists/history without duplicating masters.
5. Test the shared form schema, UI selection/failure/pagination and actual local API/UI.
6. Run required Go/web/architecture checks; publish reviewable test/recovery evidence.
   Merge/release only after checks, then verify exact images, readiness and production UI.
   Keep #1137/#1011/#1133 open until their full broader acceptance is satisfied.
