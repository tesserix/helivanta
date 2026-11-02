# Organization reference prerequisite — local evidence

Historical reference-protocol checkpoint. Subsequent department/UI, actual local
runtime migrations and OpenFGA evidence are in the
[department acceptance record](2026-11-02-organization-departments-local.md).

Branch: `feat/shared-staff-foundation-20261102`, based on `40787523`.
Scope: the [unreleased reference protocol](../api/organization.md), separate from
the [Pharma browser acceptance](2026-11-02-pharma-shared-staff-local.md).

The initial route test returned 404 before implementation. A second test failed
when the actual Workforce observer was absent. Real Postgres testing also exposed
receipt precision differences and an omitted boolean being interpreted as false;
both were corrected and their behavioral regressions retained. Final boolean
cases reject omission, null, strings, numbers, arrays and objects without changing
the pending registration. Positive/negative owner observations are covered in the
full protocol tests.

The first full architecture run correctly rejected unclassified event fields and
ad hoc payload decoding, after earlier singleton-exemption and privileged-test
fixture findings. The consumer still decodes business data into the canonical
Workforce contract with unknown-field rejection; its separate raw-field preflight
enforces required boolean presence. Every new event field has an explicit
non-PHI rationale. No architecture assertion was removed or skipped.

The Linux guest architecture process exhausted the QA VM memory. Native cached
Go completed the architecture suite in 84.566 seconds, organization tests in
9.491 seconds and bootstrap tests in 20.522 seconds. After broader invalid-boolean
coverage, organization tests passed again in 8.939 seconds. The Global API owner
routing race test passed in 3.029 seconds. The full Workforce race suite passed in
576.560 seconds; the combined final architecture/organization/Workforce command
exited successfully.

Commands actually run from `backend/`, with `GOTOOLCHAIN=local`, `GOPROXY=off`,
bounded Go concurrency and the existing local test Docker socket:

```sh
go test -race -count=1 -timeout=10m ./internal/archtest ./internal/bootstrap ./internal/modules/organization
go test -race -count=1 -timeout=5m ./internal/globalapi -run TestEveryRegisteredRouteReachesItsProductOwner
go test -race -count=1 -timeout=10m ./internal/archtest ./internal/modules/organization ./internal/modules/workforce
go test -race -count=1 -timeout=5m ./internal/modules/organization
go build ./...
go vet ./internal/modules/organization/... ./internal/modules/workforce/... ./internal/archtest/... ./internal/bootstrap/... ./internal/globalapi/...
golangci-lint run ./internal/modules/organization/... ./internal/modules/workforce/... ./internal/archtest/... ./internal/bootstrap/... ./internal/globalapi/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
```

Build, vet, scoped lint (zero issues), contractdocs generation/parity and canonical
generator parity passed. Root-level inventory parity and all 15 inventory tests
also passed:

```sh
node scripts/generate-organization-structure.mjs --check
node scripts/staff-operational-access-inventory.mjs
node scripts/staff-operational-access-inventory.mjs --check
node --test scripts/staff-operational-access-inventory.test.mjs
git diff --check
```

Ten existing source pins were refreshed after checking that Workforce Routes and
Permissions are byte-for-byte unchanged. Only additive organization migration,
publication and consumer wiring changed in that module. The ledger records 749
surfaces, 17 classified and 732 unclassified; no new lifecycle acceptance is
asserted. Required runtime cutoff obligations were preserved.

At this initial checkpoint, migrations had only run in disposable harness
databases. The subsequent record supersedes that local-runtime/UI limitation;
production still retains its approved prior schema/code.
Department hierarchy, postings, legacy HR handoff and Pharma credential/outlet
specialization remain unfinished. No issue was closed or tagged complete and no
new commit, merge, production migration or rollout occurred.
