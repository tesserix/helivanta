#!/usr/bin/env bash
set -euo pipefail
decide="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/go-inputs-changed.sh"

expect() {
  local got
  got=$(printf '%s' "$2" | bash "$decide")
  if [[ $got != "run=$1" ]]; then
    echo "FAIL: $3: got $got, want run=$1"
    exit 1
  fi
  echo "PASS: $3"
}

expect false $'apps/shell/app/login/page.tsx\ne2e/tests/session-entry.spec.ts\ndocs/superpowers/specs/x.md\n' 'frontend, e2e and docs changes skip Go'
expect false $'pnpm-lock.yaml\npackages/ui/src/zones.ts\n' 'a lockfile and package change skips Go'
expect true $'apps/shell/lib/csp.ts\nbackend/go.mod\n' 'one backend path runs Go'
expect true $'contracts/release-capabilities.json\n' 'contracts are a Go test input'
expect true $'.github/workflows/ci.yml\n' 'the workflow itself runs Go'
expect true $'newtop/file.txt\n' 'an unclassified path runs Go'
expect true $'apps.go\n' 'a root file that only starts like a skipped directory runs Go'
expect true '' 'an unreadable or empty diff runs Go'
