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

# port_tool — the ONE tool this platform is allowed to use, chosen by
# PLATFORM rather than by what happens to be installed.
#
# That distinction is the whole point. lsof 4.95.0 — the version on Ubuntu
# 24.04 and on GitHub's runner image — SILENTLY OMITS a process whose /proc
# comm contains spaces and parentheses, which is exactly how Next.js names
# its dev server: `next-server (v16.2.12)`. Measured on the runner with four
# of them listening on 4301-4304 plus the Go API on 8080,
# `lsof -nP -iTCP -sTCP:LISTEN` listed ONLY the Go API, while `ss -ltnp`
# listed all five with their pids. Reproduced independently in a bare
# ubuntu:24.04 container against a listener renamed to that exact string.
#
# An empty answer is read by every caller as "nobody holds this port", so
# that omission is a fail-OPEN in an ownership control: scripts/e2e.sh's
# fixture swap stopped nothing at all and then died on Next's "Another next
# dev server is already running", and dev-down.sh would likewise leave the
# zone apps running while reporting success (#920).
#
# Selecting on `command -v ss` instead would restore that bug the moment
# this ran on a Linux box without iproute2 (a slim container, a devcontainer,
# some self-hosted runners): the lsof branch would be chosen, preflight would
# report a cheerful "ok", and the control would be doing nothing again. On
# Linux the answer is `ss` or an error — never a quiet downgrade. macOS has
# no `ss`, and its lsof does not have the bug, so lsof is the correct tool
# there rather than a degraded one.
#
# Neither tool can always NAME a listener: run as a non-root user, `ss`
# prints a foreign process's socket row without its `users:((…pid=…))`
# field, and macOS lsof does not list another user's processes at all. That
# case is not "free" — port_holders reports it as exit status 3 (#927).
port_tool() {
  case "$(uname -s)" in
    Darwin) echo lsof ;;
    *) echo ss ;;
  esac
}

# have_port_tool — true when this platform's required tool is installed.
have_port_tool() {
  command -v "$(port_tool)" >/dev/null 2>&1
}

# require_port_tool — fail fast, for scripts that ACT on the answer
# (dev-down.sh kills processes, e2e.sh swaps fixtures). preflight.sh
# deliberately does not use this: it reports every problem it finds rather
# than exiting on the first.
require_port_tool() {
  have_port_tool && return 0
  echo "$(port_tool) is required on $(uname -s) to see which process holds a port, and it is not installed." >&2
  echo "Without it every port looks free, and this script would act on that. Run 'bash scripts/preflight.sh' for the fix." >&2
  exit 1
}

# port_holders PORT — PIDs listening on the port, one per line.
#
# stdout is only ever pids. What stdout cannot say is in the exit status,
# because an empty stdout means two different things and every caller used
# to read it as the harmless one:
#
#   0  every listener was attributed (no listener at all is also 0)
#   2  no answer: the platform's tool is missing or failed
#   3  at least one listener could NOT be attributed to a pid — it belongs
#      to another user (#927). Any pids that could be seen are still printed.
#
# 2 and 3 exist so that "I cannot tell" is never spelled the same as "the
# port is free": that is the exact fail-open shape described above, and a
# placeholder pid in stdout instead would reach `kill` and `ps -p` as a type
# error. Callers asking "is it free?" use port_free, not an empty-test.
port_holders() {
  if ! have_port_tool; then
    echo "port_holders: $(port_tool) is not installed — refusing to answer 'nobody holds :$1', which is what an empty result would mean." >&2
    return 2
  fi
  local rows pids
  if [ "$(port_tool)" = ss ]; then
    # -H drops the header; the sport filter is ss's own, so no port-number
    # substring can match by accident (":4301" vs ":14301"). A failing ss is
    # status 2: discarding its status used to make a crash read as "free".
    if ! rows=$(ss -H -ltnp "sport = :$1" 2>/dev/null); then
      echo "port_holders: ss failed for :$1 — refusing to answer 'nobody holds it'." >&2
      return 2
    fi
    printf '%s\n' "$rows" | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u
    # A row with no pid= is a listener this user cannot see into (measured:
    # a non-root ss omits users:((…)) for another user's socket).
    if printf '%s\n' "$rows" | grep -v 'pid=' | grep -q .; then
      return 3
    fi
    return 0
  fi
  # Darwin. lsof shows a non-root user only their OWN processes, so it can
  # never reveal a foreign listener. netstat lists every listening socket on
  # the machine, without an owner, and ships with macOS: a LISTEN socket on
  # the port that lsof cannot attribute is status 3. The address column ends
  # in ".PORT" (`*.5432`, `127.0.0.1.5432`, `::1.5432`); anchoring on the dot
  # keeps :15432 from matching :5432.
  if ! command -v netstat >/dev/null 2>&1; then
    echo "port_holders: netstat is not installed — without it a listener owned by another user is invisible on $(uname -s), so refusing to answer for :$1." >&2
    return 2
  fi
  if ! rows=$(netstat -an -p tcp 2>/dev/null); then
    echo "port_holders: netstat failed for :$1 — refusing to answer 'nobody holds it'." >&2
    return 2
  fi
  pids=$(lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null)
  [ -n "$pids" ] && printf '%s\n' "$pids"
  # Residual, stated in the #927 spec (D2): with one of OUR listeners on the
  # port, a second foreign one on a more specific address is not reported.
  if [ -z "$pids" ] && printf '%s\n' "$rows" |
    awk -v port="$1" '$NF == "LISTEN" && substr($4, length($4) - length(port)) == "." port { found = 1 } END { exit !found }'; then
    return 3
  fi
  return 0
}

# port_free PORT — true only when nothing at all listens on the port.
#
# THE definition of free. Testing port_holders' stdout for emptiness reads
# status 2 (cannot tell) and status 3 (a listener this user cannot name) as
# free, which is the fail-open #927 removed.
port_free() {
  local pids rc
  pids=$(port_holders "$1") && rc=0 || rc=$?
  [ "$rc" = 0 ] && [ -z "$pids" ]
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
  local pid pids rc
  # errexit-safe: status 3 is an ordinary answer, not a failure to abort on.
  pids=$(port_holders "$1") && rc=0 || rc=$?
  # Status 2 — no answer — is "not ours", so preflight fails closed rather
  # than printing `ok port 5432` for a port it cannot see into at all.
  [ "$rc" = 2 ] && return 1
  # Status 3: a listener this user cannot name is NOT ours — unless our
  # compose project publishes the port. That exception is load-bearing: a
  # published container port is held by the Docker daemon's docker-proxy,
  # which runs as root, so on a non-root Linux shell OUR OWN Postgres is
  # exactly such a listener. Without it, re-running `make up` would refuse a
  # healthy stack (#927).
  if [ "$rc" = 3 ] && ! compose_owns_port "$1"; then
    return 1
  fi
  for pid in $pids; do
    pid_is_ours "$pid" && continue
    compose_owns_port "$1" && continue
    return 1
  done
  return 0
}
