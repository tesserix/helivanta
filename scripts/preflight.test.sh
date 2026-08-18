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

# --- preflight.sh ---------------------------------------------------------

echo
echo "preflight.sh:"

# A PATH containing satisfactory versions of everything preflight looks for.
good="$TMP/bin-good"
make_shim "$good" docker      'exit 0'
make_shim "$good" go          'echo "go version go1.26.5 darwin/arm64"'
make_shim "$good" node        'echo "v22.11.0"'
make_shim "$good" pnpm        'echo "10.17.1"'

run_preflight() { # run_preflight PATHDIR [ENV=VAL ...]
  local dir="$1"; shift
  env PATH="$dir:$PATH" PREFLIGHT_PORTS="$(free_port)" \
    "$@" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1
}

out=$(run_preflight "$good"); status=$?
assert_status "clean environment exits 0" 0 "$status"

old="$TMP/bin-oldgo"
make_shim "$old" docker 'exit 0'
make_shim "$old" go     'echo "go version go1.24.2 darwin/arm64"'
make_shim "$old" node   'echo "v22.11.0"'
make_shim "$old" pnpm   'echo "10.17.1"'

out=$(run_preflight "$old"); status=$?
assert_status "old Go exits 1" 1 "$status"
assert_contains "old Go names the required version" "$out" '1.26'
assert_contains "old Go names the version found"    "$out" '1.24.2'

nodocker="$TMP/bin-nodocker"
make_shim "$nodocker" docker 'exit 1'
make_shim "$nodocker" go     'echo "go version go1.24.2 darwin/arm64"'
make_shim "$nodocker" node   'echo "v20.11.0"'

# This case relies on pnpm being ABSENT, so the PATH must be hermetic — the
# shim dir plus only the system directories preflight genuinely needs — not
# additive. An additive PATH would still find a real pnpm from the caller's
# environment (e.g. installed globally via nvm/corepack), making this
# assertion pass or fail depending on the developer's machine rather than on
# preflight.sh's own logic. /usr/sbin (macOS) and /usr/bin (Linux) are where
# lsof lives, which port_is_ours needs; sed/sort/head/ps come from /usr/bin
# and /bin.
out=$(env PATH="$nodocker:/usr/bin:/bin:/usr/sbin:/sbin" \
  PREFLIGHT_PORTS="$(free_port)" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "several problems exit 1" 1 "$status"
assert_contains "reports docker"    "$out" 'Docker is not running'
assert_contains "reports go"        "$out" 'Go 1.26+'
assert_contains "reports node"      "$out" 'Node 22+'
assert_contains "reports pnpm"      "$out" 'corepack enable'

busy_port=$(free_port)
busy_pid=$(listen_from "$TMP" "$busy_port")
out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$busy_port" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "occupied foreign port exits 1" 1 "$status"
assert_contains "names the occupied port" "$out" "$busy_port"
assert_contains "names the fix"           "$out" "make down"

# A missing lsof must be reported, not silently treated as "no port
# holders found, so every port is free" — that would fail open exactly in
# the scenario #714 exists to catch (a foreign process quietly squatting on
# a port we need).
#
# The PATH for this case must therefore be EXACTLY one directory: $nolsof,
# holding a symlink to every real tool preflight.sh and repo-owns.sh use,
# plus the usual shims. Appending any real system bin dir instead — this
# test used to append /bin, on the theory that lsof only ever lives in
# /usr/bin or /usr/sbin — puts lsof straight back on the PATH on Ubuntu,
# where /bin is a SYMLINK to /usr/bin (the usr-merge). Preflight then passes
# and all three assertions below fail on CI while passing on macOS, where
# /bin is still a real directory. Symlink the tools, never a directory.
nolsof="$TMP/bin-nolsof"
mkdir -p "$nolsof"
# bash: the script is invoked as `bash ...` and every shim's shebang is
# `/usr/bin/env bash`. dirname: REPO_ROOT in both scripts. sed/sort/head:
# version_at_least and cwd_of. wc/tr: the masterkey length check. grep:
# compose_owns_port. ps: the port-holder message. env: the shim shebangs.
for tool in bash env dirname sed sort head wc tr grep ps; do
  ln -s "$(command -v "$tool")" "$nolsof/$tool"
done
make_shim "$nolsof" docker 'exit 0'
make_shim "$nolsof" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$nolsof" node   'echo "v22.11.0"'
make_shim "$nolsof" pnpm   'echo "10.17.1"'

# Guard the guard: if lsof is reachable through this PATH the case below
# proves nothing, so say so loudly rather than reporting a green pass.
if PATH="$nolsof" command -v lsof >/dev/null 2>&1; then
  t_fail "the no-lsof PATH must not contain lsof"
fi

out=$(env PATH="$nolsof" PREFLIGHT_PORTS="12345" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "missing lsof exits 1" 1 "$status"
assert_contains "missing lsof is reported" "$out" 'lsof missing'
assert_contains "missing lsof names the fix" "$out" 'apt-get install -y lsof'

echo
echo "PREFLIGHT_PORTS (host-port overrides):"

# The HELIVANTA_* port variables (see .env.example) must actually reach the port
# list preflight checks — not just exist as unused config. Override every
# one to a distinct free port (so the run is hermetic regardless of what
# this machine's own ports 5432/6379/8080/etc. happen to be doing) and
# confirm each shows up as a checked port.
p_pg=$(free_port); p_nats=$(free_port); p_natsmon=$(free_port)
p_redis=$(free_port); p_fga=$(free_port); p_zitadel=$(free_port); p_zitadelpg=$(free_port); p_api=$(free_port)
out=$(env PATH="$good:$PATH" \
  HELIVANTA_PG_PORT="$p_pg" HELIVANTA_NATS_PORT="$p_nats" HELIVANTA_NATS_MONITOR_PORT="$p_natsmon" \
  HELIVANTA_REDIS_PORT="$p_redis" HELIVANTA_OPENFGA_PORT="$p_fga" HELIVANTA_ZITADEL_PORT="$p_zitadel" \
  HELIVANTA_ZITADEL_PG_PORT="$p_zitadelpg" \
  HELIVANTA_API_PORT="$p_api" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "all HELIVANTA_* ports overridden to free ports exits 0" 0 "$status"
assert_contains "PREFLIGHT_PORTS reflects HELIVANTA_PG_PORT override"      "$out" "port $p_pg"
assert_contains "PREFLIGHT_PORTS reflects HELIVANTA_OPENFGA_PORT override" "$out" "port $p_fga"
assert_contains "PREFLIGHT_PORTS reflects HELIVANTA_ZITADEL_PORT override" "$out" "port $p_zitadel"
assert_contains "PREFLIGHT_PORTS reflects HELIVANTA_API_PORT override"     "$out" "port $p_api"

echo
echo "HELIVANTA_DEV_ZITADEL_MASTERKEY length check:"

# Proves the check can actually fail (engineering-principles.md §5) rather
# than trusting that a wrong-length key would be caught — the whole reason
# this check exists is that Zitadel itself does NOT fail fast on this, it
# crash-loops instead (see docker-compose.dev.yml's zitadel service
# comment), so a silently-passing check here would be worse than none.
out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  HELIVANTA_DEV_ZITADEL_MASTERKEY="tooShort" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "a masterkey that is not 32 bytes exits 1" 1 "$status"
assert_contains "the wrong length is reported" "$out" "is 8 bytes, want exactly 32"

out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  HELIVANTA_DEV_ZITADEL_MASTERKEY="HmsDevZitadelMasterKey32BytesXXX" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "the real 32-byte default exits 0" 0 "$status"

echo
echo "PREFLIGHT_SKIP (Makefile):"

# Make auto-imports every shell environment variable, so an ambient
# PREFLIGHT_SKIP=1 (left exported from an earlier debugging session, say)
# must not silently disable preflight for every `make up`/`make dev-infra`.
# Only a deliberate command-line `make ... PREFLIGHT_SKIP=1` — reset-dev.sh's
# use — may skip it.
out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  PREFLIGHT_SKIP=1 make -C "$REPO_ROOT" preflight 2>&1); status=$?
assert_status "ambient PREFLIGHT_SKIP=1 still runs preflight, exits 0 on a clean env" 0 "$status"
case "$out" in
  *"Preflight skipped"*) t_fail "ambient PREFLIGHT_SKIP=1 must not skip preflight" ;;
  *) t_ok "ambient PREFLIGHT_SKIP=1 must not skip preflight" ;;
esac

out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  make -C "$REPO_ROOT" preflight PREFLIGHT_SKIP=1 2>&1); status=$?
assert_status "command-line PREFLIGHT_SKIP=1 exits 0" 0 "$status"
assert_contains "command-line PREFLIGHT_SKIP=1 skips preflight" "$out" "Preflight skipped"

echo
echo "reset-dev.sh:"

# Sourced (not $0), so the main guard keeps this from running the script.
. "$REPO_ROOT/scripts/reset-dev.sh"

reset_reply_accepts "reset"
assert_status "reset_reply_accepts true for the exact word" 0 "$?"

reset_reply_accepts "no"
assert_status "reset_reply_accepts false for no" 1 "$?"

reset_reply_accepts ""
assert_status "reset_reply_accepts false for empty string" 1 "$?"

reset_reply_accepts "RESET"
assert_status "reset_reply_accepts false for RESET (case-sensitive)" 1 "$?"

reset_reply_accepts " reset"
assert_status "reset_reply_accepts false for leading whitespace" 1 "$?"

echo
if [ "$failed" -gt 0 ]; then
  echo "$failed failed, $passed passed"
  exit 1
fi
echo "$passed passed"
