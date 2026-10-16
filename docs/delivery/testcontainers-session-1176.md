# Per-process Testcontainers sessions (#1176)

Sibling Go test binaries previously derived the same Testcontainers session from their common go-test parent. After all clients left, a later package could reuse a shutting-down reaper and time out before Postgres started.

Testinfra now allocates a random process-owned session before Testcontainers configuration is read. Explicit TESTCONTAINERS_SESSION_ID remains respected for intentional sharing and the existing #963 regression. Ryuk stays enabled with normal cleanup and default timeout. No assertion or package is skipped. This is test configuration, not application identity or tenancy.

The new regression observed identical sessions from sibling subprocesses before the fix. After the change, process sessions differ. The real-Docker test starts NATS in each process, lets the processes exit and verifies automatic removal of both containers. A short 500 ms helper-only grace exercises cleanup; it does not change suite defaults. The existing four-process shared-reaper regression still passes under race detection.

Validation:
- Session isolation and explicit-session preservation passed.
- Real-Docker cleanup and shared-reaper race tests passed.
- golangci-lint: 0 issues; go vet and go build passed.
- govulncheck@v1.9.0 passed with no reachable vulnerable symbols.
- No dependency version changed; existing Docker SDK/error-classifier dependencies are direct test imports.
- Initial broad local run hit a local NATS/SSH-forward timeout. Bounded full race/coverage rerun passed 37 application packages and 1,551 tests/subtests; all 25 gated packages reached at least 70%. The existing logger-writer guard fails under JSON output even on unchanged main; the complete testinfra package was rerun in canonical non-JSON mode with race/coverage and passed. No assertions were changed or disabled. Eight data-only contract packages have no executable tests and retain their existing architecture-checked exemption.

Diagnostics: /tmp/helivanta-1176-red.log, /tmp/helivanta-1176-green-identity.log, /tmp/helivanta-1176-green-docker.log and /tmp/helivanta-1176-full-go-bounded.jsonl. The local Colima port adapter remains outside tracked source.

Go remains 1.27.2. No production migration or business-runtime change is required. Keep #1176 open until hosted CI verifies the full suite and approved release health.

First hosted attempt `38894793923` passed web, browser E2E, scripts, secret scans
and image builds. Its Go runner spent 854 seconds installing the pinned linter,
then exceeded the five-minute package-loading deadline before reaching tests.
The existing build now runs before lint to warm the application compilation
cache; lint receives a ten-minute analysis deadline inside a thirty-minute step
bound that also includes installation. No analyzer, assertion or test is removed.
This separate cold-runner failure is recorded rather than reported as a reaper
regression or hidden by an unchanged retry.
