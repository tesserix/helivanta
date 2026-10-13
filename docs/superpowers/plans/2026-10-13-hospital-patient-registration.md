# Hospital patient registration implementation

Related: #981, #991; design: ../specs/2026-10-13-hospital-patient-registration.md.

1. Reproduce missing hospital isolation through the real HTTP/PostgreSQL harness.
2. Append hospital/grant/audit migration and enforce scope for every patient path.
3. Add bounded POST search and input validation; prove isolation and consent behavior.
4. Add Patients workspace, registration and synthetic fixtures with UI tests.
5. Run Go race/lint/coverage and frontend tests/typecheck/lint/build; exercise browser.
6. Review migration/release evidence before GitOps rollout and synthetic live acceptance.
