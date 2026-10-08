# Shell scripts are checked like code

**Issue:** [#924](https://github.com/tesserix/helivanta/issues/924)

## The problem

No linter runs against the repo's shell scripts. Several of them are
controls:
- `preflight.sh` fails a stack closed;
- `coverage-gate.sh` enforces the backend's 70% floor;
- `e2e.sh` owns the fixture swaps between suite phases;
- `repo-owns.sh` decides what `dev-down.sh` may kill.

A quoting or errexit bug in a control fails toward "the gate passed" far more
often than toward an error. `docs/standards/frontend.md` recorded the gap,
which is the weakest control the repo recognises.

## Decisions

### D1: `make lint-shell` runs shellcheck at every severity, in CI's `scripts` job

`scripts/lint-shell.sh` runs `shellcheck` over every shell script. CI's
`scripts` job runs it via `make lint-shell`, beside `make test-scripts`.
Every severity counts, including `info` and `style`.

The tree was already close to clean: four findings in nine scripts. That
makes the strictest bar cheap to hold now, and expensive to reintroduce
later.

### D2: Files are discovered, never listed

Every tracked file (`git ls-files`) is a candidate. A file is checked if
either:
- its name ends in `.sh` or `.bash`; or
- its first line is a `sh`/`bash`/`dash`/`ksh` shebang.

`zsh` is deliberately not matched, because shellcheck does not support it.

A list would be a rule someone has to remember to update when adding a
script. Discovering nothing is a failure, never a pass over zero files.

### D3: shellcheck is pinned by digest and run in Docker

The image is `koalaman/shellcheck:v0.10.0@sha256:2097951f…`. Findings differ
between releases: Ubuntu 24.04's package is 0.9.0, Homebrew is newer. An
unpinned binary would let the same tree pass on one machine and fail on
another. Docker is already a hard requirement of the dev stack and is present
on the CI runner, so this adds no dependency.

### D4: Sourced files are followed

`.shellcheckrc` sets `source-path=SCRIPTDIR` and `external-sources=true`.
Each `.` line carries a `# shellcheck source=` directive, relative to the
sourcing script, so a function or variable in `repo-owns.sh` is judged in the
script that uses it. A source that cannot be resolved is SC1091, which fails
the gate like any other finding.

### D5: Findings are fixed, or disabled at their site with the reason

| Finding | Resolution |
|---|---|
| SC1091 ×4: sourced files not followed | `source=` directives (D4) |
| SC2034: `busy_pid` unused in `preflight.test.sh` | Fixed: the fixture's pid was never used, so the assignment is gone |
| SC2012: `ls -ld … \| cut -c9` in `preflight.sh` | Disabled on that one `if`. The check reads only the mode string's other-write character, at a fixed position for every name, so SC2012's filename-parsing concern does not apply. `stat` takes different flags on GNU and BSD. |
| SC2016: single-quoted `$REPO_ROOT` in a test needle | Disabled at the site. The needle is `reset-dev.sh`'s source text, so the literal is the point. |

There is no blanket exclusion and no `disable=` in `.shellcheckrc`.

### D6: No `shfmt`

`shellcheck` checks correctness, and that is this gate's bar. `shfmt` would
reformat every script, which is a rewrite #924 explicitly puts out of scope.
Its findings would also be layout, which cannot change a gate's verdict. If
consistent layout is ever wanted, it is a separate change, made as a single
mechanical commit.

## Not covered

- Inline `run:` blocks in `.github/workflows/*.yml`. Checking those is
  `actionlint`'s job, which embeds shellcheck, and it is a separate tool and
  decision.
- Shell embedded in the `Makefile`'s recipes.

## Tests

- **Discovery, in `scripts/preflight.test.sh`.** A throwaway git repo with
  `lint-shell.sh` copied in, with `LINT_SHELL_LIST=1`:
  - checked: `.sh`; `.bash`; `env bash`, `/bin/sh` and `/bin/bash -eu`
    shebangs;
  - not checked: a `zsh` shebang, a `python3` shebang, a shebang on line 2
    of a text file, and an untracked `.sh`;
  - a repo with no scripts exits 1.
- **Discovery mutations, each shown failing the suite:**
  - widening the interpreter match to any `*sh`;
  - dropping the empty guard;
  - matching by extension only.
- **The gate itself, shown failing:**
  - an appended `rm -rf $HOME/$1` (SC2115, SC2086);
  - a `.` of a missing file (SC1091).
  CI runs the real gate on every push.
