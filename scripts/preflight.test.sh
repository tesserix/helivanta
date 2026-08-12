#!/usr/bin/env bash
# Tests for scripts/lib/repo-owns.sh and scripts/preflight.sh.
#
# Deliberately dependency-free: no bats, no Docker, no network. Anything
# external (docker, go, node, pnpm) is shimmed onto PATH, so these run
# identically on a laptop and on a CI runner.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
passed=0
failed=0

t_ok()   { passed=$((passed + 1)); printf '  ok    %s\n' "$1"; }
t_fail() { failed=$((failed + 1)); printf '  FAIL  %s\n' "$1"; }

assert_status() { # assert_status NAME WANT GOT
  if [ "$2" = "$3" ]; then t_ok "$1"; else
    t_fail "$1"; printf '        want exit %s, got %s\n' "$2" "$3"
  fi
}

assert_contains() { # assert_contains NAME HAYSTACK NEEDLE
  case "$2" in
    *"$3"*) t_ok "$1" ;;
    *) t_fail "$1"; printf '        output did not contain: %s\n' "$3" ;;
  esac
}

# --- fixtures -------------------------------------------------------------

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"; kill $(jobs -p) 2>/dev/null' EXIT

# listen_from DIR PORT — start a listener whose cwd is DIR, print its pid.
listen_from() {
  ( cd "$1" && exec python3 -c "
import socket, sys, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', int(sys.argv[1])))
s.listen(1)
time.sleep(300)
" "$2" ) >/dev/null 2>&1 &
  local pid=$!
  sleep 1
  echo "$pid"
}

# free_port — a port nothing is using.
free_port() {
  python3 -c "
import socket
s = socket.socket(); s.bind(('127.0.0.1', 0))
print(s.getsockname()[1]); s.close()
"
}

# shim_dir NAME BODY... — build a PATH dir containing fake executables.
make_shim() { # make_shim DIR NAME BODY
  mkdir -p "$1"
  printf '#!/usr/bin/env bash\n%s\n' "$3" > "$1/$2"
  chmod +x "$1/$2"
}

# --- repo-owns.sh ---------------------------------------------------------

. "$REPO_ROOT/scripts/lib/repo-owns.sh"

echo "repo-owns.sh:"

own_port=$(free_port)
own_pid=$(listen_from "$REPO_ROOT" "$own_port")
pid_is_ours "$own_pid"
assert_status "pid_is_ours true for a process inside the repo" 0 "$?"

foreign_port=$(free_port)
foreign_pid=$(listen_from "$TMP" "$foreign_port")
pid_is_ours "$foreign_pid"
assert_status "pid_is_ours false for a process outside the repo" 1 "$?"

# docker shim that reports no published ports at all
no_compose="$TMP/bin-nocompose"
make_shim "$no_compose" docker 'exit 0'

PATH="$no_compose:$PATH" port_is_ours "$(free_port)"
assert_status "port_is_ours true for a free port" 0 "$?"

PATH="$no_compose:$PATH" port_is_ours "$foreign_port"
assert_status "port_is_ours false for a foreign holder" 1 "$?"

PATH="$no_compose:$PATH" port_is_ours "$own_port"
assert_status "port_is_ours true when we hold it ourselves" 0 "$?"

# docker shim that reports our compose project publishing the foreign port —
# this is the re-runnability guarantee: our own containers are not a conflict.
yes_compose="$TMP/bin-compose"
make_shim "$yes_compose" docker \
  "echo '{\"Service\":\"postgres\",\"Publishers\":[{\"PublishedPort\":$foreign_port}]}'"

PATH="$yes_compose:$PATH" port_is_ours "$foreign_port"
assert_status "port_is_ours true when our compose project publishes it" 0 "$?"

echo
if [ "$failed" -gt 0 ]; then
  echo "$failed failed, $passed passed"
  exit 1
fi
echo "$passed passed"
