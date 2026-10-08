# Plan: go-job container forensics (#963, diagnosability step)

Spec: `docs/superpowers/specs/2026-10-08-go-job-container-forensics-design.md`

- [x] `ci.yml` go job: background `docker events` recorder before
  `coverage-gate.sh`.
- [x] On failure:
  - container death events;
  - `docker ps -a` and inspect of exited containers;
  - kernel OOM lines;
  - the events artifact.
- [x] Local proof: the workflow's `run:` blocks against a SIGKILLed canary.
- [x] CI proof: a temporary deliberately-failing commit, observed and then
  reverted. Run 37727533215 printed the canary's `kill` (signal 9) and `die`
  (exit 137), `OOMKilled=false`, "no OOM kill in the kernel log", and uploaded
  the `go-docker-events` artifact.
- [x] First real occurrence read (run 37727687289): Ryuk's session prune,
  with no OOM kill, while iam was still running (spec D4).
- [x] Follow every Ryuk container's log during the tests; print and upload
  it on failure. Proven locally: the no-reaper line, and a captured canary.
- [x] Second occurrence read (run 37731855071): no Ryuk connection from
  iam was alive when the last other package exited (spec D5).
- [x] Excluded locally: GC finalizing a dropped handle's connection; iam not
  connecting.
- [x] testcontainers' log on in every test binary, pid-prefixed, shown only
  for a failing package; pinned by `TestTestcontainersLogIsOnWithoutVerbose`
  (fails without the init).
- [x] Root cause from the third occurrence: a failed Ryuk handshake on the
  reuse path in testcontainers-go v0.43.0 (spec, Root cause and fix).
- [x] Fix: testcontainers-go and its nats, openfga and postgres modules to
  v0.44.0 (go.mod shows only those four direct changes).
- [x] Regression test `TestConcurrentBinariesAllConnectToTheReaper`: fails
  3 of 3 on v0.43.0 and passes 5 of 5 on v0.44.0.
- [x] Gates: `golangci-lint` 0 issues; `./scripts/coverage-gate.sh` green.
