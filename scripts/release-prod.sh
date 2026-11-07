#!/usr/bin/env bash
# Fast-forwards release/prod, which Kargo auto-promotes to production, to a published commit.
# Usage: scripts/release-prod.sh [commit]   (default: the deploy branch head)
set -euo pipefail

readonly branch=release/prod
readonly registry=ghcr.io/tesserix/helivanta
readonly signer='https://github.com/tesserix/helivanta/.github/workflows/images.yml@refs/heads/main'
readonly issuer=https://token.actions.githubusercontent.com

die() { printf 'release-prod: %s\n' "$1" >&2; exit 1; }

git fetch --quiet origin "+refs/heads/deploy:refs/remotes/origin/deploy"
current=
if git ls-remote --exit-code --heads origin "$branch" >/dev/null; then
  git fetch --quiet origin "+refs/heads/$branch:refs/remotes/origin/$branch"
  current=$(git rev-parse "refs/remotes/origin/$branch")
fi

if [ $# -gt 0 ] && ! [[ $1 =~ ^[0-9a-f]{7,40}$ ]]; then
  die "expected a 7-40 character commit id, got: $1"
fi
requested=${1:-refs/remotes/origin/deploy}
target=$(git rev-parse --verify --quiet "${requested}^{commit}") || die "unknown commit: $requested"

# deploy only advances after both images were built, scanned, signed and pushed.
git merge-base --is-ancestor "$target" refs/remotes/origin/deploy ||
  die "$target is not on deploy yet; wait for its images workflow to finish"

if [ "$current" = "$target" ]; then
  echo "$branch is already at $target"
  exit 0
fi
if [ -n "$current" ] && ! git merge-base --is-ancestor "$current" "$target"; then
  die "$target is behind $branch ($current); merge a revert to main, or promote older Freight in Kargo"
fi

# A failed build on main leaves its commit on deploy's history without images.
for image in helivanta-api helivanta-shell; do
  cosign verify --certificate-identity "$signer" --certificate-oidc-issuer "$issuer" \
    "$registry/$image:$target" >/dev/null ||
    die "$registry/$image:$target is not signed by the main images workflow"
done

git push --quiet origin "$target:refs/heads/$branch"
echo "$branch -> $target"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '%s -> %s (from %s)\n' "$branch" "$target" "${current:-none}" >>"$GITHUB_STEP_SUMMARY"
fi
