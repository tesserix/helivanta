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
cwd_of() {
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
port_holders() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null
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
