#!/usr/bin/env bash
# Orchestrates the full e2e suite across its mutually exclusive fixtures.
#
# idle-timeout.spec.ts (#848 Task 8) needs apps/shell's dev server pointed
# at a short-IDLE_TIMEOUT API, and session-renewal.spec.ts (#916 Task 4)
# needs one pointed at a short-SESSION_TTL API. Next.js 16 refuses a second
# `next dev` for the same project directory, so neither fixture can run
# alongside the main stack's own shell, or alongside each other (see
# e2e/playwright.config.ts and the Makefile's two fixture comments). This
# script runs the suite in three phases instead: specs+bulk against the
# main stack, then one swap per fixture (stop the zone apps -> start the
# fixture -> run its project -> stop the fixture), restoring the zone apps
# at the end.
#
# Cleanup is unconditional (EXIT trap): a failure partway through must
# still restore the zone apps and free the fixture's ports, or the
# developer is left with no shell running (or a stray fixture blocking the
# next invocation) instead of a clear error.
#
# Assumes `make dev` (or `make up`) and `make seed` have already brought
# up infra + API + shell + medicore — the same assumption the "specs" and
# "bulk" Playwright projects already make.

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib/repo-owns.sh
. "$REPO_ROOT/scripts/lib/repo-owns.sh"

SHELL_PORT=4301
# `make dev-web` starts all four zone apps as one `pnpm turbo dev` process
# group (Makefile). Stopping just the shell's listener still tears down
# its turbo siblings (observed live: killing :4301 took :4302-4304 with
# it) — so the swap treats the whole zone-app layer as one unit to stop
# and restore, not apps/shell alone.
ZONE_PORTS=(4301 4302 4303 4304)
API_PORT=${HELIVANTA_API_PORT:-8080}
IDLE_API_PORT=${HELIVANTA_IDLE_API_PORT:-8099}
IDLE_WEB_PORT=${HELIVANTA_IDLE_WEB_PORT:-4399}
RENEWAL_API_PORT=${HELIVANTA_RENEWAL_API_PORT:-8098}
RENEWAL_WEB_PORT=${HELIVANTA_RENEWAL_WEB_PORT:-4398}
# The app host every URL below is checked on — see the Makefile's
# HELIVANTA_WEB_HOST comment for why this is helivanta.localhost and not
# localhost (#916 Task 4).
WEB_HOST=${HELIVANTA_WEB_HOST:-helivanta.localhost}

LOG_DIR=$(mktemp -d "${TMPDIR:-/tmp}/hms-e2e.XXXXXX")

# require_port_up / wait_for_port both curl WITHOUT -f: a 404 (medicore's
# "/" returns one — its routes live under /opd, /ipd, not at the root) or
# a 3xx (a zone app redirecting an unauthenticated request to apps/shell's
# /login) still proves the dev server itself answered. Only "curl could
# not connect at all" should read as not-ready.
#
# require_port_up PORT URL LABEL — fails fast with a clear message instead
# of the swap below silently stealing a port from a stack that isn't even
# running yet.
require_port_up() {
  local port=$1 url=$2 label=$3
  if ! curl -sS --max-time 2 "$url" >/dev/null 2>&1; then
    echo "$label (:$port) is not answering at $url." >&2
    echo "Run 'make dev' (or 'make up') and 'make seed' first." >&2
    exit 1
  fi
}

# stop_port_if_ours PORT LABEL — kills whatever holds PORT, but only if it
# belongs to this repo (same ownership test dev-down.sh uses). Refuses,
# rather than guessing, if a stranger holds the port.
stop_port_if_ours() {
  local port=$1 label=$2 pid found=0
  for pid in $(port_holders "$port"); do
    found=1
    if pid_is_ours "$pid"; then
      kill "$pid" 2>/dev/null || true
    else
      echo "Refusing to stop $label (:$port) — pid $pid's cwd is $(cwd_of "$pid"), not this repo." >&2
      exit 1
    fi
  done
  [ "$found" = 1 ] || return 0
  local waited=0
  while [ -n "$(port_holders "$port")" ]; do
    waited=$((waited + 1))
    if [ "$waited" -ge 10 ]; then
      local pid
      for pid in $(port_holders "$port"); do
        echo "$label (:$port) would not stop — pid $pid still listening." >&2
      done
      exit 1
    fi
    sleep 1
  done
}

# wait_for_port URL LABEL — polls instead of sleeping a fixed amount;
# `next dev` can take anywhere from 10-60s to come up on a laptop, and
# longer on a 2-core CI runner that is also hosting Postgres, NATS, Redis,
# OpenFGA, Zitadel and a Go API. The ceiling is 180s rather than the
# original 60 for that reason, and it is a CEILING, not a wait: the loop
# exits the moment the URL answers, so a fast machine pays nothing for it.
# The job-level timeout in .github/workflows/ci.yml is what bounds a real hang.
WAIT_FOR_PORT_TIMEOUT=${WAIT_FOR_PORT_TIMEOUT:-180}
wait_for_port() {
  local url=$1 label=$2 waited=0
  printf 'Waiting for %s (%s)…' "$label" "$url"
  until curl -sS --max-time 2 "$url" >/dev/null 2>&1; do
    waited=$((waited + 1))
    if [ "$waited" -ge "$WAIT_FOR_PORT_TIMEOUT" ]; then
      echo
      echo "Timed out after ${WAIT_FOR_PORT_TIMEOUT}s waiting for $label at $url." >&2
      return 1
    fi
    printf '.'
    sleep 1
  done
  echo ' ready.'
}

web_restored=0

# restore_web — idempotent; safe to call more than once (normal teardown
# calls it directly, the EXIT trap calls it again as a backstop). Skips
# the restart if the zone apps are already up (e.g. cleanup running twice,
# or nothing was ever stopped because phase 1 itself failed).
restore_web() {
  [ "$web_restored" = 1 ] && return 0
  web_restored=1
  if [ -n "$(port_holders "$SHELL_PORT")" ]; then
    return 0
  fi
  echo "Restoring main zone apps (make dev-web) ..."
  (cd "$REPO_ROOT" && nohup make dev-web >"$LOG_DIR/dev-web.log" 2>&1 &)
  local port
  for port in "${ZONE_PORTS[@]}"; do
    # "/" rather than "/login": only apps/shell serves a login page
    # directly, the other three zone apps redirect an unauthenticated
    # request there — either way a response means the dev server is up.
    wait_for_port "http://$WEB_HOST:$port/" "zone app on :$port"
  done
}

teardown_fixture() {
  stop_port_if_ours "$IDLE_WEB_PORT" "dev-web-idle-timeout"
  stop_port_if_ours "$IDLE_API_PORT" "dev-api-idle-timeout"
  stop_port_if_ours "$RENEWAL_WEB_PORT" "dev-web-renewal"
  stop_port_if_ours "$RENEWAL_API_PORT" "dev-api-renewal"
}

# dump_logs — prints whatever the backgrounded `make` invocations wrote.
# Those processes are started with nohup into $LOG_DIR, so on a failure their
# output is the ONLY account of why a fixture never bound its port — and
# until this existed the script deleted it unread, leaving nothing but
# "Timed out after 60s". That is exactly the case this suite is most likely
# to fail in on a machine nobody can attach to (CI, #920).
dump_logs() {
  local f
  for f in "$LOG_DIR"/*.log; do
    [ -e "$f" ] || continue
    echo "----- $(basename "$f") -----" >&2
    tail -50 "$f" >&2
  done
}

cleanup() {
  local status=$?
  trap - EXIT
  if [ "$status" != 0 ]; then
    dump_logs
  fi
  teardown_fixture
  restore_web
  rm -rf "$LOG_DIR"
  exit $status
}
trap cleanup EXIT INT TERM

# The stale-instance guard, before anything else. Zitadel writes its
# instance domain ONCE at first provisioning and answers "Instance not
# found" to any other host afterwards — the failure #916 Task 4 introduced
# by moving the IdP off `localhost`. preflight.sh has the only legible
# diagnosis of it, but it only ran on `make dev` / `make up`, and the
# developer most likely to hit this is precisely the one who pulls this
# branch and runs `make e2e` against a stack whose volume predates it:
# every login in phase 1 would fail against a Zitadel that answers healthz
# perfectly well, with nothing anywhere naming the hostname as the cause.
#
# Only this one check: the ports preflight would otherwise inspect are held
# by the very stack this script requires to be up.
bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-instance-domain

require_port_up "$SHELL_PORT" "http://$WEB_HOST:$SHELL_PORT/login" "main shell"
require_port_up "$API_PORT" "http://localhost:$API_PORT/healthz" "main API"

echo "Phase 1/3 — specs + bulk against the main stack…"
pnpm --filter e2e exec playwright test --project=specs --project=bulk

echo "Phase 2/3 — idle-timeout against its own API + shell…"
echo "Stopping main zone apps so the fixture can bind :$SHELL_PORT ..."
for port in "${ZONE_PORTS[@]}"; do
  stop_port_if_ours "$port" "zone app on :$port"
done

echo "Starting idle-timeout fixture…"
(cd "$REPO_ROOT" && nohup make dev-api-idle-timeout >"$LOG_DIR/dev-api-idle-timeout.log" 2>&1 &)
(cd "$REPO_ROOT" && nohup make dev-web-idle-timeout >"$LOG_DIR/dev-web-idle-timeout.log" 2>&1 &)
wait_for_port "http://localhost:$IDLE_API_PORT/healthz" "dev-api-idle-timeout"
wait_for_port "http://$WEB_HOST:$IDLE_WEB_PORT/login" "dev-web-idle-timeout"

pnpm --filter e2e exec playwright test --project=idle-timeout

echo "Stopping idle-timeout fixture…"
teardown_fixture

# Phase 3 reuses the same swapped-out state phase 2 left behind — the zone
# apps are still stopped, so nothing needs restoring in between. The
# session-renewal fixture binds its OWN ports (RENEWAL_*), so the only
# reason it cannot overlap phase 2 is Next.js's one-dev-server-per-project
# rule, which teardown_fixture above has just satisfied.
echo "Phase 3/3 — session-renewal against its own API + shell…"
echo "Starting session-renewal fixture…"
(cd "$REPO_ROOT" && nohup make dev-api-renewal >"$LOG_DIR/dev-api-renewal.log" 2>&1 &)
(cd "$REPO_ROOT" && nohup make dev-web-renewal >"$LOG_DIR/dev-web-renewal.log" 2>&1 &)
wait_for_port "http://localhost:$RENEWAL_API_PORT/healthz" "dev-api-renewal"
wait_for_port "http://$WEB_HOST:$RENEWAL_WEB_PORT/login" "dev-web-renewal"

pnpm --filter e2e exec playwright test --project=renewal

echo "Stopping session-renewal fixture…"
teardown_fixture
restore_web

echo "make e2e: all three phases passed."
