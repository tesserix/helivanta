#!/usr/bin/env bash
# Verifies that the whole Helivanta stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

# Same HELIVANTA_* host-port variables as the Makefile/preflight (see
# .env.example) — falls back to the stock ports when unset, so this still
# works run standalone with no .env present.
OPENFGA_PORT=${HELIVANTA_OPENFGA_PORT:-8090}
ZITADEL_PORT=${HELIVANTA_ZITADEL_PORT:-20080}
# Zitadel resolves its INSTANCE from the Host header, so it must be reached on
# the same host its issuer is stamped with — see the Makefile's
# HELIVANTA_ZITADEL_HOST comment and the README's "Hosts" section (#916 Task 4).
ZITADEL_HOST=${HELIVANTA_ZITADEL_HOST:-auth.tesserix.localhost}
API_PORT=${HELIVANTA_API_PORT:-8080}
# The host the apps are served on. Read from the environment for the SAME
# reason ZITADEL_HOST above is (#916 Task 4): these two must be different
# registrable domains, and a developer who overrides one has to be able to
# override the other. Every app URL below is built from it — an earlier
# version read the variable here and then wrote `helivanta.localhost` into
# four literals anyway, which made the comment at the zone checks argue
# against what the lines under it actually did, and made an override print
# a sign-in URL the developer's stack does not serve.
WEB_HOST=${HELIVANTA_WEB_HOST:-helivanta.localhost}

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

# Zitadel writes its instance domain ONCE, at first-instance provisioning,
# and answers "Instance not found" to every other host afterwards — the
# stale-volume failure #916 Task 4 introduced by moving the IdP off
# `localhost`. scripts/preflight.sh has the only legible diagnosis of it,
# and `make verify-local` did not run preflight at all: the zitadel check
# below probes /debug/healthz, which is instance-INDEPENDENT and answers 200
# on a stale volume, so this script printed `ok zitadel` and then failed
# opaquely in the authenticated round trip at the bottom. Running the one
# relevant check here, before the misleading `ok`, is the whole fix.
#
# Only that check, not the whole preflight: this script's precondition is a
# stack that is already UP, so preflight's port checks would be reporting on
# ports our own stack legitimately holds, and its toolchain checks were
# satisfied before `make dev` ever ran.
echo "Preflight (stale-instance guard only — see comment above):"
bash "$(dirname "${BASH_SOURCE[0]}")/preflight.sh" --only zitadel-instance-domain || fail=1

# The sibling wedge (#923): zitadel-login reads its PAT once at boot and can
# latch an invalid one, then sit there reporting `unhealthy` forever with
# nothing reading the signal. Cheap to check alongside the instance-domain
# guard above, for the same reason: this script's whole job is catching a
# stack that LOOKS up but is not actually usable.
bash "$(dirname "${BASH_SOURCE[0]}")/preflight.sh" --only zitadel-login-health || fail=1

echo "Infrastructure:"
docker compose -f docker-compose.dev.yml ps --status running --format '  ok    {{.Service}}' || fail=1
check "openfga"          "http://localhost:$OPENFGA_PORT/healthz"
# /debug/healthz, not the compose healthcheck's own /app/zitadel ready — the
# latter is documented as unreliable on v4.15.3 (docker-compose.dev.yml's
# zitadel service comment): it reports "not ready" even while serving real
# traffic, so trusting it here would produce the exact false FAIL this
# script's whole retry-with-attempts design exists to avoid.
check "zitadel"           "http://$ZITADEL_HOST:$ZITADEL_PORT/debug/healthz"

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
# $WEB_HOST (helivanta.localhost by default), not localhost (#916 Task 4,
# design spec D6): the shell's OIDC redirect URIs are registered against that
# host, so this is the origin a developer must actually use — checking
# `localhost` here would pass (Next binds every interface) while a human
# following the printed URL below could not complete a sign-in. The three zone
# apps have no OIDC registration of their own, but use the same host so one
# origin is quoted throughout.
check "shell    (4301)"  "http://$WEB_HOST:4301/login"    10 20
check "medicore (4302)"  "http://$WEB_HOST:4302/medicore" 10 20
check "pharmacy (4303)"  "http://$WEB_HOST:4303/pharmacy" 10 20
check "lab      (4304)"  "http://$WEB_HOST:4304/lab"      10 20

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
# (/login -> Zitadel -> /api/auth/callback) and session renewal (POST
# /v1/auth/renew, #916 — no longer a browser-side silent-renew flow at all).
# Those are the Playwright suite's job — `make e2e`, which runs all three
# phases; `pnpm --filter e2e exec playwright test` runs only the first.
echo "Authenticated round trip (API-level — see script comment for what this does and does not cover):"
node scripts/zitadel-verify-login.mjs || fail=1

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed."
  # The browser still transits Zitadel's /oauth/v2/authorize, but only as a
  # 302 — since #854 the credential form that actually renders is Helivanta's own
  # /login. Saying "you will be redirected to Zitadel" here read as though a
  # hosted Zitadel page were expected, which would now be a defect.
  echo "Sign in at http://$WEB_HOST:4301 — the sign-in form is Helivanta's own /login."
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
