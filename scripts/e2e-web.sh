#!/usr/bin/env bash
# Builds and runs the web apps the e2e suite drives — as PRODUCTION
# standalone servers, never `next dev` (#976, design spec
# docs/superpowers/specs/2026-10-12-e2e-against-production-builds-design.md).
#
#   scripts/e2e-web.sh build           build every app the three e2e stacks need
#   scripts/e2e-web.sh start NAME PORT run one built app in the foreground
#
# Why production builds: under `next dev`, the shell's rewrites() proxy
# intermittently got `read ECONNRESET` from a zone's dev server for one
# Turbopack chunk, answered 500, and the page never hydrated (#976). That
# path — on-demand chunk compilation, the HMR client — does not exist in
# production, so e2e was testing a server production never runs.
#
# Why three shell builds (spec D2): NEXT_PUBLIC_ZITADEL_CLIENT_ID is inlined
# into the client bundle and rewrites() destinations (API_URL) are resolved
# at BUILD time, and the main, idle-timeout and renewal stacks differ in
# exactly those two values.
#
# Why builds live in $REPO_ROOT/.e2e-web (spec D3): each is copied out of
# apps/<app>/.next the way Dockerfile.shell assembles the image, so the next
# build of the same app can overwrite .next; and the standalone server.js
# chdirs into its own folder, while e2e.sh only stops processes whose cwd is
# inside this repository (scripts/lib/repo-owns.sh).
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
OUT="$REPO_ROOT/.e2e-web"
SECRETS="$REPO_ROOT/dev/zitadel/secrets"
ISSUER=${NEXT_PUBLIC_ZITADEL_ISSUER_URL:-${ZITADEL_ISSUER_URL:-http://auth.tesserix.localhost:20080}}
API_PORT=${HELIVANTA_API_PORT:-8080}
IDLE_API_PORT=${HELIVANTA_IDLE_API_PORT:-8099}
RENEWAL_API_PORT=${HELIVANTA_RENEWAL_API_PORT:-8098}

# client_id_from FILE — the ZITADEL_CLIENT_ID a dev env file provisions.
# Fails loudly rather than building a shell with an empty client id, which
# would only surface later as a sign-in that silently goes nowhere.
client_id_from() {
  local file=$1 id
  if [ ! -s "$file" ]; then
    echo "$file does not exist or is empty — run 'make dev-infra' first." >&2
    exit 1
  fi
  # shellcheck disable=SC1090 # provisioned at runtime by scripts/zitadel-bootstrap.mjs
  id=$(set -a; . "$file"; printf '%s' "${ZITADEL_CLIENT_ID:-}")
  if [ -z "$id" ]; then
    echo "$file sets no ZITADEL_CLIENT_ID." >&2
    exit 1
  fi
  printf '%s' "$id"
}

# assemble APP NAME — copy apps/APP's standalone build into $OUT/NAME, with
# static assets and public/ beside the server exactly as Dockerfile.shell
# lays them out (its COPY --from=builder lines).
assemble() {
  local app=$1 name=$2 src="$REPO_ROOT/apps/$1"
  rm -rf "${OUT:?}/$name"
  mkdir -p "$OUT/$name"
  cp -R "$src/.next/standalone/." "$OUT/$name/"
  mkdir -p "$OUT/$name/apps/$app/.next"
  cp -R "$src/.next/static" "$OUT/$name/apps/$app/.next/static"
  if [ -d "$src/public" ]; then
    cp -R "$src/public" "$OUT/$name/apps/$app/public"
  fi
  printf '%s' "$app" >"$OUT/$name/.app"
}

# build_app APP NAME CLIENT_ID API_URL — one production build, assembled.
build_app() {
  local app=$1 name=$2 client_id=$3 api_url=$4
  echo "Building $name (apps/$app, API_URL=$api_url)…"
  (
    cd "$REPO_ROOT"
    NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 \
      NEXT_PUBLIC_ZITADEL_ISSUER_URL="$ISSUER" \
      NEXT_PUBLIC_ZITADEL_CLIENT_ID="$client_id" \
      API_URL="$api_url" \
      pnpm --filter "@helivanta/$app" build
  )
  assemble "$app" "$name"
}

build() {
  local main idle renewal
  main=$(client_id_from "$SECRETS/zitadel.env")
  idle=$(client_id_from "$SECRETS/zitadel-idle-timeout.env")
  renewal=$(client_id_from "$SECRETS/zitadel-renewal.env")

  # Zones first and against the main stack — what they served under
  # `next dev` too (spec D2).
  build_app medicore medicore "$main" "http://localhost:$API_PORT"
  build_app pharmacy pharmacy "$main" "http://localhost:$API_PORT"
  build_app lab lab "$main" "http://localhost:$API_PORT"
  build_app shell shell-main "$main" "http://localhost:$API_PORT"
  build_app shell shell-idle-timeout "$idle" "http://localhost:$IDLE_API_PORT"
  build_app shell shell-renewal "$renewal" "http://localhost:$RENEWAL_API_PORT"
  echo "Built every e2e web app into $OUT."
}

# start NAME PORT — run one assembled build in the foreground (the caller
# backgrounds it). API_URL is passed at RUNTIME too, as the image does
# (Dockerfile.shell's runtime ENV API_URL): server-side code reads it then.
start() {
  local name=$1 port=$2 app api_url
  if [ ! -f "$OUT/$name/.app" ]; then
    echo "$OUT/$name is not built — run '$0 build' first." >&2
    exit 1
  fi
  app=$(cat "$OUT/$name/.app")
  case "$name" in
    shell-idle-timeout) api_url="http://localhost:$IDLE_API_PORT" ;;
    shell-renewal) api_url="http://localhost:$RENEWAL_API_PORT" ;;
    *) api_url="http://localhost:$API_PORT" ;;
  esac
  cd "$OUT/$name"
  NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT="$port" API_URL="$api_url" \
    exec node "apps/$app/server.js"
}

case "${1:-}" in
  build) build ;;
  start)
    [ $# -eq 3 ] || { echo "usage: $0 start NAME PORT" >&2; exit 2; }
    start "$2" "$3"
    ;;
  *)
    echo "usage: $0 build | start NAME PORT" >&2
    exit 2
    ;;
esac
