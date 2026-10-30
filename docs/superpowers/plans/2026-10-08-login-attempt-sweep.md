# Plan: sweep expired login attempts (#869)

Spec: `docs/superpowers/specs/2026-10-08-login-attempt-sweep-design.md`

- [x] `loginattempt_sweep.go`:
  - `loginAttemptStore.SweepExpired`: one indexed
    `DELETE … WHERE expires_at < now()` through the system pool, returning the
    count;
  - `RunAttemptSweeper`: a boot pass, then one pass every
    `attemptSweepInterval` (= `loginAttemptTTL`);
  - `sweepAttemptsSafely`: contains a panic, logs an error, and logs the count
    on every pass.
- [x] `cmd/api/main.go`: `go loginUIHandlers.RunAttemptSweeper(ctx)` next to
  `bus.RunPruner`.
- [x] Comments corrected: `loginAttemptTTL` (loginui.go) and `Get`
  (loginattempt.go) no longer say that nothing sweeps the table.
- [x] `loginattempt_sweep_test.go`, per spec §Tests. Rows are inserted with
  plain SQL, so the sweep, not expiry-on-read, is what removes them.
- [x] Mutations, each shown failing the suite and then reverted:
  - comparison inverted;
  - boot sweep removed;
  - tick never sweeping.
- [x] Gates: `make lint-go`, `go test -race ./...`,
  `./scripts/coverage-gate.sh`. The frontend is untouched.
