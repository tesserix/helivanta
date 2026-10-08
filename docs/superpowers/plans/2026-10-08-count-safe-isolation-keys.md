# Plan: test isolation keys survive `go test -count=N` (#929)

Spec: `docs/superpowers/specs/2026-10-08-count-safe-isolation-keys-design.md`

- [x] Reproduce on `main`: `-race -count=3` over `pkg/events` and
  `reference` gave nine failures across five tests.
- [x] `internal/testinfra/isolation.go`: `IsolationKey(t)`.
- [x] Replace every `t.Name()` isolation key (25 sites) and correct the
  comments in `harness.go` and `containers.go`.
- [x] `internal/archtest/isolation_key_test.go`: the rule and its detector
  self-test.
- [x] `internal/testinfra/isolation_test.go`: stability and per-iteration
  uniqueness.
- [x] `docs/standards/backend.md` §9: shared containers, isolation keys,
  re-running with `-count`.
- [x] Mutations per spec §Tests.
- [x] Gates:
  - `-count=3` over the affected packages;
  - `go test -race -count=2 ./...` over the whole backend;
  - `golangci-lint`;
  - `./scripts/coverage-gate.sh`.
