#!/usr/bin/env bash
# go-inputs-changed.sh — must this pull request run the Go jobs? (#1832)
# Reads the changed paths, one per line, on stdin and prints `run=true` or
# `run=false` for $GITHUB_OUTPUT.
#
# It answers false only when EVERY path is a known non-Go input. Go tests also
# read contracts/, dev/, scripts/ and the root compose file, and a path nobody
# has classified yet may be one of them, so anything unlisted runs the jobs.
# The browser tests that drive apps/ through Go run in the web job, not here.
set -euo pipefail
run=false
seen=false
while IFS= read -r path; do
  [[ -n $path ]] || continue
  seen=true
  case "$path" in
    apps/* | packages/* | e2e/* | docs/* | package.json | pnpm-lock.yaml | pnpm-workspace.yaml | turbo.json) ;;
    *) run=true ;;
  esac
done
# No paths means the diff could not be read, not that nothing changed.
[[ $seen == true ]] || run=true
echo "run=$run"
