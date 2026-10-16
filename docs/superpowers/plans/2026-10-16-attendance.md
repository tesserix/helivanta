# Attendance plan

Issue: #1039. ADR: [0028](../../adr/0028-attendance.md). Spec: [attendance](../specs/2026-10-16-attendance-design.md).

## Slice 1: policy, punches and derived attendance

1. Migration `0016_workforce_attendance`: settings and revisions, punches, voids, forced RLS, immutability triggers, `workforce_attendance_policy_on`.
2. `contracts/workforce-attendance.json` and `scripts/generate-workforce-attendance.mjs` for Go and zod models.
3. Handlers: settings, punches, void, day sheet, totals, person range; permissions, routes, events, arch-test allowlists.
4. Pure derivation function with table-driven tests; integration tests against PostgreSQL.
5. `@helivanta/api` attendance helpers and tests; shell page `/workforce/attendance` with zone entry and page tests.
6. `scripts/verify-attendance.mjs`, `docs/api/workforce.md`, `docs/hr-capability-coverage.md`.

## Slice 2: missing-punch corrections (closes #1038 slice 3)

Filed corrections that add or void punches, decided by a different person, linked to the punches they create or void.

## Slice 3: finalisation and reports

Finalised timesheet snapshots with recalculation versions, punch locks inside finalised periods, monthly counters and the MIS punch report reconciled to individual rows.
