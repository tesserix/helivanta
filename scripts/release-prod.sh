#!/usr/bin/env bash
# Fast-forwards release/prod, which Kargo auto-promotes to production, to a published commit.
# Usage: scripts/release-prod.sh [commit]   (default: the deploy branch head)
set -euo pipefail

readonly branch=release/prod
readonly registry=ghcr.io/tesserix/helivanta
readonly images=(helivanta-api helivanta-shell)
# Kargo's Warehouse follows this tag; its Freight criteria require each image's revision to equal release/prod.
readonly release_tag=release-prod
readonly revision_key=org.opencontainers.image.revision
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

if [ -n "$current" ] && [ "$current" != "$target" ] && ! git merge-base --is-ancestor "$current" "$target"; then
  die "$target is behind $branch ($current); merge a revert to main, or promote older Freight in Kargo"
fi

# Resolve each digest once so the signature checked is the exact artefact the tag will name.
digests=()
for image in "${images[@]}"; do
  repo="$registry/$image"
  digest=$(crane digest "$repo:$target") || die "$repo:$target is not published"
  [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || die "$repo:$target resolved to an invalid digest: $digest"
  revision=$(crane manifest "$repo@$digest" | jq -r --arg key "$revision_key" '.annotations[$key] // empty') ||
    die "cannot read the manifest of $repo@$digest"
  [ "$revision" = "$target" ] ||
    die "$repo@$digest is annotated with revision '${revision:-none}', not $target; rebuild it on main"
  # A failed build on main leaves its commit on deploy's history without signed images.
  cosign verify --certificate-identity "$signer" --certificate-oidc-issuer "$issuer" \
    "$repo@$digest" >/dev/null ||
    die "$repo@$digest is not signed by the main images workflow"
  digests+=("$digest")
done

# Tags move before the branch: until release/prod matches their revision, Kargo creates no Freight.
for i in "${!images[@]}"; do
  crane tag "$registry/${images[$i]}@${digests[$i]}" "$release_tag" ||
    die "cannot point $registry/${images[$i]}:$release_tag at ${digests[$i]}"
done

summary() {
  echo "$1"
  for i in "${!images[@]}"; do echo "${images[$i]}:$release_tag -> ${digests[$i]}"; done
}
if [ "$current" = "$target" ]; then
  summary "$branch is already at $target"
  exit 0
fi

git push --quiet origin "$target:refs/heads/$branch"
summary "$branch -> $target"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  summary "$branch -> $target (from ${current:-none})" >>"$GITHUB_STEP_SUMMARY"
fi
