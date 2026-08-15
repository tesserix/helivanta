#!/usr/bin/env bash
# Checks that this machine can actually run the stack, before `make up`
# touches Docker.
#
# Reports EVERY problem it finds, not the first. A fresh clone on a new
# laptop typically has more than one thing wrong — no NODE_AUTH_TOKEN, an
# old Node, something already on 8080 — and discovering them one failed
# boot at a time is precisely the experience issue #714 objects to.
#
# A port held by our own containers or our own processes is not a conflict:
# `make up` is documented as safe to re-run and seeding is idempotent, so a
# stack that is already up must pass. See scripts/lib/repo-owns.sh.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
. "$REPO_ROOT/scripts/lib/repo-owns.sh"

# Overridable so the test harness can simulate a fresh clone with no
# node_modules yet, without touching the repo's real one.
NODE_MODULES_DIR=${NODE_MODULES_DIR:-$REPO_ROOT/node_modules}

# Built from the HMS_* host-port variables (see .env.example) so an
# overridden port is actually checked, instead of preflight passing a stale
# port while compose starts on a different one. Each falls back to today's
# default when unset, so a bare `bash scripts/preflight.sh` with no .env and
# no Make involved still checks the stock ports. The four zone-app ports are
# out of scope for this variable set (see .env.example) and stay literal.
PREFLIGHT_PORTS=${PREFLIGHT_PORTS:-"${HMS_PG_PORT:-5432} ${HMS_NATS_PORT:-4222} ${HMS_NATS_MONITOR_PORT:-8222} ${HMS_REDIS_PORT:-6379} ${HMS_OPENFGA_PORT:-8090} ${HMS_ZITADEL_PORT:-20080} ${HMS_ZITADEL_PG_PORT:-5433} ${HMS_API_PORT:-8080} 4301 4302 4303 4304"}
GO_MIN=1.26
NODE_MIN=22

# Zitadel does not fail fast on a wrong-length masterkey — it crash-loops
# on every restart instead (docker-compose.dev.yml's zitadel service
# comment has the full story, reproduced live while wiring this stack up).
# Checked here, before Docker is touched, so the one length that matters
# is caught in one place rather than rediscovered per developer via a log
# grep. HMS_DEV_ZITADEL_MASTERKEY mirrors the Makefile's own default so a
# bare `bash scripts/preflight.sh` with no Make involved still checks it.
ZITADEL_MASTERKEY_CHECK=${HMS_DEV_ZITADEL_MASTERKEY:-HmsDevZitadelMasterKey32BytesXXX}

# Newline-delimited rather than an array: macOS ships bash 3.2, where
# expanding an empty array under `set -u` is an error.
FAILURES=""

ok()   { printf '  ok    %s\n' "$1"; }
warn() { printf '  warn  %s\n' "$1"; }
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
    fail "go $GO_MIN+" "Go $GO_MIN+ required (backend/go.mod: 1.26.5), not found — https://go.dev/dl/"
  elif version_at_least "$have" "$GO_MIN"; then
    ok "go $have"
  else
    fail "go $GO_MIN+" "Go $GO_MIN+ required (backend/go.mod: 1.26.5), found $have"
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
      "HMS_DEV_ZITADEL_MASTERKEY is $len bytes, want exactly 32 — Zitadel does not fail fast on this, it crash-loops on every restart instead (docker-compose.dev.yml's zitadel service comment has the full story)"
  fi
}

# `up`, `dev-infra` and `reset` never run `pnpm install` — the token is only
# needed to *install* @tesserix/web from GitHub Packages. So a missing token
# is only fatal when node_modules doesn't exist yet and an install is
# actually coming; otherwise it's a warning, since every developer's gh
# token rotates sooner or later and that shouldn't block a stack that
# doesn't need to install anything.
check_node_auth_token() {
  if [ -n "${NODE_AUTH_TOKEN:-}" ]; then
    ok "NODE_AUTH_TOKEN"
  elif [ ! -d "$NODE_MODULES_DIR" ]; then
    fail "NODE_AUTH_TOKEN" \
      "NODE_AUTH_TOKEN unset and node_modules is missing — pnpm install needs it for @tesserix/web from GitHub Packages — export NODE_AUTH_TOKEN=\$(gh auth token)"
  else
    warn "NODE_AUTH_TOKEN unset — fine unless you need to (re)run pnpm install — export NODE_AUTH_TOKEN=\$(gh auth token) first if you do"
  fi
}

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
        "port $port held by pid ${holder:-unknown} ($(ps -p "${holder:-0}" -o comm= 2>/dev/null || echo unknown)) — stop it, or 'make down' if it is a stale HMS process"
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
  check_node_auth_token
  check_zitadel_masterkey
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

# Guarded so the test harness can source this file and call one check at a
# time without running the whole suite.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
  exit $?
fi
