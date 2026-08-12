#!/usr/bin/env bash
# Verifies that the whole HMS stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

# Same HMS_* host-port variables as the Makefile/preflight (see
# .env.example) — falls back to the stock ports when unset, so this still
# works run standalone with no .env present.
OPENFGA_PORT=${HMS_OPENFGA_PORT:-8090}
GIP_PORT=${HMS_GIP_PORT:-9099}
API_PORT=${HMS_API_PORT:-8080}

fail=0

# check <name> <url> [attempts] [max-time]
#
# Retries because `next dev` compiles a route the first time it is
# requested: a cold zone can take tens of seconds to answer its very first
# request and answer in milliseconds thereafter. Without retrying, the
# first run of this script on a freshly started stack reports FAIL against
# a perfectly healthy app — the worst possible moment to cry wolf, since a
# developer seeing it has no way to tell a cold compile from a real break.
check() {
  local name="$1" url="$2" attempts="${3:-1}" max_time="${4:-5}"
  local i
  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS --max-time "$max_time" "$url" >/dev/null 2>&1; then
      printf '  ok    %-24s %s\n' "$name" "$url"
      return
    fi
    [ "$i" -lt "$attempts" ] && sleep 2
  done
  printf '  FAIL  %-24s %s\n' "$name" "$url"
  fail=1
}

echo "Infrastructure:"
docker compose -f docker-compose.dev.yml ps --status running --format '  ok    {{.Service}}' || fail=1
check "openfga"          "http://localhost:$OPENFGA_PORT/healthz"
check "gip emulator"     "http://localhost:$GIP_PORT/"

echo "Backend:"
# The API exposes /healthz and /readyz (backend/internal/httpserver/server.go)
# — not /health and /ready.
check "api health"       "http://localhost:$API_PORT/healthz"
check "api ready"        "http://localhost:$API_PORT/readyz"

echo "Frontend zones:"
# Each zone app sets basePath in next.config.ts (e.g. medicore is served
# under /medicore, not "/") — the bare root 404s.
#
# 10 attempts x 20s covers a cold `next dev` first-request compile, which
# routinely exceeds the 5s the backend checks use.
check "shell    (4301)"  "http://localhost:4301/login"    10 20
check "medicore (4302)"  "http://localhost:4302/medicore" 10 20
check "pharmacy (4303)"  "http://localhost:4303/pharmacy" 10 20
check "lab      (4304)"  "http://localhost:4304/lab"      10 20

# One authenticated round trip, through the shell rather than straight at
# the API (issue #772). Every check above is an unauthenticated GET for a
# 200, which is why this script reported "All checks passed" throughout
# issue #770 — a bug where every zone proxied /api/* to the wrong service,
# so no permission resolved and the sidebar collapsed to a single zone.
#
# Going via localhost:4301 is the whole point: a call straight to the API
# port would have passed then too. This exercises the real chain — GIP
# emulator, the shell's /api/session route, the /api/* rewrite, the Go
# API's token verification, and OpenFGA actually resolving a permission.
#
# Each hop is reported separately, because "the stack is broken" is not
# actionable but "the session exchange failed" is.
EXPECT_PERMISSION=medicore.visit.read
SEED_USER=test@hms.dev
SEED_PASSWORD=password123

# json_field FIELD — reads JSON on stdin, prints one top-level string field.
# Node rather than python3 or jq: Node 22 is already a hard prerequisite
# (preflight enforces it), so this adds no new dependency.
json_field() {
  node -e '
let s = "";
process.stdin.on("data", (d) => (s += d));
process.stdin.on("end", () => {
  try { process.stdout.write(String(JSON.parse(s)[process.argv[1]] ?? "")); }
  catch { process.stdout.write(""); }
});' "$1" 2>/dev/null
}

round_trip() {
  local jar token status body
  jar=$(mktemp) || return 1

  # 1. Sign in against the emulator, exactly as the browser SDK does.
  token=$(curl -fsS --max-time 10 \
    -X POST "http://localhost:$GIP_PORT/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=demo-key" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$SEED_USER\",\"password\":\"$SEED_PASSWORD\",\"returnSecureToken\":true}" \
    2>/dev/null | json_field idToken)
  if [ -z "$token" ]; then
    printf '  FAIL  %-24s emulator sign-in failed for %s — is the stack seeded? run '"'"'make seed'"'"'\n' \
      "auth round trip" "$SEED_USER"
    rm -f "$jar"
    return 1
  fi

  # 2. Exchange it for the session cookie via the shell's own route.
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -c "$jar" \
    -X POST "http://localhost:4301/api/session" \
    -H 'Content-Type: application/json' \
    -d "{\"idToken\":\"$token\"}" 2>/dev/null)
  if [ "$status" != "200" ]; then
    printf '  FAIL  %-24s POST /api/session returned %s (expected 200)\n' \
      "auth round trip" "$status"
    rm -f "$jar"
    return 1
  fi

  # 3. Call the API *through the shell rewrite*, carrying that cookie.
  body=$(curl -s --max-time 20 -b "$jar" \
    "http://localhost:4301/api/v1/iam/me/permissions" 2>/dev/null)
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -b "$jar" \
    "http://localhost:4301/api/v1/iam/me/permissions" 2>/dev/null)
  rm -f "$jar"

  if [ "$status" != "200" ]; then
    printf '  FAIL  %-24s GET /api/v1/iam/me/permissions via :4301 returned %s\n' \
      "auth round trip" "$status"
    printf '        the shell is proxying /api/* somewhere unexpected — check API_URL\n'
    printf '        and turbo.json globalPassThroughEnv (see issue #770)\n'
    return 1
  fi

  # 4. A 200 is not enough: assert a real permission came back, which only
  #    happens if OpenFGA resolved the seeded grants.
  case "$body" in
    *"$EXPECT_PERMISSION"*)
      printf '  ok    %-24s %s resolved %s\n' "auth round trip" "$SEED_USER" "$EXPECT_PERMISSION"
      return 0
      ;;
    *)
      printf '  FAIL  %-24s 200 but %s missing from the response\n' \
        "auth round trip" "$EXPECT_PERMISSION"
      printf '        permissions resolved empty — OpenFGA tuples may not be rebuilt;\n'
      printf '        restart the API, or run '"'"'make seed'"'"' if this stack was never seeded\n'
      return 1
      ;;
  esac
}

echo "Authenticated round trip:"
round_trip || fail=1

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed. Log in at http://localhost:4301/login"
  echo "  test@hms.dev       / password123  (tenant_admin)"
  echo "  pharmacist@hms.dev / password123  (pharmacist)"
else
  echo "Some checks failed. Common causes:"
  echo "  - 'make dev' not running, or still starting (Next.js takes ~20s)"
  echo "  - NODE_AUTH_TOKEN unset: export NODE_AUTH_TOKEN=\$(gh auth token)"
  echo "  - Docker not running (infra and backend tests both need it)"
  echo "  - /readyz failing on openfga: check 'docker compose -f docker-compose.dev.yml logs openfga'"
fi
exit "$fail"
