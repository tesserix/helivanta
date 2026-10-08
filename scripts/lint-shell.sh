#!/usr/bin/env bash
# lint-shell.sh — run shellcheck over every shell script in the repo (#924).
#
# Several of these scripts are controls, not conveniences: preflight.sh fails
# a stack closed, coverage-gate.sh enforces the backend's coverage floor,
# e2e.sh owns the fixture swaps. A quoting or errexit bug in a control tends
# to fail in the direction of "the gate passed", so they are checked like
# code.
#
# WHICH FILES is decided here, not listed. A list is a rule a developer has
# to remember to update. Every tracked file is a candidate. One is checked if
# its name ends in .sh or .bash, or if its first line is a sh/bash shebang
# (an executable with no extension). If discovery finds nothing, that is a
# failure, never a pass over zero files.
#
# WHICH SHELLCHECK is pinned by digest and run in a container. Findings
# differ between shellcheck releases, so an unpinned binary would make this
# gate pass on one machine and fail on another for the same tree.
# .shellcheckrc at the repo root holds the configuration. Every severity
# counts, including "style": a finding is fixed or disabled at its site with
# the reason stated there.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
SHELLCHECK_IMAGE=koalaman/shellcheck:v0.10.0@sha256:2097951f02e735b613f4a34de20c40f937a6c8f18ecb170612c88c34517221fb

cd "$REPO_ROOT"

# is_shell FILE — true for a shell script, by extension or by shebang.
is_shell() {
  case "$1" in
    *.sh | *.bash) return 0 ;;
  esac
  [ -f "$1" ] || return 1
  local first
  IFS= read -r first <"$1" 2>/dev/null || return 1
  # sh, bash, dash and ksh are the dialects shellcheck checks; `zsh` must
  # not match, which is why the interpreter name is anchored after / or env.
  [[ $first =~ ^\#!.*(/|env[[:space:]]+)(ba|da|k)?sh([[:space:]]|$) ]]
}

files=()
while IFS= read -r -d '' f; do
  is_shell "$f" && files+=("$f")
done < <(git ls-files -z)

if [ "${#files[@]}" -eq 0 ]; then
  echo "lint-shell: found no shell scripts — refusing to report a pass over nothing (is this a git checkout?)" >&2
  exit 1
fi

if [ "${LINT_SHELL_LIST:-0}" = 1 ]; then
  printf '%s\n' "${files[@]}"
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  echo "lint-shell: Docker is required to run the pinned shellcheck ($SHELLCHECK_IMAGE), and it is not reachable." >&2
  exit 1
fi

echo "shellcheck ($SHELLCHECK_IMAGE) over ${#files[@]} scripts:"
printf '  %s\n' "${files[@]}"
docker run --rm -v "$REPO_ROOT:/mnt:ro" -w /mnt "$SHELLCHECK_IMAGE" "${files[@]}"
echo "shellcheck: clean"
