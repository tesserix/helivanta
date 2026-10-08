# Plan: a listener nobody can attribute is not a free port (#927)

Spec: `docs/superpowers/specs/2026-10-08-port-is-ours-fail-closed-design.md`

- [x] `scripts/lib/repo-owns.sh`:
  - `port_holders` status 3 on Linux (a row without `pid=`) and on Darwin
    (`netstat` LISTEN with no `lsof` pid);
  - status 2 when `ss` or `netstat` fails or is missing;
  - `port_free`;
  - `port_is_ours` handles status 3, with the compose exception;
  - the #927 paragraph in `port_tool`'s comment is replaced by what is now
    true.
- [x] `scripts/preflight.sh check_ports`: its own message for status 3.
- [x] `scripts/e2e.sh stop_port_if_ours`: refuse on status 3; the wait loop
  uses `port_free`. errexit-safe capture of the status.
- [x] `scripts/dev-down.sh`: SKIPPED line for status 3; "Still listening"
  uses `port_free`.
- [x] `scripts/preflight.test.sh`: the stubbed Linux and Darwin cases and the
  real foreign-listener case, per spec §Tests.
- [x] `ci.yml` scripts job: `PREFLIGHT_TEST_REQUIRE_FOREIGN=1`.
- [x] Mutations per spec §Tests, each shown failing and then reverted.
- [x] Gates: `make test-scripts` as a non-root user with passwordless sudo
  and `PREFLIGHT_TEST_REQUIRE_FOREIGN=1`, the way CI runs it (104 passed);
  `pnpm turbo lint format:check`.
