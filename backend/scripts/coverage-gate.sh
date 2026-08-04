#!/usr/bin/env bash
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"
out=$(go test -race -count=1 -cover ./... 2>&1) || { echo "$out"; exit 1; }
echo "$out"
fail=0
while read -r pkg pct; do
  case "$pkg" in
    github.com/tesserix/hms/internal/modules/*|github.com/tesserix/hms/pkg/*|github.com/tesserix/hms/internal/platform|github.com/tesserix/hms/internal/platform/*) ;;
    *) continue ;;
  esac
  p="${pct%\%}"
  if awk "BEGIN{exit !($p < $FLOOR)}"; then
    echo "FAIL coverage $pkg: ${pct} < ${FLOOR}%"
    fail=1
  fi
done < <(echo "$out" | awk '$1=="ok" && /coverage:/{for(i=1;i<=NF;i++) if($i=="coverage:") print $2, $(i+1)}')
exit $fail
