#!/usr/bin/env bash
# Verifies that the whole HMS stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

# Same HMS_* host-port variables as the Makefile/preflight (see
# .env.example) — falls back to the stock ports when unset, so this still
# works run standalone with no .env present.
OPENFGA_PORT=${HMS_OPENFGA_PORT:-8090}
ZITADEL_PORT=${HMS_ZITADEL_PORT:-20080}
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
# /debug/healthz, not the compose healthcheck's own /app/zitadel ready — the
# latter is documented as unreliable on v4.15.3 (docker-compose.dev.yml's
# zitadel service comment): it reports "not ready" even while serving real
# traffic, so trusting it here would produce the exact false FAIL this
# script's whole retry-with-attempts design exists to avoid.
check "zitadel"           "http://localhost:$ZITADEL_PORT/debug/healthz"

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

# One authenticated round trip, at the API level (#838 Task 6). This used
# to go through the shell's own /api/session route (issue #772) — every
# unauthenticated GET above is why this script reported "All checks passed"
# throughout issue #770, a bug where every zone proxied /api/* to the wrong
# service, so no permission resolved and the sidebar collapsed to one zone.
#
# It cannot go through the shell anymore: apps/shell still authenticates
# via Firebase (Task 6 is dev stack/seeding only, the frontend is a
# separate follow-on), so /api/session no longer exists on any path a real
# Zitadel token can take. This instead verifies what IS wired today,
# straight against the API: a real Zitadel token (via the actual hosted
# login UI, not a shortcut) → POST /v1/auth/login → an HMS session that
# resolves a real permission on /v1/iam/me/permissions. Each hop is still
# reported separately by scripts/zitadel-verify-login.mjs, because "the
# stack is broken" is not actionable but "the session exchange failed" is.
#
# Deliberately NOT re-verified here: the shell's own auth wiring
# (/api/session, the /api/* rewrite through :4301) — that is untestable
# until the frontend is ported off Firebase, and this script must not
# pretend otherwise.
echo "Authenticated round trip (API-level — see script comment for what this does and does not cover):"
node scripts/zitadel-verify-login.mjs || fail=1

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed."
  echo "http://localhost:4301/login renders but cannot sign these accounts in yet —"
  echo "apps/shell still authenticates via Firebase (see README's Quick start)."
  echo "Zitadel accounts, login-verified by 'make seed':"
  echo "  test@hms.dev       / HmsDev123!  (tenant_admin)"
  echo "  pharmacist@hms.dev / HmsDev123!  (pharmacist)"
else
  echo "Some checks failed. Common causes:"
  echo "  - 'make dev' not running, or still starting (Next.js takes ~20s)"
  echo "  - NODE_AUTH_TOKEN unset: export NODE_AUTH_TOKEN=\$(gh auth token)"
  echo "  - Docker not running (infra and backend tests both need it)"
  echo "  - /readyz failing on openfga: check 'docker compose -f docker-compose.dev.yml logs openfga'"
fi
exit "$fail"
