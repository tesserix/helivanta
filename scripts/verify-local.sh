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
