#!/usr/bin/env bash
# Tests for scripts/release-prod.sh against a local bare remote, with cosign shimmed.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
SIGNER='https://github.com/tesserix/helivanta/.github/workflows/images.yml@refs/heads/main'
passed=0
failed=0

t_ok()   { passed=$((passed + 1)); printf '  ok    %s\n' "$1"; }
t_fail() { failed=$((failed + 1)); printf '  FAIL  %s\n' "$1"; }

assert_status() { # assert_status NAME WANT GOT
  if [ "$2" = "$3" ]; then t_ok "$1"; else
    t_fail "$1"; printf '        want exit %s, got %s\n' "$2" "$3"
  fi
}

assert_failed() { # assert_failed NAME GOT
  if [ "$2" != 0 ]; then t_ok "$1"; else t_fail "$1"; printf '        want non-zero exit\n'; fi
}

assert_equals() { # assert_equals NAME WANT GOT
  if [ "$2" = "$3" ]; then t_ok "$1"; else
    t_fail "$1"; printf '        want [%s], got [%s]\n' "$2" "$3"
  fi
}

assert_contains() { # assert_contains NAME HAYSTACK NEEDLE
  case "$2" in
    *"$3"*) t_ok "$1" ;;
    *) t_fail "$1"; printf '        output did not contain: %s\n' "$3" ;;
  esac
}

export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
remote="$tmp/remote.git"
work="$tmp/work"
shims="$tmp/shims"
export COSIGN_LOG="$tmp/cosign.log"

mkdir -p "$shims"
cat >"$shims/cosign" <<'SHIM'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$COSIGN_LOG"
if [ -n "${COSIGN_REJECT:-}" ]; then
  case "$*" in *"$COSIGN_REJECT"*) echo "no matching signatures" >&2; exit 1 ;; esac
fi
exit 0
SHIM
chmod +x "$shims/cosign"

git init -q --bare -b main "$remote"
git init -q -b main "$work"
git -C "$work" remote add origin "$remote"
commit() { # commit MESSAGE -> prints sha
  git -C "$work" -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m "$1"
  git -C "$work" rev-parse HEAD
}
remote_ref() { git --git-dir="$remote" rev-parse --verify --quiet "refs/heads/$1"; }
release() { # release [COMMIT] -> sets out, status
  : >"$COSIGN_LOG"
  out=$(cd "$work" && PATH="$shims:$PATH" bash "$REPO_ROOT/scripts/release-prod.sh" "$@" 2>&1)
  status=$?
}

c1=$(commit one)
c2=$(commit two)
c3=$(commit three)
git -C "$work" push -q origin "$c3:refs/heads/main" "$c2:refs/heads/deploy"

echo "release-prod.sh"

release
assert_status "creates release/prod at the deploy head when absent" 0 "$status"
assert_equals "release/prod points at the deploy head" "$c2" "$(remote_ref release/prod)"
assert_contains "verifies the api image of that commit" "$(cat "$COSIGN_LOG")" "ghcr.io/tesserix/helivanta/helivanta-api:$c2"
assert_contains "verifies the shell image of that commit" "$(cat "$COSIGN_LOG")" "ghcr.io/tesserix/helivanta/helivanta-shell:$c2"
assert_contains "requires the main images workflow signer" "$(cat "$COSIGN_LOG")" "--certificate-identity $SIGNER"
assert_contains "requires the GitHub Actions issuer" "$(cat "$COSIGN_LOG")" "--certificate-oidc-issuer https://token.actions.githubusercontent.com"

release
assert_status "is a no-op when release/prod is already at the commit" 0 "$status"
assert_contains "says it is already released" "$out" "already"

release "$c3"
assert_failed "refuses a commit whose images have not advanced deploy" "$status"
assert_equals "leaves release/prod unchanged after that refusal" "$c2" "$(remote_ref release/prod)"

git -C "$work" checkout -q -b side "$c1"
side=$(commit side)
git -C "$work" push -q origin "$side:refs/heads/feat/side"
git -C "$work" checkout -q main
release "$side"
assert_failed "refuses a published feature-branch commit that is not on deploy" "$status"

release deadbeefdeadbeefdeadbeefdeadbeefdeadbeef
assert_failed "refuses an unknown commit" "$status"

release main
assert_failed "refuses a branch name instead of a commit id" "$status"
assert_contains "explains that a commit id is required" "$out" "commit id"

release --upload-pack=true
assert_failed "refuses an option-like argument" "$status"
assert_equals "leaves release/prod unchanged after invalid input" "$c2" "$(remote_ref release/prod)"

git -C "$work" push -q origin "$c3:refs/heads/deploy"
release
assert_status "fast-forwards to a newer deploy head" 0 "$status"
assert_equals "release/prod follows the new deploy head" "$c3" "$(remote_ref release/prod)"

release "$c1"
assert_failed "refuses to move release/prod backwards" "$status"
assert_contains "points rollback at a revert or a Kargo promotion" "$out" "revert"
assert_equals "leaves release/prod unchanged after a backwards request" "$c3" "$(remote_ref release/prod)"

c4=$(commit four)
c5=$(commit five)
git -C "$work" push -q origin "$c5:refs/heads/main" "$c5:refs/heads/deploy"
COSIGN_REJECT=helivanta-shell release "$c4"
assert_failed "refuses a commit whose shell image signature does not verify" "$status"
assert_equals "leaves release/prod unchanged after a signature failure" "$c3" "$(remote_ref release/prod)"

release "${c4:0:12}"
assert_status "releases an explicit older deploy commit by short id" 0 "$status"
assert_equals "release/prod is the full id of that commit" "$c4" "$(remote_ref release/prod)"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
