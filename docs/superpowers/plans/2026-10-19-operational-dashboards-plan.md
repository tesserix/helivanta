# Operational dashboard implementation

Issues: #573, #574, #576, #577. Design: ../specs/2026-10-19-operational-dashboards-design.md.

1. Observe a failing authenticated clinical aggregate HTTP test.
2. Implement bounded timezone-aware count windows and owner-module endpoints.
3. Verify real PostgreSQL totals, empty buckets, calendar/DST boundaries,
   validation, missing permissions and cross-tenant isolation.
4. Add hospital-local workforce summaries and explicit-access/revocation tests.
5. Observe failing dashboard interaction tests, then implement responsive
   filters, metric cards, charts, accessible tables and independent retry states.
6. Run affected and repository quality gates; inspect a local browser preview.
   Record exact validation and limitations in docs/operational-dashboards.md.

7. Review legacy dashboard labels/patterns without copying hospital records.
   Add permission-aware focused views, visit mix, service cohort distributions
   and calendar-day averages through failing UI tests.
8. Reproduce and correct midnight-DST date shifting; verify real HTTP totals,
   requested dates, tenant isolation and SQL boundaries for all three clinical
   owner endpoints. Keep calendar iteration independent of local midnight.
9. Re-run frontend quality gates and backend race/coverage checks; inspect the
   focused views and distributions at desktop/mobile sizes using real local APIs
   and disposable synthetic PostgreSQL fixtures. Document source-workflow gaps.
