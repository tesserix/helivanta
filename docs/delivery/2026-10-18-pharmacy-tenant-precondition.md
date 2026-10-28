# Pharmacy expected-tenant precondition

Story [#1237](https://github.com/tesserix/helivanta/issues/1237), assigned to Sam123ben under #162. [Specification](../superpowers/specs/2026-10-18-1237-pharmacy-tenant-precondition.md) and [plan](../superpowers/plans/2026-10-18-1237-pharmacy-tenant-precondition.md).

## Change

Pharmacy now compares an optional expected-tenant header with the verified session at its existing principal boundaries. An explicitly empty, malformed, nil, repeated or mismatched header returns 409 `session_changed`, generic refresh guidance and no-store. Absent and valid matching values remain compatible, including UUID forms accepted by the established Supply contract. The header never selects a tenant or grants access.

Caller tracing covered the catalogue/stock/payables scopes, directory and affiliation operations, medication/dispense handlers and generic insights/configuration handlers. Those last two now have an explicit pharmacy guard as well. Route membership and permission checks still execute first. Existing live outlet grants, scoped references and FORCE RLS remain authoritative. Rejected requests create no CSV, successful export audit, state change or business/outbox event.

There is no application migration, new endpoint, dependency, cache, event, workflow, bucket, role or deployment configuration. The existing CSV UI displays the generic API guidance; only its contract test changes. Legacy stock screens were reviewed for #1232; this request-boundary check changes no legacy field mapping or business-report definition.

## Evidence

- The new public HTTP-interface regression failed with expected 409 / actual 200 and an attributed successful CSV generation. After the shared scope check it passed. A declared-route matrix then reproduced the uncovered registry, medication, dispense, insights and configuration families before their integration; it subsequently passed.
- Real PostgreSQL tests cover absent/canonical/URN/braced headers; empty/malformed/nil/repeated/comma-combined/mismatched headers; both tenant principals; wrong-action/unassigned/revoked/anonymous denials; unchanged state/audit/outbox counts for a valid rejected write; and matching/absent-header idempotent replay. The route matrix exercises every declared pharmacy route and checks that key route families are present and rejected exports generate no success audit.
- Thirty-three local API requests used two real OIDC subjects against the built Go 1.27.2 service and existing TEST1229 synthetic fixtures. Matching/optional/URN CSV bytes and reader responses matched. Invalid/stale/empty/repeated headers returned 409; anonymous access 401 and a foreign-outlet reference 404. Four repeated valid-body writes with a stale header created no location. Read-only SQL under the non-superuser/non-BYPASSRLS application role matched stock, audit and outbox counts before/after. These OIDC subjects share one tenant; the two-tenant principal cases are covered by the real-database integration harness rather than claimed as a two-tenant OIDC/browser walkthrough.
- Go vet/build passed, lint reported zero issues, all 15 frontend static tasks passed, and the Next.js 16 production build passed. The full shell suite passed all 509 tests across 70 files. The full Go 1.27.2 backend race/count-one/coverage gate also passed using the dedicated cache.
- The original full Go run was interrupted by missing shared cache artifacts, including standard-library files. The old local dependency stack also became unavailable. Its failed log is retained separately; no passing source assertion is inferred from that interruption. A retry uses a fresh dedicated cache with the unchanged race/count-one/70% coverage gate.
- A fresh isolated Compose project restored PostgreSQL, Zitadel, OpenFGA, NATS JetStream and Valkey on dedicated loopback ports. Existing migrations applied successfully to that fresh local database. Twenty-six additional API checks used two newly created, password-verified OIDC subjects in separate tenants. Matching/optional/URN/braced CSV responses matched; foreign objects returned 404; stale, malformed, empty and repeated headers returned 409; anonymous requests returned 401. Repeated rejected writes left organization/location, audit and outbox counts unchanged under the non-superuser/non-BYPASSRLS runtime role. The helper respected normal rate limits through bounded retries; no limit was weakened.
- A new component contract test exercises the existing 409 refresh guidance without creating a blob or download. Existing scope/filter cancellation tests remain. The rebuilt local UI completed a real browser OIDC callback, displayed the signed-in dashboard and stock workspace, selected the synthetic pharmacy/outlet, and showed the saved Asia/Kolkata timezone. Native Chrome saved the empty-stock CSV (344 bytes, zero data rows); its SHA-256 matched the attributed API export log, and the page reported “CSV download started.” Earlier concurrent user browser activity was worked around without altering other tabs. A native stale-header walkthrough is not claimed. The isolated UI is at `http://helivanta1237.localhost:14402`, backed by the fixed API on port 18097. The API checks and component test provide the stale-header and download-denial evidence.

The full-suite gates must be collected before committing or requesting release. No new production rollout is authorized or performed for this story; #1237 remains open pending review, named release approval and deployed verification. The preceding stock-export story #1232 is already separately released and closed at `f7e642a6236e59424f52e6ed37752d1b76235163`.

## Commands run

Backend, using the existing Colima socket, task GOCACHE and `GOFLAGS=-p=1`:

```sh
go test ./internal/modules/pharmacy -run '^TestPharmacyStockExportRejectsStaleExpectedTenant$' -count=1
go test ./internal/modules/pharmacy -run '^TestPharmacyExpectedTenantRejectsEveryRouteFamilyWithoutSideEffects$' -count=1
go test ./internal/modules/pharmacy -run '^TestPharmacyStockExportRejectsStaleExpectedTenant$|^TestPharmacyExpectedTenant' -count=1
./scripts/coverage-gate.sh
go vet ./...
go build -o /tmp/helivanta-1237-api ./cmd/api
golangci-lint run ./...
```

Frontend, with Node 24.11.1:

```sh
pnpm install --frozen-lockfile
pnpm turbo type-check lint format:check
pnpm --filter @helivanta/shell exec vitest run --pool=threads --maxWorkers=1
pnpm --filter @helivanta/shell build
git diff --check
```

Private OIDC configuration, session values, local API/SQL helpers and credential-bearing launch configuration remain outside Git. No customer or legacy data is copied.

Recovery checks also ran `go run ./cmd/migrate`, the existing Zitadel bootstrap with private local callback-port overrides, and another `next build` against the fresh local issuer. The private two-tenant acceptance helper ran with Node 24.11.1. None targets production.
