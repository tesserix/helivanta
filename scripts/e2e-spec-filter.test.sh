#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib/e2e-spec-filter.sh
. "$REPO_ROOT/scripts/lib/e2e-spec-filter.sh"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/helivanta-e2e-filter.XXXXXX")
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/e2e/tests"
touch "$fixture/e2e/tests/pharmacy-stocktake-reviewed.spec.ts"
validate_e2e_spec_filter "$fixture" ""
validate_e2e_spec_filter "$fixture" pharmacy-stocktake-reviewed.spec.ts
for invalid in missing.spec.ts ../pharmacy-stocktake-reviewed.spec.ts /tmp/foreign.spec.ts --help 'a.spec.ts --project=bulk' 'a.spec.ts;false' 'a.spec.ts
false'; do
  if validate_e2e_spec_filter "$fixture" "$invalid" >/dev/null 2>&1; then
    echo "accepted an invalid focused E2E selection" >&2
    exit 1
  fi
done
echo "focused E2E selection tests passed"
