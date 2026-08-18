#!/usr/bin/env bash
# Verifies that the whole Helivanta stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

# Same HELIVANTA_* host-port variables as the Makefile/preflight (see
# .env.example) — falls back to the stock ports when unset, so this still
# works run standalone with no .env present.
OPENFGA_PORT=${HELIVANTA_OPENFGA_PORT:-8090}
ZITADEL_PORT=${HELIVANTA_ZITADEL_PORT:-20080}
API_PORT=${HELIVANTA_API_PORT:-8080}

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
# It goes straight against the API rather than through the shell: the
# browser path is exercised end to end by the Playwright suite, and
# duplicating it here would make this script depend on four Next dev
# servers being warm to answer "is auth wired up". This verifies: a real
# Zitadel token (via a genuine authorization-code-plus-PKCE exchange
# against Zitadel's own v2 APIs, not a shortcut — see
# scripts/zitadel-verify-login.mjs's own comment on why driving the token
# exchange itself, not just generating a callback_url, is the entire point)
# → POST /v1/auth/login → a Helivanta session that resolves a real permission on
# /v1/iam/me/permissions. Each hop is still reported separately by
# scripts/zitadel-verify-login.mjs, because "the stack is broken" is not
# actionable but "the token exchange failed" is — #854 Task 7 found a real
# defect (Zitadel's oidc_config PUT silently resetting authMethodType) that
# only broke at exactly that hop, invisible to every check that stopped
# earlier.
#
# Deliberately NOT re-verified here: the browser redirect flow
# (/login -> Zitadel -> /api/auth/callback) and silent renewal. Those are
# the Playwright suite's job — `pnpm --filter e2e exec playwright test`.
echo "Authenticated round trip (API-level — see script comment for what this does and does not cover):"
node scripts/zitadel-verify-login.mjs || fail=1

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed."
  # The browser still transits Zitadel's /oauth/v2/authorize, but only as a
  # 302 — since #854 the credential form that actually renders is Helivanta's own
  # /login. Saying "you will be redirected to Zitadel" here read as though a
  # hosted Zitadel page were expected, which would now be a defect.
  echo "Sign in at http://localhost:4301 — the sign-in form is Helivanta's own /login."
  echo "Zitadel accounts, login-verified by 'make seed':"
  echo "  test@helivanta.dev       / HmsDev123!  (tenant_admin)"
  echo "  pharmacist@helivanta.dev / HmsDev123!  (pharmacist)"
else
  echo "Some checks failed. Common causes:"
  echo "  - 'make dev' not running, or still starting (Next.js takes ~20s)"
  echo "  - dependencies not installed: run 'pnpm install'"
  echo "  - Docker not running (infra and backend tests both need it)"
  echo "  - /readyz failing on openfga: check 'docker compose -f docker-compose.dev.yml logs openfga'"
fi
exit "$fail"
