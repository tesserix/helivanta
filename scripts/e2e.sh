#!/usr/bin/env bash
# Orchestrates the full e2e suite across its two mutually exclusive fixtures.
#
# idle-timeout.spec.ts (#848 Task 8) needs apps/shell's dev server pointed
# at a short-IDLE_TIMEOUT API, and Next.js 16 refuses a second `next dev`
# for the same project directory — so that fixture cannot run alongside
# the main stack's own shell (see e2e/playwright.config.ts and the
# Makefile's "Idle timeout e2e fixture" comment). This script runs the
# suite in two phases instead: specs+bulk against the main stack, then a
# swap (stop the zone apps -> start the fixture -> idle-timeout project ->
# stop the fixture -> restart the zone apps).
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
# `next dev` can take anywhere from 10-60s to come up.
wait_for_port() {
  local url=$1 label=$2 waited=0
  printf 'Waiting for %s (%s)…' "$label" "$url"
  until curl -sS --max-time 2 "$url" >/dev/null 2>&1; do
    waited=$((waited + 1))
    if [ "$waited" -ge 60 ]; then
      echo
      echo "Timed out after 60s waiting for $label at $url." >&2
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
    wait_for_port "http://localhost:$port/" "zone app on :$port"
  done
}

teardown_fixture() {
  stop_port_if_ours "$IDLE_WEB_PORT" "dev-web-idle-timeout"
  stop_port_if_ours "$IDLE_API_PORT" "dev-api-idle-timeout"
}

cleanup() {
  local status=$?
  trap - EXIT
  teardown_fixture
  restore_web
  rm -rf "$LOG_DIR"
  exit $status
}
trap cleanup EXIT INT TERM

require_port_up "$SHELL_PORT" "http://localhost:$SHELL_PORT/login" "main shell"
require_port_up "$API_PORT" "http://localhost:$API_PORT/healthz" "main API"

echo "Phase 1/2 — specs + bulk against the main stack…"
pnpm --filter e2e exec playwright test --project=specs --project=bulk

echo "Phase 2/2 — idle-timeout against its own API + shell…"
echo "Stopping main zone apps so the fixture can bind :$SHELL_PORT ..."
for port in "${ZONE_PORTS[@]}"; do
  stop_port_if_ours "$port" "zone app on :$port"
done

echo "Starting idle-timeout fixture…"
(cd "$REPO_ROOT" && nohup make dev-api-idle-timeout >"$LOG_DIR/dev-api-idle-timeout.log" 2>&1 &)
(cd "$REPO_ROOT" && nohup make dev-web-idle-timeout >"$LOG_DIR/dev-web-idle-timeout.log" 2>&1 &)
wait_for_port "http://localhost:$IDLE_API_PORT/healthz" "dev-api-idle-timeout"
wait_for_port "http://localhost:$IDLE_WEB_PORT/login" "dev-web-idle-timeout"

pnpm --filter e2e exec playwright test --project=idle-timeout

echo "Stopping idle-timeout fixture…"
teardown_fixture
restore_web

echo "make e2e: both phases passed."
