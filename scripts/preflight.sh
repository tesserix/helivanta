#!/usr/bin/env bash
# Checks that this machine can actually run the stack, before `make up`
# touches Docker.
#
# Reports EVERY problem it finds, not the first. A fresh clone on a new
# laptop typically has more than one thing wrong — an old Node, no pnpm,
# something already on 8080 — and discovering them one failed boot at a
# time is precisely the experience issue #714 objects to.
#
# A port held by our own containers or our own processes is not a conflict:
# `make up` is documented as safe to re-run and seeding is idempotent, so a
# stack that is already up must pass. See scripts/lib/repo-owns.sh.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
. "$REPO_ROOT/scripts/lib/repo-owns.sh"

# Built from the HELIVANTA_* host-port variables (see .env.example) so an
# overridden port is actually checked, instead of preflight passing a stale
# port while compose starts on a different one. Each falls back to today's
# default when unset, so a bare `bash scripts/preflight.sh` with no .env and
# no Make involved still checks the stock ports. The four zone-app ports are
# out of scope for this variable set (see .env.example) and stay literal.
PREFLIGHT_PORTS=${PREFLIGHT_PORTS:-"${HELIVANTA_PG_PORT:-5432} ${HELIVANTA_NATS_PORT:-4222} ${HELIVANTA_NATS_MONITOR_PORT:-8222} ${HELIVANTA_REDIS_PORT:-6379} ${HELIVANTA_OPENFGA_PORT:-8090} ${HELIVANTA_ZITADEL_PORT:-20080} ${HELIVANTA_ZITADEL_PG_PORT:-5433} ${HELIVANTA_API_PORT:-8080} 4301 4302 4303 4304"}
GO_MIN=1.26
NODE_MIN=22

# Read from backend/go.mod rather than hardcoding: the patch version moved
# 1.26.5 -> 1.26.6 to clear HIGH stdlib CVEs the image scan caught, and the
# hardcoded copy here silently became a lie. GO_MIN stays major.minor, which
# is what version_at_least actually gates on.
GO_MOD_VERSION=$(sed -n 's/^go \([0-9][0-9.]*\).*/\1/p' "$REPO_ROOT/backend/go.mod" 2>/dev/null)
GO_MOD_VERSION=${GO_MOD_VERSION:-$GO_MIN}

# Zitadel does not fail fast on a wrong-length masterkey — it crash-loops
# on every restart instead (docker-compose.dev.yml's zitadel service
# comment has the full story, reproduced live while wiring this stack up).
# Checked here, before Docker is touched, so the one length that matters
# is caught in one place rather than rediscovered per developer via a log
# grep. HELIVANTA_DEV_ZITADEL_MASTERKEY mirrors the Makefile's own default so a
# bare `bash scripts/preflight.sh` with no Make involved still checks it.
ZITADEL_MASTERKEY_CHECK=${HELIVANTA_DEV_ZITADEL_MASTERKEY:-HmsDevZitadelMasterKey32BytesXXX}
# The host Zitadel is expected to answer on. Must match the Makefile's
# HELIVANTA_ZITADEL_HOST and docker-compose.dev.yml's ZITADEL_EXTERNALDOMAIN —
# see check_zitadel_instance_domain below for what goes wrong when it does
# not, and why that failure is otherwise unreadable.
ZITADEL_HOST_CHECK=${HELIVANTA_ZITADEL_HOST:-auth.tesserix.localhost}
ZITADEL_PORT_CHECK=${HELIVANTA_ZITADEL_PORT:-20080}

# Newline-delimited rather than an array: macOS ships bash 3.2, where
# expanding an empty array under `set -u` is an error.
FAILURES=""

ok()   { printf '  ok    %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; FAILURES="${FAILURES}  - $2
"; }

# version_at_least HAVE WANT — true when HAVE >= WANT.
version_at_least() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]
}

check_docker() {
  if docker info >/dev/null 2>&1; then
    ok "docker daemon"
  else
    fail "docker daemon" "Docker is not running — start Docker Desktop (or 'colima start')"
  fi
}

check_compose() {
  if docker compose version >/dev/null 2>&1; then
    ok "docker compose v2"
  else
    fail "docker compose v2" "Compose v2 required — 'docker compose version' failed"
  fi
}

check_go() {
  local have
  have=$(go version 2>/dev/null | sed -n 's/.*go\([0-9][0-9.]*\).*/\1/p')
  if [ -z "$have" ]; then
    fail "go $GO_MIN+" "Go $GO_MIN+ required (backend/go.mod: $GO_MOD_VERSION), not found — https://go.dev/dl/"
  elif version_at_least "$have" "$GO_MIN"; then
    ok "go $have"
  else
    fail "go $GO_MIN+" "Go $GO_MIN+ required (backend/go.mod: $GO_MOD_VERSION), found $have"
  fi
}

check_node() {
  local have
  have=$(node --version 2>/dev/null | sed 's/^v//')
  if [ -z "$have" ]; then
    fail "node $NODE_MIN+" "Node $NODE_MIN+ required (package.json engines), not found"
  elif version_at_least "$have" "$NODE_MIN"; then
    ok "node $have"
  else
    fail "node $NODE_MIN+" "Node $NODE_MIN+ required (package.json engines), found $have"
  fi
}

check_pnpm() {
  if pnpm --version >/dev/null 2>&1; then
    ok "pnpm $(pnpm --version)"
  else
    fail "pnpm" "pnpm missing — run 'corepack enable'"
  fi
}

check_zitadel_masterkey() {
  local len
  len=$(printf '%s' "$ZITADEL_MASTERKEY_CHECK" | wc -c | tr -d ' ')
  if [ "$len" = 32 ]; then
    ok "zitadel masterkey (32 bytes)"
  else
    fail "zitadel masterkey" \
      "HELIVANTA_DEV_ZITADEL_MASTERKEY is $len bytes, want exactly 32 — Zitadel does not fail fast on this, it crash-loops on every restart instead (docker-compose.dev.yml's zitadel service comment has the full story)"
  fi
}

# Zitadel resolves its INSTANCE from the Host header, and writes its instance
# domain exactly once — at first-instance provisioning. Change
# HELIVANTA_ZITADEL_HOST (or docker-compose.dev.yml's ZITADEL_EXTERNALDOMAIN)
# on a stack whose volume already exists and the container comes up perfectly
# healthy while every request on the NEW host is answered "Instance not
# found". That is not hypothetical: it is what every developer who already had
# a stack hit exactly once when #916 Task 4 moved the IdP off `localhost`.
#
# It is also invisible where it happens. `make dev-infra` polls
# /debug/healthz, which is instance-INDEPENDENT and answers 200 regardless, so
# the wait succeeds; then scripts/zitadel-bootstrap.mjs dies inside
# scripts/lib/zitadel.mjs's managementAPI with a raw
# `HTTP 404 {"code":5,"message":"Instance not found"}` — no mention of
# hostnames, of ZITADEL_EXTERNALDOMAIN, or of the one thing that fixes it
# (`make reset`, which drops the volume).
#
# So this is the sibling of check_zitadel_masterkey above, and exists for the
# identical reason: Zitadel does not fail legibly on this class of
# misconfiguration, and this repo orders enforcement as boot failure >
# documented convention. The README documents the constraint; this makes it a
# control.
#
# NOT a failure when Zitadel is simply not running yet — that is the normal
# state of a fresh clone before the first `make up`, and demanding a live
# Zitadel here would block the very command that starts it. Only a Zitadel
# that ANSWERS, and answers "Instance not found" for the configured host, is
# a failure: that is unambiguously a stale volume, never a cold start.
check_zitadel_instance_domain() {
  # scripts/reset-dev.sh sets this. Its whole job is to DROP the volume and
  # re-provision on the configured host, so a stale instance is this
  # command's expected input, not a reason to refuse it. Without the escape
  # hatch the check deadlocks the user: the failure message says "run
  # RESET_YES=1 make reset", and reset-dev.sh runs preflight before it
  # destroys anything, so that command would refuse for the very reason it
  # was being run. A control that blocks its own remedy is a worse defect
  # than the one it catches.
  if [ "${PREFLIGHT_SKIP_ZITADEL_INSTANCE:-}" = "1" ]; then
    ok "zitadel instance domain (skipped — this run re-provisions it)"
    return
  fi
  local base="http://${ZITADEL_HOST_CHECK}:${ZITADEL_PORT_CHECK}"
  if ! curl -fsS --max-time 3 "${base}/debug/healthz" >/dev/null 2>&1; then
    ok "zitadel instance domain (not running yet — will be provisioned on ${ZITADEL_HOST_CHECK})"
    return
  fi
  # An instance-SCOPED endpoint, unlike /debug/healthz. The OIDC discovery
  # document is served per instance and needs no credential, so it answers
  # 200 on a correctly-provisioned host and 404 "Instance not found" on a
  # host this instance was never provisioned for.
  local body
  body=$(curl -sS --max-time 3 "${base}/.well-known/openid-configuration" 2>/dev/null || true)
  case "$body" in
    *"Instance not found"*)
      fail "zitadel instance domain" \
        "Zitadel is running but does not recognise the host '${ZITADEL_HOST_CHECK}' — its instance domain was written once, at first-instance provisioning, and cannot be changed in place. This is what a stale volume looks like after HELIVANTA_ZITADEL_HOST or ZITADEL_EXTERNALDOMAIN changed (#916 Task 4 moved the IdP off 'localhost'). Fix: 'RESET_YES=1 make reset', which drops the volume and re-provisions on the current host. Without it, 'make dev-infra' will pass its healthz wait and then die in scripts/zitadel-bootstrap.mjs with a bare 'HTTP 404 Instance not found'"
      ;;
    *issuer*)
      ok "zitadel instance domain (${ZITADEL_HOST_CHECK})"
      ;;
    *)
      # Answering healthz but not a recognisable discovery document —
      # mid-provisioning, most likely. Not a hostname problem, and not
      # something to block a developer on.
      ok "zitadel instance domain (still provisioning on ${ZITADEL_HOST_CHECK})"
      ;;
  esac
}

# Nothing here checks a registry token. @tesserix/web moved to the PUBLIC npm
# registry (#866/#868) and the repo-local .npmrc that pinned the scope to
# GitHub Packages is gone, so `pnpm install` on a fresh clone needs no
# credentials at all. A check that demanded one would block onboarding for a
# token nobody needs — the opposite of what this script is for.

# port_is_ours (scripts/lib/repo-owns.sh) shells out to lsof, and reads an
# empty result — its normal behaviour when lsof is simply absent — as "no
# holder found, port is free". Without this check that makes every port
# check pass open: a foreign Postgres on 5432 would print "ok port 5432"
# right before compose dies with exactly the cryptic error #714 exists to
# eliminate. Must run before check_ports, and check_ports must not run its
# real logic if this failed.
check_lsof() {
  if command -v lsof >/dev/null 2>&1; then
    ok "lsof"
  else
    fail "lsof" \
      "lsof missing — port checks can't detect what holds a port without it — brew install lsof (macOS; present by default) or sudo apt-get install -y lsof (Debian/Ubuntu)"
  fi
}

check_ports() {
  # Without lsof, port_is_ours can't see any holder and would report every
  # port "ok" even when a foreign process has it — fail open. check_lsof
  # above already recorded the failure; skip the misleading "ok" lines here.
  command -v lsof >/dev/null 2>&1 || return 0

  local port holder
  for port in $PREFLIGHT_PORTS; do
    if port_is_ours "$port"; then
      ok "port $port"
    else
      holder=$(port_holders "$port" | head -1)
      fail "port $port" \
        "port $port held by pid ${holder:-unknown} ($(ps -p "${holder:-0}" -o comm= 2>/dev/null || echo unknown)) — stop it, or 'make down' if it is a stale Helivanta process"
    fi
  done
}

main() {
  echo "Preflight:"
  check_docker
  check_compose
  check_go
  check_node
  check_pnpm
  check_zitadel_masterkey
  check_zitadel_instance_domain
  check_lsof
  check_ports

  if [ -n "$FAILURES" ]; then
    echo
    echo "Cannot start — fix these first:"
    printf '%s' "$FAILURES"
    return 1
  fi
  echo
  echo "All prerequisites satisfied."
  return 0
}

# run_one CHECK — run a single named check and exit non-zero if it failed.
#
# Exists for `make dev-infra`, which needs check_zitadel_instance_domain at a
# moment `make up`'s preflight cannot cover it: preflight runs BEFORE
# `docker compose up`, so on the `make down` -> pull -> `make dev-infra`
# path Zitadel is not answering yet and the check correctly reports "not
# running yet — will be provisioned". The stale instance only becomes
# observable once the container is up, which is after the healthz wait and
# before scripts/zitadel-bootstrap.mjs — exactly where dev-infra calls this.
# Re-probing there costs one HTTP request and turns the bare
# `HTTP 404 {"code":5,"message":"Instance not found"}` into the message
# check_zitadel_instance_domain already writes.
run_one() {
  case "$1" in
    zitadel-instance-domain) check_zitadel_instance_domain ;;
    *) echo "preflight: unknown check '$1'" >&2; return 2 ;;
  esac
  if [ -n "$FAILURES" ]; then
    echo
    echo "Cannot continue — fix this first:"
    printf '%s' "$FAILURES"
    return 1
  fi
  return 0
}

# Guarded so the test harness can source this file and call one check at a
# time without running the whole suite.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  if [ "${1:-}" = "--only" ]; then
    run_one "${2:-}"
    exit $?
  fi
  main "$@"
  exit $?
fi
