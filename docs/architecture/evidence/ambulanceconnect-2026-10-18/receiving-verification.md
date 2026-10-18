# Receiving release verification, 2026-10-19

This extends the earlier crew evidence. It covers the actual `apps/shell`
production Next build and the real emergency backend. No real patient records,
emergency calls, clinical approvals or production bookings were created.

Executed validation:

- Emergency, IAM, authorization and configuration packages passed `go test -race`.
- Full affected web suites: shell 563 tests, API client 310, shared UI 95.
- Workspace type checks and ESLint passed; production Next build passed.
- Shared emergency contract regeneration check passed.
- `go vet ./...`, `go build ./...` and `golangci-lint run` passed.
- Six existing browser journeys passed. A teardown race found in the crew fixture
  was fixed by draining its route handlers; no test or timeout was removed.
- The new 390px receiving journey passed against disposable Postgres/OpenBao/NATS:
  pre-alert, separate hospital acceptance, transport, arrival, independent physical
  receipt, crew handover, fresh readiness and supervisor clearance.
- Negative SQL/API cases cover revoked/expired receiving and clinical authority,
  destination supersession and exact command binding, unassigned/cross-tenant denial,
  replay, separate receipt subjects and retention of occupied capacity on timeout.
- A new regression checks that suspending new reservations preserves orchestration
  and permits existing trips to finish.

Screenshots: [receiving](receiving-mobile.png), [crew transport](transport-mobile.png),
[clearance](cleared-mobile.png). All records and references are synthetic.

Public booking/status/cancellation, native booking/device evidence, canonical
workforce qualification linkage and the clinical pilot safety case are not covered
by this verification. Deployment evidence is recorded separately after promotion.
