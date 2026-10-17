# Plan: staff bank and tax identifiers

Spec: [staff identifiers](../specs/2026-10-18-staff-identifiers-design.md). Issue: #1041 slice 4b.

1. **Transit client** `pkg/transit`: encrypt, decrypt and HMAC with derived context, Kubernetes and userpass login with re-login on expiry, every failure reduced to `ErrUnavailable` or `ErrIntegrity`; OpenBao testcontainer in `internal/testinfra`.
2. **Config** `RequireTransit`: all four OpenBao settings or none; partial configuration stops boot.
3. **Migration `0025_workforce_staff_identifiers`** (`staff_identifiers.sql`): identifier and revision tables, sealed/mask/separation constraints, PAN uniqueness for in-force and pending, proposal, decision, approval and advance triggers, forced RLS.
4. **Handlers** (`staff_identifiers.go`): get, propose, decide, withdraw, history and reveal; authorise before touching the vault; audit and outbox in the business transaction; `PermPayrollIdentifiersReveal`.
5. **Log redaction** of PAN and UAN shapes in `pkg/logging`.
6. **Postgres tests** for every scenario in the spec, then mutation checks on each guard; archtest matrix and pagination allowlist entries.
7. **Contract and TS client**: schemas, paths and form schemas in `packages/api/src/payroll.ts`, unit tests.
8. **Shell** `staff-identifiers.tsx` in the Staff pay panel with masked values, proposal form, decision and reveal; page tests.
9. **Infrastructure** in tesserix-k8s: Transit mount, policy, auth role, network policies and API env.
10. **Docs**: `docs/api/workforce.md`, `docs/hr-capability-coverage.md`, ADR 0029, `docs/runbooks/secrets.md`, `docs/runbooks/soc2-controls.md`.
11. **Gates**: `make lint-go`, `go test -race`, coverage gate, `pnpm turbo lint type-check test build format:check`, `make lint-shell`.
