# Plan: shell scripts are checked like code (#924)

Spec: `docs/superpowers/specs/2026-10-08-shellcheck-gate-design.md`

- [x] `scripts/lint-shell.sh`:
  - discovery by extension or shebang over `git ls-files`;
  - fails on zero files;
  - digest-pinned shellcheck in Docker;
  - `LINT_SHELL_LIST=1` prints the file set.
- [x] `.shellcheckrc`: `source-path=SCRIPTDIR`, `external-sources=true`.
- [x] `source=` directives on the four `.` lines.
- [x] Findings resolved per spec D5.
- [x] `make lint-shell`; CI `scripts` job runs it.
- [x] Docs:
  - `docs/standards/frontend.md` no longer records the gap;
  - `CLAUDE.md` names the gate.
- [x] Discovery tests in `preflight.test.sh`, and the mutations in spec
  §Tests.
- [x] Gates:
  - `make lint-shell` clean;
  - `make test-scripts` as a non-root user with
    `PREFLIGHT_TEST_REQUIRE_FOREIGN=1` (108 passed).
