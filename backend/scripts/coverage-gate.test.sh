#!/usr/bin/env bash
set -euo pipefail

gate="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/coverage-gate.sh"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
export GOWORK=off

new_fixture() {
  rm -rf "$fixture/project"
  mkdir -p "$fixture/project/internal/modules/sample" "$fixture/project/internal/integration"
  cd "$fixture/project"
  cat >go.mod <<'EOF'
module github.com/tesserix/helivanta

go 1.26
EOF
  cat >internal/modules/sample/sample.go <<'EOF'
package sample
func First() int { return 1 }
func Second() int { return 2 }
EOF
  cat >internal/modules/sample/sample_test.go <<'EOF'
package sample_test
import (
 "testing"
 "github.com/tesserix/helivanta/internal/modules/sample"
)
func TestFirst(t *testing.T) {
 if sample.First() != 1 { t.Fatal("wrong first value") }
}
EOF
  cat >internal/integration/sample_test.go <<'EOF'
package integration_test
import (
 "testing"
 "github.com/tesserix/helivanta/internal/modules/sample"
)
func TestSecond(t *testing.T) {
 if sample.Second() != 2 { t.Fatal("wrong second value") }
}
EOF
}

run_gate() { bash "$gate"; }

expect_pass() {
  if ! run_gate >"$fixture/output" 2>&1; then
    cat "$fixture/output"
    echo "FAIL: $1"
    exit 1
  fi
  echo "PASS: $1"
}

expect_fail() {
  if run_gate >"$fixture/output" 2>&1; then
    cat "$fixture/output"
    echo "FAIL: $1 unexpectedly passed"
    exit 1
  fi
  if ! grep -Fq "$2" "$fixture/output"; then
    cat "$fixture/output"
    echo "FAIL: $1 failed for the wrong reason"
    exit 1
  fi
  echo "PASS: $1"
}

new_fixture
expect_pass 'cross-package execution counts toward its source owner'

# Repeating a covered call in another binary must not dilute uncovered blocks.
cat >>internal/modules/sample/sample.go <<'EOF'
func Third() int { return 3 }
EOF
cat >>internal/integration/sample_test.go <<'EOF'
func TestFirstAgain(t *testing.T) {
 if sample.First() != 1 { t.Fatal("wrong first value") }
}
EOF
expect_fail 'uncovered owner statements still fail the 70% floor' 'FAIL coverage github.com/tesserix/helivanta/internal/modules/sample:'

mkdir -p internal/modules/sibling
cat >internal/modules/sibling/sibling.go <<'EOF'
package sibling
func First() int { return 1 }
func Second() int { return 2 }
EOF
cat >internal/modules/sibling/sibling_test.go <<'EOF'
package sibling
import "testing"
func TestValues(t *testing.T) {
 if First() != 1 || Second() != 2 { t.Fatal("wrong sibling values") }
}
EOF
expect_fail 'a fully covered sibling cannot hide an undercovered owner' 'FAIL coverage github.com/tesserix/helivanta/internal/modules/sample:'

new_fixture
rm internal/modules/sample/sample_test.go
cat >>internal/integration/sample_test.go <<'EOF'
func TestFirstWithoutOwnerTests(t *testing.T) {
 if sample.First() != 1 { t.Fatal("wrong first value") }
}
EOF
expect_fail 'cross-package execution cannot replace owner test files' 'no test files'

new_fixture
cat >>internal/modules/sample/sample_test.go <<'EOF'
func TestBroken(t *testing.T) { t.Fatal("synthetic behavior regression") }
EOF
expect_fail 'behavioral failures fail the gate' 'synthetic behavior regression'

new_fixture
mkdir -p internal/modules/sample/contract
cat >internal/modules/sample/contract/data.go <<'EOF'
package contract
type Value struct { Name string }
EOF
expect_pass 'data-only contract packages retain their existing exemption'

mkdir -p internal/modules/untested
cat >internal/modules/untested/untested.go <<'EOF'
package untested
func Value() int { return 1 }
EOF
expect_fail 'an unimported gated package cannot disappear from the gate' 'no test files'

# CI shards (#1832): each runner tests part of the module and one check judges
# the merged evidence. The fixture's two packages carry no weight, so shard 1
# takes internal/integration and shard 2 the owner package.
shards() {
  rm -rf "$fixture/shards"
  for shard in 1 2; do
    bash "$gate" run "$shard" 2 "$fixture/shards/$shard" || return 1
  done
  bash "$gate" check "$fixture/shards"
}
run_gate() { shards; }

new_fixture
expect_pass 'shards merge cross-package execution from another runner'
if [ "$(cat "$fixture/shards/1/packages")" != github.com/tesserix/helivanta/internal/integration ]; then
  echo 'FAIL: the owner package and its cross-package test did not run on separate shards'
  exit 1
fi

run_gate() {
  rm -rf "$fixture/shards"
  bash "$gate" run 1 2 "$fixture/shards/1" && bash "$gate" check "$fixture/shards"
}
expect_fail 'a shard that did not report fails the merged check' 'did not test every package exactly once'

run_gate() { bash "$gate" run 3 2 "$fixture/shards/3"; }
expect_fail 'a shard outside the total is refused' 'usage:'

run_gate() { shards; }
cat >>internal/modules/sample/sample_test.go <<'EOF'
func TestBroken(t *testing.T) { t.Fatal("synthetic shard regression") }
EOF
expect_fail 'a failing test fails its shard' 'synthetic shard regression'
