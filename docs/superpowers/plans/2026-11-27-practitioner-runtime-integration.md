# Professional source runtime execution

See the [specification](../specs/2026-11-27-practitioner-runtime-integration.md) and [full #169 design](../../design/pharmacist-verification.md).

- [x] Reproduce the omitted professional owner through a failing bootstrap test.
- [x] Register the owner and reconcile independent architecture/permission assertions.
- [x] Add explicit Global API mapping and compiled-unreleased route coverage.
- [x] Connect the existing evidence workflow to a dedicated, least-privilege worker; cover startup/credential failure paths.
- [x] Include the binary and deployment configuration through the owning GitOps repositories.
- [x] Generate/check runtime contracts; align source ownership and deployment documentation.
- [ ] Run focused local tests, builds, linters and hosted role/RLS/workflow acceptance.
- [ ] Obtain specific live migration/worker/configuration approval before rollout.
- [ ] Verify deployed source behavior without asserting clinical authority.

The complete verification workflow remains a separate, unfinished goal throughout this source integration.

## Validation evidence (2026-11-27)

The bootstrap and gateway tests were observed failing before the owner/mapping
changes; the worker startup tests failed before their implementation. A deliberate
professional-route manifest mutation to `baseline` is rejected by the new release
regression. The canonical generated manifest is restored before the passing run.

Commands completed locally without running an application or database:

- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check`
- `go test -race ./cmd/practitioner-evidence-worker -count=1`
- Seven explicitly selected pure bootstrap tests, with `-race`
- Eight explicitly selected static architecture/matrix tests
- `go test ./internal/globalapi -run '^TestEveryRegisteredRouteReachesItsProductOwner$' -count=1`
- `go test -race ./internal/release ./cmd/contractdocs -count=1`
- `go vet ./cmd/practitioner-evidence-worker ./internal/bootstrap ./internal/globalapi ./cmd/contractdocs`
- Scoped `golangci-lint run` for those command/bootstrap/gateway targets
- `go build -o /private/tmp/helivanta-169-runtime-build/ ./cmd/api ./cmd/migrate ./cmd/global-api ./cmd/contractdocs ./cmd/practitioner-evidence-worker`
- `node scripts/generate-release-capabilities.mjs --check` and scoped Prettier

The corresponding infrastructure candidate has 47 passing focused rendered-chart
tests, both affected Helm chart lints and one mocked professional-path Terraform
plan test. Those are configuration assertions, not actual GCS/RLS acceptance.

One mistakenly unfiltered bootstrap test command attempted container-backed tests
and failed because rootless Docker is absent. No service or container started;
the command scope was corrected. Full real-dependency tests remain hosted-CI
evidence, not replaced by filtered local checks.

Migration/credential/workload approval, hosted checks and live acceptance remain
open. The overall #169 clinical workflow remains unfinished and disabled.
