#!/usr/bin/env bash
# Shared ownership test: does a process or a port belong to THIS repo's stack?
#
# Two callers need the same answer for opposite reasons. dev-down.sh must
# never kill a process it does not own — port 8080 in particular is popular
# and a developer may well have something unrelated on it. preflight.sh must
# not *fail* on a port our own stack already holds, because `make up` is
# documented as safe to re-run and seeding is idempotent.
#
# Getting those two out of step would be the worst kind of bug: a teardown
# that kills a stranger's server, or a preflight that refuses to start a
# stack that is already healthy. One definition, sourced by both.

REPO_ROOT=${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)}
COMPOSE_FILE=${COMPOSE_FILE:-$REPO_ROOT/docker-compose.dev.yml}

# cwd_of PID — the process's working directory, empty if it cannot be read.
#
# /proc first, where it exists. It is the authoritative answer on Linux, it
# costs a readlink instead of a process scan, and — the reason it is FIRST
# rather than a fallback — it does not depend on lsof being able to parse the
# process at all. See port_holders below for why that matters.
cwd_of() {
  if [ -r "/proc/$1/cwd" ]; then
    readlink "/proc/$1/cwd" 2>/dev/null
    return
  fi
  lsof -a -p "$1" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1
}

# pid_is_ours PID — true when the process runs from inside this repository.
pid_is_ours() {
  local cwd
  cwd=$(cwd_of "$1")
  case "$cwd" in
    "$REPO_ROOT" | "$REPO_ROOT"/*) return 0 ;;
    *) return 1 ;;
  esac
}

# port_holders PORT — PIDs listening on the port, one per line.
#
# `ss` first, lsof only where there is no `ss` (macOS). This is not a
# preference, it is a correctness fix: lsof 4.95.0 — the version on Ubuntu
# 24.04 and on GitHub's runner image — SILENTLY OMITS a process whose
# /proc comm contains spaces and parentheses, which is exactly how Next.js
# names its dev server: `next-server (v16.2.12)`. Measured on the runner
# with four of them listening on 4301-4304 plus the Go API on 8080:
# `lsof -nP -iTCP -sTCP:LISTEN` listed ONLY the Go API, while `ss -ltnp`
# listed all five with their pids.
#
# An empty answer here is read by every caller as "nobody holds this port",
# so that omission is a fail-OPEN in an ownership control: scripts/e2e.sh's
# fixture swap stopped nothing at all and then died on Next's "Another next
# dev server is already running", and dev-down.sh would likewise leave the
# zone apps running while reporting success (#920).
#
# macOS has no `ss` and its lsof does not have this bug, so the fallback
# there is the correct tool, not a degraded one.
port_holders() {
  if command -v ss >/dev/null 2>&1; then
    # -H drops the header; the sport filter is ss's own, so no port-number
    # substring can match by accident (":4301" vs ":14301").
    ss -H -ltnp "sport = :$1" 2>/dev/null \
      | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u
  else
    lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null
  fi
}

# compose_owns_port PORT — true when our compose project publishes the port.
#
# A container's listener belongs to the Docker daemon, not to a process whose
# cwd is in the repo, so pid_is_ours can never recognise it. Ask compose
# instead. If docker is unreachable the answer is a truthful "no": whatever
# holds that port, it is not a container of ours that is currently running.
compose_owns_port() {
  docker compose -f "$COMPOSE_FILE" ps --format json 2>/dev/null \
    | grep -q "\"PublishedPort\":$1[,}]"
}

# port_is_ours PORT — true when free, or held only by our processes or our
# containers.
port_is_ours() {
  local pid
  for pid in $(port_holders "$1"); do
    pid_is_ours "$pid" && continue
    compose_owns_port "$1" && continue
    return 1
  done
  return 0
}
