#!/usr/bin/env bash
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"
profile=$(mktemp)
trap 'rm -f "$profile"' EXIT
# Owner code is also exercised through gateway and cross-module integration tests.
out=$(go test -race -count=1 -coverpkg=./... -coverprofile="$profile" -timeout=40m ./... 2>&1) || { echo "$out"; exit 1; }
echo "$out"
is_gated() {
  case "$1" in
    # A module's contract package holds its published event interface and
    # nothing else: const, type and var only, with no func or method
    # declarations at all (#827). That is not a convention here — it is
    # asserted by TestContractPackagesDeclareOnlyData, which fails on a
    # func declaration and on a func literal bound to a var. A package
    # with no functions has no statements, so `go test -cover` reports it
    # as 0.0%/no test files no matter what anyone writes, and the "no test
    # files" arm below would fail it forever.
    #
    # Exempting a path is normally how a coverage gate rots. This one is
    # safe only because a *separate mechanical check* guarantees there is
    # no behaviour behind the exempted name; if that arch test is ever
    # deleted, this exemption becomes a hole and must go with it.
    github.com/tesserix/helivanta/internal/modules/*/contract) return 1 ;;
    github.com/tesserix/helivanta/internal/modules/*|github.com/tesserix/helivanta/pkg/*|github.com/tesserix/helivanta/internal/platform|github.com/tesserix/helivanta/internal/platform/*) return 0 ;;
    *) return 1 ;;
  esac
}

counts=$(awk '
  NR == 1 {next}
  {
    block = $1
    pkg = block
    sub(/:[^:]*$/, "", pkg)
    sub(/\/[^\/]*$/, "", pkg)
    if (!(block in statements)) {
      statements[block] = $2
      owner[block] = pkg
      total[pkg] += $2
    }
    if ($3 > 0) hit[block] = 1
  }
  END {
    for (block in hit) covered[owner[block]] += statements[block]
    for (pkg in total) if (total[pkg] > 0) print pkg, covered[pkg] + 0, total[pkg]
  }
' "$profile") || { echo 'FAIL coverage: cannot read combined profile'; exit 1; }

fail=0
while read -r pkg covered total; do
  is_gated "$pkg" || continue
  pct=$(awk -v covered="$covered" -v total="$total" 'BEGIN {printf "%.1f", 100 * covered / total}')
  if awk -v covered="$covered" -v total="$total" -v floor="$FLOOR" 'BEGIN {exit !(100 * covered < floor * total)}'; then
    echo "FAIL coverage $pkg: ${pct}% < ${FLOOR}%"
    fail=1
  else
    echo "PASS coverage $pkg: ${pct}% >= ${FLOOR}%"
  fi
done < <(echo "$counts" | sort)

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
