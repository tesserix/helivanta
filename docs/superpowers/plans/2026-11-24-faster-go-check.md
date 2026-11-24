# Plan: a faster required `go` check

Spec: [2026-11-24-faster-go-check-design.md](../specs/2026-11-24-faster-go-check-design.md). Issue #1832.

1. `backend/scripts/coverage-gate.sh`: add `run` and `check` modes. Keep the no-argument mode as one local run. Read every `mode:` header in the merged profile, and fail on an empty profile.
2. `backend/scripts/coverage-shard-weights.txt`: per-package seconds from CI run 45309457674.
3. `backend/scripts/coverage-gate.test.sh`: shard merge, missing shard, out-of-range shard and failing shard cases.
4. `scripts/ci/go-inputs-changed.sh` and its test.
5. `.github/workflows/ci.yml`: add `go-changes`, `go-static`, `go-test` (a 3-shard matrix) and the required `go` job. Drop the focused steps the gate repeats.
6. `docs/standards/backend.md`: describe the CI shards and the skip rule.
7. Hosted validation: this PR's CI run must show three shards, the merged check passing, and `go` green.
