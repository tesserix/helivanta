# Attendance corrections — plan

Spec: [attendance corrections](../specs/2026-10-16-attendance-corrections-design.md). Issue: #1038 slice 3, #1039 slice 2.

1. Migration `attendance_corrections.sql` (`0017_workforce_attendance_corrections`):
   - The corrections and events tables, with their triggers.
   - The `correction_id` columns on punches and voids.
   - Replaced punch and void insert triggers.
   - RLS.
2. Contract: extend `contracts/workforce-attendance.json` with the correction models and kinds/statuses, then regenerate the Go and TS types.
3. Backend `attendance_corrections.go`:
   - File, decide, cancel, get, list and history handlers.
   - Approval writes the punch and void.
   - Routes and permissions in `module.go`.
   - The event subject in `contract/events.go` plus the payload allow-list.
   - Reader redaction extended.
4. Tests `attendance_corrections_test.go`. The spec's test list must all pass under `-race`, and coverage must stay at or above 70%.
5. `packages/api`: correction paths, schemas and labels, with tests.
6. Shell:
   - `corrections-panel.tsx`: board, decision and cancel actions.
   - `correction-form.tsx`.
   - Ledger "Request correction" action.
   - Page tests.
7. `scripts/verify-attendance-corrections.mjs`, plus docs: `docs/api/workforce.md`, `docs/hr-capability-coverage.md` and the ADR-0028 status line.
8. Gates:
   - `make lint-go`, the coverage gate and `go test -race`.
   - turbo lint, type-check, test, build and format:check.
   - `make lint-shell` and the generator `--check`.
9. PR referencing #1038/#1039. CI green, admin merge, then verify the rollout read-only.
