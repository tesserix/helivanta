# A faster required `go` check

**Issue:** [#1832](https://github.com/tesserix/helivanta/issues/1832).

## Problem

The required `go` check takes 1.5 to 2.3 hours, and every pull request waits for it, including frontend-only ones. The main run for `a6a67bda` (CI run 45309457674) spent about 101 minutes in `./scripts/coverage-gate.sh`. That gate runs `go test -p=1 -race -coverpkg=./... ./...`, so the packages run one after another on one runner. `-p=1` stays (#763): database-heavy suites share a runner. Per-package times: `internal/globalapi` 31 min, `modules/workforce` 24 min, `modules/iam` 14 min, `modules/pharmacy` 7.5 min, the remaining 65 packages about 25 min together. About 25 focused `go test` steps before the gate took another 25 minutes. Each runs a subset of `./...`, which the gate then runs again.

## Decisions

### D1 — Skip the Go jobs only for paths known not to be Go inputs

`scripts/ci/go-inputs-changed.sh` reads the pull request's changed paths and answers `run=false` only when every path is under `apps/`, `packages/`, `e2e/` or `docs/`, or is `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml` or `turbo.json`. Go tests also read `contracts/`, `dev/`, `scripts/` and `docker-compose.dev.yml`, so these paths run. A path nobody has classified also runs. So does an empty or unreadable diff. The Go browser tests that drive `apps/` run in the `web` job, which is unchanged.

The diff is between the pull request's merge commit and its first parent, the base tip. So it is the change against current `main`. Pushes to `main` always run in full: each main run is the record for its commit.

### D2 — The required check stays named `go`

The ruleset requires a check named `go`, so a skipped job would never satisfy it. `go` becomes a small job that needs `go-changes`, `go-static` and `go-test`, and runs unless the workflow was cancelled. It fails unless `go-changes` succeeded. When the Go jobs ran, it also fails unless `go-static` and every `go-test` shard succeeded. The ruleset is unchanged.

### D3 — Three shards, one coverage judgement

`coverage-gate.sh run <shard> <total> <dir>` tests one shard's packages with the same `go test` flags as before, `-p=1` included. It writes the package list, the profile and the output to `<dir>`. `coverage-gate.sh check <dir>` merges every shard and applies the unchanged floor and no-test-files rules. Before that, it fails if the shards did not test the output of `go list ./...` exactly once each, so a missing shard cannot leave a package unjudged. Coverage blocks are merged as before: a block counts as covered if any test binary on any shard hit it. With no arguments the script runs everything as one shard and checks it, the same local command as before.

Packages are assigned longest first, each to the least-loaded shard. The weights come from `backend/scripts/coverage-shard-weights.txt`. A package not listed there weighs 1 second. Weights affect balance only, never which packages run. With the measured weights, three shards come to about 34 minutes each; four would only reach 31, because `internal/globalapi` alone takes that long.

### D4 — Remove the focused steps the gate repeats

Every focused `go test` step whose tests `./...` already runs is removed. They existed to fail early, which a 35-minute shard no longer needs. Two steps stay in `go-static`: the drug master latency check, which needs `HELIVANTA_LATENCY_CHECK` and must be measured alone, and `coverage-gate.test.sh`. The #963 Docker and Ryuk forensics move to each shard, with one artifact per shard.

## Not in this slice

- Per-package databases so that `-p` can go above 1.
- Splitting `internal/globalapi`, which sets the 31-minute floor.
- Larger runners.

## Verification

- `scripts/ci/go-inputs-changed.test.sh` runs before the decision. It covers skip, run, unclassified-path and empty-diff cases, and was shown to fail against a classifier that skipped `contracts/`.
- `coverage-gate.test.sh` adds four cases. Two shards merge cross-package coverage that neither shard has alone, and the test asserts the two packages ran on different shards. A shard that did not report fails the check. A shard number above the total is refused. A failing test fails its shard.
- This pull request changes `ci.yml`, so its own run exercises the shards and the merged check end to end.
