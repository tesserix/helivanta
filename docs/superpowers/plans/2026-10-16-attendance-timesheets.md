# Attendance timesheets — plan

Spec: [attendance timesheets](../specs/2026-10-16-attendance-timesheets-design.md). Issue: #1039 slice 3.

1. Migration `attendance_timesheets.sql` (`0019_workforce_attendance_timesheets`):
   - The period, period-day, event, timesheet, people and day tables.
   - The reconciliation constraint trigger.
   - The lock triggers on punches, voids, corrections and policy revisions.
   - RLS.
2. Contract: extend `contracts/workforce-attendance.json` with the period, timesheet, person, day, event and punch-report models, then regenerate the Go and TS types.
3. Backend `attendance_timesheets.go`:
   - Period create, finalise, reopen, get, list and history handlers.
   - Timesheet, people and day listings, and the punch report.
   - `PermAttendanceFinalise`, the routes, the event subject and payload allow-list, the archtest matrix and the unpaginated allow-list.
   - Map the lock to `period_locked` in the attendance and correction error responses.
4. Tests `attendance_timesheets_test.go`. The spec's test list must all pass under `-race`, and coverage must stay at or above 70%.
5. `packages/api`: paths, form schemas, labels and tests.
6. Shell:
   - `timesheets-panel.tsx`: periods, finalise form, versions, people and days, recalculate and reopen.
   - `punch-report.tsx`.
   - Page tests.
7. `scripts/verify-attendance-timesheets.mjs`, plus docs: `docs/api/workforce.md`, ADR-0028 and `docs/hr-capability-coverage.md`.
8. Gates:
   - `make lint-go` and the coverage gate.
   - turbo lint, type-check, test, build and format:check.
   - `make lint-shell` and the generator `--check`.
9. PR referencing #1039. CI green, admin merge, then verify the rollout read-only.
