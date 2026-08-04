#!/usr/bin/env bash
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"
out=$(go test -race -count=1 -cover ./... 2>&1) || { echo "$out"; exit 1; }
echo "$out"
is_gated() {
  case "$1" in
    github.com/tesserix/hms/internal/modules/*|github.com/tesserix/hms/pkg/*|github.com/tesserix/hms/internal/platform|github.com/tesserix/hms/internal/platform/*) return 0 ;;
    *) return 1 ;;
  esac
}

fail=0
while read -r pkg pct; do
  is_gated "$pkg" || continue
  p="${pct%\%}"
  if awk "BEGIN{exit !($p < $FLOOR)}"; then
    echo "FAIL coverage $pkg: ${pct} < ${FLOOR}%"
    fail=1
  fi
done < <(echo "$out" | awk '$1=="ok" && /coverage:/{for(i=1;i<=NF;i++) if($i=="coverage:") print $2, $(i+1)}')

# Packages with no test files never get an "ok" prefix. Depending on the
# Go version this shows up either as the classic "?  pkg  [no test files]"
# line, or (Go 1.20+ with -cover) as a bare "pkg  <tab>  coverage: 0.0% of
# statements" line with no "ok"/"FAIL" prefix at all. Catch both: any line
# whose first field names a gated package directly, or whose first field
# is "?", means no test file backed that package.
while read -r pkg; do
  is_gated "$pkg" || continue
  echo "FAIL coverage $pkg: no test files"
  fail=1
done < <(echo "$out" | awk '
  $1=="?" {print $2; next}
  $1 ~ /^github\.com\// {print $1}
')

exit $fail
