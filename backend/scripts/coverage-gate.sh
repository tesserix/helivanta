#!/usr/bin/env bash
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"
weights="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/coverage-shard-weights.txt"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Usage (#1832):
#   coverage-gate.sh                            test every package, then check the floor
#   coverage-gate.sh run <shard> <total> <dir>  test one CI shard's packages into <dir>
#   coverage-gate.sh check <dir>                check the floor over every shard in <dir>/*/
# CI runs the shards on separate runners and checks once over their merged
# results, so the floor and the no-test-files rule judge the same evidence as
# a single local run.
usage() {
  echo 'usage: coverage-gate.sh [run <shard> <total> <dir> | check <dir>]' >&2
  exit 2
}

# Longest package first onto the least-loaded shard. The weights only balance
# the shards: an unlisted package weighs 1s and still lands in exactly one.
shard_packages() {
  go list ./... |
    awk -v weights="$weights" '
      BEGIN { while ((getline line < weights) > 0) if (line !~ /^(#|$)/) { split(line, f, " "); w[f[2]] = f[1] } }
      { print (($0 in w) ? w[$0] : 1), $0 }' |
    sort -k1,1nr -k2,2 |
    awk -v shard="$1" -v total="$2" '{
      b = 1
      for (i = 2; i <= total; i++) if (load[i] < load[b]) b = i
      load[b] += $1
      if (b == shard) print $2
    }'
}

run_shard() {
  if ! [[ $1 =~ ^[1-9][0-9]*$ && $2 =~ ^[1-9][0-9]*$ ]] || (($1 > $2)); then usage; fi
  mkdir -p "$3"
  shard_packages "$1" "$2" >"$3/packages"
  : >"$3/profile"
  : >"$3/output"
  [[ -s "$3/packages" ]] || return 0
  local pkg pkgs=() out
  while IFS= read -r pkg; do pkgs+=("$pkg"); done <"$3/packages"
  # Owner code is also exercised through gateway and cross-module integration tests.
  # Database-heavy package fixtures share the runner; retain concurrency inside each suite.
  out=$(go test -p=1 -race -count=1 -coverpkg=./... -coverprofile="$3/profile" -timeout=40m "${pkgs[@]}" 2>&1) || { echo "$out"; exit 1; }
  echo "$out" | tee "$3/output"
}

# A shard that did not report, or a package tested twice or never, fails
# here: a missing package would otherwise leave its coverage unjudged.
merge_shards() {
  local dirs
  shopt -s nullglob
  dirs=("$1"/*/)
  shopt -u nullglob
  ((${#dirs[@]})) || { echo "FAIL coverage: no shard results in $1"; exit 1; }
  if ! diff <(go list ./... | sort) <(cat "${dirs[@]/%/packages}" | sort) >"$work/packages.diff"; then
    echo 'FAIL coverage: the shards did not test every package exactly once (< untested, > extra)'
    cat "$work/packages.diff"
    exit 1
  fi
  cat "${dirs[@]/%/profile}" >"$work/profile"
  out=$(cat "${dirs[@]/%/output}")
}

case "${1:-}" in
  run)
    (($# == 4)) || usage
    run_shard "$2" "$3" "$4"
    exit 0
    ;;
  check)
    (($# == 2)) || usage
    merge_shards "$2"
    ;;
  "")
    run_shard 1 1 "$work/shards/all"
    merge_shards "$work/shards"
    ;;
  *) usage ;;
esac
profile="$work/profile"
grep -qv '^mode:' "$profile" || { echo 'FAIL coverage: the combined profile is empty'; exit 1; }

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
  /^mode:/ {next}
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
