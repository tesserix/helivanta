#!/usr/bin/env bash
# Orchestrates the full e2e suite across its three Playwright phases.
#
# The web tier is this script's own (#976, design spec
# docs/superpowers/specs/2026-10-12-e2e-against-production-builds-design.md):
# the shell is built for production three times (scripts/e2e-web.sh) — once
# against the main API, once against the short-IDLE_TIMEOUT API that
# idle-timeout.spec.ts (#848) needs, once against the short-SESSION_TTL API
# session-renewal.spec.ts (#916) needs — and served behind the e2e ingress
# (dev/e2e/Caddyfile), which routes like production. MediCore, Pharmacy and
# Lab are routes of that one app since #933, not separate apps.
#
# Phases: specs+bulk against the main stack, then each fixture against its
# own shell build and API.
#
# Cleanup is unconditional (EXIT trap): a failure partway through must
# still stop everything this script started, free the fixtures' ports, and
# restore a developer's `make dev-web` if one was running — not leave the
# developer with no shell (or a stray server blocking the next invocation)
# instead of a clear error.
#
# Assumes infra, the API and `make seed` are already up.

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=lib/repo-owns.sh
# shellcheck source=lib/repo-owns.sh
. "$REPO_ROOT/scripts/lib/repo-owns.sh"

SHELL_PORT=4301
# The port a developer's `make dev-web` serves on: one `next dev` for
# apps/shell, which owns every route since #933. This script stops it for
# the run and restores it afterwards (restore_web).
ZONE_PORTS=(4301)
API_PORT=${HELIVANTA_API_PORT:-8080}
GLOBAL_API_PORT=${HELIVANTA_GLOBAL_API_PORT:-8081}
IDLE_GLOBAL_API_PORT=${HELIVANTA_IDLE_GLOBAL_API_PORT:-8199}
RENEWAL_GLOBAL_API_PORT=${HELIVANTA_RENEWAL_GLOBAL_API_PORT:-8198}
main_global_started=0
IDLE_API_PORT=${HELIVANTA_IDLE_API_PORT:-8099}
IDLE_WEB_PORT=${HELIVANTA_IDLE_WEB_PORT:-4399}
RENEWAL_API_PORT=${HELIVANTA_RENEWAL_API_PORT:-8098}
RENEWAL_WEB_PORT=${HELIVANTA_RENEWAL_WEB_PORT:-4398}
# The app host every URL below is checked on — see the Makefile's
# HELIVANTA_WEB_HOST comment for why this is helivanta.localhost and not
# localhost (#916 Task 4).
WEB_HOST=${HELIVANTA_WEB_HOST:-helivanta.localhost}

# This script ACTS on port ownership — it stops a developer's web app to
# free :4301 for the run. A missing platform port tool makes every port look free, so
# refuse up front rather than "successfully" stopping nothing (#920).
require_port_tool

LOG_DIR=$(mktemp -d "${TMPDIR:-/tmp}/hms-e2e.XXXXXX")

# require_port_up / wait_for_port both curl WITHOUT -f: a 404 or a 3xx (an
# unauthenticated request redirected to /login) still proves the server
# itself answered. Only "curl could
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
  local port=$1 label=$2 pid pids rc found=0
  # errexit-safe capture: status 3 must be handled here, not abort the run.
  pids=$(port_holders "$port") && rc=0 || rc=$?
  if [ "$rc" = 3 ]; then
    # Another user's listener (#927). It is not ours to stop, and an empty
    # pid list must not be read as "nothing to stop".
    echo "Refusing to stop $label (:$port) — it is held by a process this user cannot identify (another user's)." >&2
    exit 1
  fi
  [ "$rc" = 0 ] || exit 1
  for pid in $pids; do
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
  while ! port_free "$port"; do
    waited=$((waited + 1))
    if [ "$waited" -ge 10 ]; then
      local pid
      for pid in $(port_holders "$port"); do
        echo "$label (:$port) would not stop — pid $pid still listening." >&2
      done
      echo "$label (:$port) is still not free after ${waited}s." >&2
      exit 1
    fi
    sleep 1
  done
}

# wait_for_port URL LABEL [PID] — polls instead of sleeping a fixed amount.
#
# WALL CLOCK, not an iteration count. The loop used to run N iterations of
# `curl --max-time 2` + `sleep 1`, so "60s" actually meant up to 180s — and
# against a server that ACCEPTS and then stalls (a `next dev` paying for its
# first-request compile, which is the case this function exists for) each
# iteration costs the full curl timeout. At 180 iterations that would have
# been ~9 minutes per call, six calls per run, against a 30-minute job
# budget. Now the number in the message is the number that elapses.
#
# 180s rather than the original 60: a 2-core CI runner also hosting Postgres,
# NATS, Redis, OpenFGA, Zitadel and a Go API takes longer to boot `next dev`
# than a laptop does. It is a CEILING, not a wait — the loop exits the moment
# the URL answers, so a fast machine pays nothing for it.
#
# PID is optional and is what makes the ceiling rarely matter: when the
# process being waited on has already exited (Next.js refusing a second dev
# server for the same project directory, say — the #920 failure), there is
# nothing to wait for, so say so at once instead of burning the ceiling.
WAIT_FOR_PORT_TIMEOUT=${WAIT_FOR_PORT_TIMEOUT:-180}
wait_for_port() {
  local url=$1 label=$2 pid=${3:-} deadline=$((SECONDS + WAIT_FOR_PORT_TIMEOUT))
  printf 'Waiting for %s (%s)…' "$label" "$url"
  until curl -sS --max-time 2 "$url" >/dev/null 2>&1; do
    if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
      echo
      echo "$label exited before it ever answered at $url — see its log above." >&2
      return 1
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo
      echo "Timed out after ${WAIT_FOR_PORT_TIMEOUT}s waiting for $label at $url." >&2
      return 1
    fi
    printf '.'
    sleep 1
  done
  echo ' ready.'
}

# start_bg LABEL COMMAND... — run a make target in the background, logging to
# $LOG_DIR/LABEL.log, and print its pid. The pid is the point: the previous
# `( … & )` form discarded it, so a fixture that died on startup was
# indistinguishable from one still booting and cost the full wait ceiling.
start_bg() {
  local label=$1
  shift
  ( cd "$REPO_ROOT" && exec nohup "$@" >"$LOG_DIR/$label.log" 2>&1 ) &
  echo $!
}

web_restored=0
# dev_web_was_running — whether a developer's `make dev-web` held the main
# shell port when this script began. This script now runs PRODUCTION builds
# of the web apps on those ports (#976, design spec
# docs/superpowers/specs/2026-10-12-e2e-against-production-builds-design.md
# D4), so it stops that dev stack first and must put it back afterwards —
# but only if there was one: CI starts no dev web stack, and a run that left
# one behind would be a stack nobody asked for.
dev_web_was_running=0

# stop_ports_if_ours LABEL PORT... — stop every process this repo owns on any
# of PORTS, as ONE step: every port's holders are resolved and checked BEFORE
# anything is killed, then all are killed, then each port is waited free.
#
# Why not stop_port_if_ours in a loop: one kill can take down processes on
# other ports too (a turbo tree tears down its siblings), so by the time a
# loop reached the next port its socket could still be listed while its
# process was already gone — port_holders then reports a listener nobody can
# identify (rc 3) and refuses, correctly, to touch it. Observed (#976) when
# `make dev-web` still started four apps. Resolving every port first removes
# the race without weakening that refusal.
stop_ports_if_ours() {
  local label=$1 port pid pids rc all=()
  shift
  for port in "$@"; do
    pids=$(port_holders "$port") && rc=0 || rc=$?
    if [ "$rc" = 3 ]; then
      echo "Refusing to stop $label (:$port) — it is held by a process this user cannot identify (another user's)." >&2
      exit 1
    fi
    [ "$rc" = 0 ] || exit 1
    for pid in $pids; do
      if ! pid_is_ours "$pid"; then
        echo "Refusing to stop $label (:$port) — pid $pid's cwd is $(cwd_of "$pid"), not this repo." >&2
        exit 1
      fi
      all+=("$pid")
    done
  done
  [ "${#all[@]}" -gt 0 ] || return 0
  kill "${all[@]}" 2>/dev/null || true
  local waited=0
  for port in "$@"; do
    while ! port_free "$port"; do
      waited=$((waited + 1))
      if [ "$waited" -ge 20 ]; then
        echo "$label (:$port) is still not free after ${waited}s." >&2
        exit 1
      fi
      sleep 1
    done
  done
}

# The ingress (spec D5): a Caddy container (docker-compose.dev.yml's
# e2e-ingress, dev/e2e/Caddyfile) on the ports browsers and Zitadel's
# redirect URIs use — $SHELL_PORT, $IDLE_WEB_PORT, $RENEWAL_WEB_PORT —
# routing /api/v1 to the API and the rest to that stack's shell, the way
# production's Istio VirtualService does. The
# shells therefore listen on INTERNAL ports behind it.
COMPOSE=(docker compose -f "$REPO_ROOT/docker-compose.dev.yml" --profile e2e)
SHELL_INTERNAL_PORT=4311
IDLE_SHELL_INTERNAL_PORT=4409
RENEWAL_SHELL_INTERNAL_PORT=4408

# MAIN_WEB — the main stack's production build and the port it serves
# (scripts/e2e-web.sh): the shell built against the main API. A list, so a
# future second app is one entry, not a rewrite.
MAIN_WEB=("shell-main:$SHELL_INTERNAL_PORT")
MAIN_WEB_PORTS=("$SHELL_INTERNAL_PORT")

# stop_ingress — remove the ingress container. Idempotent. Done before any
# port check on $SHELL_PORT: Docker, not this repository, holds a published
# port, so stop_port_if_ours would (rightly) refuse to touch it.
stop_ingress() {
  "${COMPOSE[@]}" rm -sf e2e-ingress >/dev/null 2>&1 || return 1
}

# start_main_web — start every main-stack production server and wait for each
# to answer. "/" answers with a redirect to /login when unauthenticated, and
# any response means the server is up. `|| return 1` explicitly, for the reason restore_web's
# loop documents below (#920).
start_main_web() {
  local entry name port pid
  for entry in "${MAIN_WEB[@]}"; do
    name=${entry%%:*}
    port=${entry#*:}
    pid=$(start_bg "e2e-web-$name" scripts/e2e-web.sh start "$name" "$port")
    wait_for_port "http://$WEB_HOST:$port/" "$name (production build) on :$port" "$pid" || return 1
  done
}

# restore_web — idempotent; safe to call more than once (normal teardown
# calls it directly, the EXIT trap calls it again as a backstop). Removes the
# ingress, stops the production web servers this script started, then
# restarts the developer's `make dev-web` if, and only if, one was running
# when the script began.
restore_web() {
  [ "$web_restored" = 1 ] && return 0
  web_restored=1
  if [ "$main_global_started" = 1 ]; then
    stop_port_if_ours "$GLOBAL_API_PORT" "Global API" || return 1
    main_global_started=0
  fi
  stop_ingress || return 1
  stop_ports_if_ours "e2e web app" "${MAIN_WEB_PORTS[@]}"
  local port
  [ "$dev_web_was_running" = 1 ] || return 0
  echo "Restoring the developer's web app (make dev-web) ..."
  local web_pid
  web_pid=$(start_bg dev-web make dev-web)
  for port in "${ZONE_PORTS[@]}"; do
    # "/" answers with a redirect to /login when unauthenticated — any
    # response means the dev server is up.
    #
    # `|| return 1` EXPLICITLY, not left to errexit. cleanup() calls this
    # function as an operand of an `||` list, and POSIX says `-e` is ignored
    # for every command in an AND-OR list but the last — bash applies that
    # suppression inside the subshell and into every function it calls, so a
    # failing wait_for_port here would neither abort nor be noticed. Two
    # things went wrong because of that, both observed: the loop polled all
    # four ports instead of stopping at the first failure (4 x
    # WAIT_FOR_PORT_TIMEOUT = 12 minutes inside a 30-minute job), and this
    # function's status became merely the LAST port's, so ":4301 dead,
    # :4302-4304 alive" returned 0 and the run went green with apps/shell
    # never restored (#920).
    wait_for_port "http://$WEB_HOST:$port/" "web app on :$port" "$web_pid" || return 1
  done
}

teardown_fixture() {
  stop_port_if_ours "$IDLE_GLOBAL_API_PORT" "idle Global API"
  stop_port_if_ours "$RENEWAL_GLOBAL_API_PORT" "renewal Global API"
  stop_port_if_ours "$IDLE_SHELL_INTERNAL_PORT" "e2e-web-shell-idle-timeout"
  stop_port_if_ours "$IDLE_API_PORT" "dev-api-idle-timeout"
  stop_port_if_ours "$RENEWAL_SHELL_INTERNAL_PORT" "e2e-web-shell-renewal"
  stop_port_if_ours "$RENEWAL_API_PORT" "dev-api-renewal"
}

# dump_logs — prints whatever the backgrounded `make` invocations wrote.
# Those processes are started with nohup into $LOG_DIR, so on a failure their
# output is the ONLY account of why a fixture never bound its port — and
# until this existed the script deleted it unread, leaving nothing but
# "Timed out after 60s". That is exactly the case this suite is most likely
# to fail in on a machine nobody can attach to (CI, #920).
logs_dumped=0
dump_logs() {
  # Idempotent: cleanup dumps on a failed RUN, and its re-armed EXIT trap
  # dumps again if the cleanup path itself fails. One account, not two.
  if [ "$logs_dumped" = 1 ]; then
    return 0
  fi
  logs_dumped=1
  local f
  for f in "$LOG_DIR"/*.log; do
    [ -e "$f" ] || continue
    echo "----- $(basename "$f") -----" >&2
    # In full, not a tail. A `next dev` or `go run` that fails at BOOT puts
    # the cause in the first lines and then may print nothing else, or may
    # print pages of unrelated banner; a tail shows the consequence and hides
    # the cause. Same argument .github/workflows/ci.yml makes for dumping
    # Zitadel's logs whole.
    cat "$f" >&2
  done
}

cleanup() {
  local status=$?
  trap - EXIT
  if [ "$status" != 0 ]; then
    dump_logs
  fi
  # The cleanup path can fail on its own, and that failed SILENTLY in the
  # worst case: all 17 tests pass, so status is 0 and the dump above is
  # skipped; then restore_web's `make dev-web` will not come back up, the
  # script dies with $LOG_DIR populated and never read, and the run is red
  # with no account of why. Exactly what dump_logs exists for.
  #
  # Each half runs in its own SUBSHELL and its status is TESTED. Three
  # approaches were tried before this one and two of them do not work:
  #
  #   - Re-arming an EXIT trap here does nothing. Bash does not re-enter the
  #     EXIT trap from within the EXIT trap handler, so a `trap … EXIT` set
  #     inside this function never fires. Demonstrated on bash 5.2 (the CI
  #     runner) and 3.2 (macOS): the second handler's output never appears.
  #   - An ERR trap does not cover it either. ERR is not inherited by shell
  #     functions without `set -E`, and this path IS a function; and
  #     stop_port_if_ours calls `exit 1` outright, which no ERR trap sees.
  #   - A subshell contains an `exit`: stop_port_if_ours's outright `exit 1`
  #     becomes an ordinary non-zero status this function can test, instead
  #     of tearing down the whole script. That is what the subshells below
  #     are for, and it is ALL they are for.
  #
  # What the subshell does NOT do is convert a command that fails under
  # `set -e`. Errexit is ignored for every command in an AND-OR list except
  # the last, and bash pushes that suppression into the subshell and into the
  # functions it calls — so `( restore_web ) || cleanup_status=1` runs
  # restore_web with errexit effectively off. Propagation there has to be
  # explicit, and it is: restore_web's own loop ends in `|| return 1`. Any
  # future function called from here owes the same.
  #
  # Two subshells rather than one, so a failing teardown_fixture still leaves
  # restore_web to run: the developer gets their web app back even when the
  # fixture teardown could not finish.
  local cleanup_status=0
  ( teardown_fixture ) || cleanup_status=1
  ( restore_web ) || cleanup_status=1
  if [ "$cleanup_status" != 0 ]; then
    dump_logs
    if [ "$status" = 0 ]; then
      status=1
    fi
  fi
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

# A wedged zitadel-login (#923) is the other way this stack silently fails
# an e2e run: it reads its PAT once at boot and, if that read raced
# provisioning, latches an invalid token forever — nothing self-heals it,
# and the phase-1 spec path against helivanta-web never touches
# zitadel-login on the common path, so a wedge here previously surfaced as
# an inexplicable 66-minute hang (#923's own root-cause finding) rather than
# a clean, immediate failure. Catch it before Playwright ever starts.
bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health

require_port_up "$API_PORT" "http://localhost:$API_PORT/healthz" "main API"
if [ -z "$(port_holders "$GLOBAL_API_PORT")" ]; then
  main_global_pid=$(start_bg global-api make dev-global-api)
  main_global_started=1
  wait_for_port "http://localhost:$GLOBAL_API_PORT/healthz" "Global API" "$main_global_pid"
fi
require_port_up "$GLOBAL_API_PORT" "http://localhost:$GLOBAL_API_PORT/healthz" "Global API"

# The web tier is this script's own (spec D4/D5): production builds, never
# `next dev`, behind an ingress that routes like production (#976). A
# developer's dev web stack on the main ports is stopped for the run and
# restored by cleanup. A previous run's ingress is removed first, so it is
# neither mistaken for a dev stack nor left holding $SHELL_PORT.
stop_ingress
if [ -n "$(port_holders "$SHELL_PORT")" ]; then
  dev_web_was_running=1
fi
echo "Stopping any web apps on ${ZONE_PORTS[*]} $SHELL_INTERNAL_PORT for the run…"
stop_ports_if_ours "web app" "${ZONE_PORTS[@]}" "$SHELL_INTERNAL_PORT"
echo "Building production web apps…"
scripts/e2e-web.sh build
echo "Starting the main web stack from its production builds…"
start_main_web
echo "Starting the e2e ingress…"
"${COMPOSE[@]}" up -d e2e-ingress
wait_for_port "http://$WEB_HOST:$SHELL_PORT/login" "e2e ingress on :$SHELL_PORT"

echo "Phase 1/3 — specs + bulk against the main stack…"
pnpm --filter e2e exec playwright test --project=specs --project=bulk

echo "Phase 2/3 — idle-timeout against its own API + shell…"
# The main web stack keeps running: it was only ever stopped here because
# two `next dev` servers cannot share apps/shell. Production standalone
# builds share no directory (spec D4).

echo "Starting idle-timeout fixture…"
idle_api_pid=$(start_bg dev-api-idle-timeout make dev-api-idle-timeout)
idle_global_pid=$(start_bg idle-global-api make dev-global-api HELIVANTA_GLOBAL_API_PORT="$IDLE_GLOBAL_API_PORT" HELIVANTA_API_PORT="$IDLE_API_PORT")
wait_for_port "http://localhost:$IDLE_GLOBAL_API_PORT/healthz" "idle Global API" "$idle_global_pid"
idle_web_pid=$(start_bg e2e-web-shell-idle-timeout scripts/e2e-web.sh start shell-idle-timeout "$IDLE_SHELL_INTERNAL_PORT")
wait_for_port "http://localhost:$IDLE_API_PORT/healthz" "dev-api-idle-timeout" "$idle_api_pid"
# Through the ingress, the way the specs reach it.
wait_for_port "http://$WEB_HOST:$IDLE_WEB_PORT/login" "shell-idle-timeout (production build) via the ingress" "$idle_web_pid"

pnpm --filter e2e exec playwright test --project=idle-timeout

echo "Stopping idle-timeout fixture…"
teardown_fixture

# The session-renewal fixture binds its OWN ports (RENEWAL_*). Phases 2 and
# 3 still run one after the other, so each fixture's failure is reported on
# its own, but nothing now forces it: the one-dev-server-per-project rule
# that once did no longer applies to production builds (spec D4).
echo "Phase 3/3 — session-renewal against its own API + shell…"
echo "Starting session-renewal fixture…"
renewal_api_pid=$(start_bg dev-api-renewal make dev-api-renewal)
renewal_global_pid=$(start_bg renewal-global-api make dev-global-api HELIVANTA_GLOBAL_API_PORT="$RENEWAL_GLOBAL_API_PORT" HELIVANTA_API_PORT="$RENEWAL_API_PORT")
wait_for_port "http://localhost:$RENEWAL_GLOBAL_API_PORT/healthz" "renewal Global API" "$renewal_global_pid"
renewal_web_pid=$(start_bg e2e-web-shell-renewal scripts/e2e-web.sh start shell-renewal "$RENEWAL_SHELL_INTERNAL_PORT")
wait_for_port "http://localhost:$RENEWAL_API_PORT/healthz" "dev-api-renewal" "$renewal_api_pid"
wait_for_port "http://$WEB_HOST:$RENEWAL_WEB_PORT/login" "shell-renewal (production build) via the ingress" "$renewal_web_pid"

pnpm --filter e2e exec playwright test --project=renewal

echo "Stopping session-renewal fixture…"
teardown_fixture
restore_web

echo "make e2e: all three phases passed."
