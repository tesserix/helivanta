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

assert_equals() { # assert_equals NAME WANT GOT
  if [ "$2" = "$3" ]; then t_ok "$1"; else
    t_fail "$1"; printf '        want [%s], got [%s]\n' "$2" "$3"
  fi
}

assert_contains() { # assert_contains NAME HAYSTACK NEEDLE
  case "$2" in
    *"$3"*) t_ok "$1" ;;
    *) t_fail "$1"; printf '        output did not contain: %s\n' "$3" ;;
  esac
}

assert_not_contains() { # assert_not_contains NAME HAYSTACK NEEDLE
  case "$2" in
    *"$3"*) t_fail "$1"; printf '        output unexpectedly contained: %s\n' "$3" ;;
    *) t_ok "$1" ;;
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
# No Zitadel answering. check_zitadel_instance_domain treats that as a fresh
# clone (its own doc comment says why that must not be a failure), so every
# case below that is not ABOUT that check is unaffected by it — and, more
# importantly, is not silently coupled to whether this machine happens to
# have a dev stack up on auth.tesserix.localhost:20080 right now.
make_shim "$good" curl        'exit 1'

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
make_shim "$old" curl   'exit 1'

out=$(run_preflight "$old"); status=$?
assert_status "old Go exits 1" 1 "$status"
assert_contains "old Go names the required version" "$out" '1.26'
assert_contains "old Go names the version found"    "$out" '1.24.2'

nodocker="$TMP/bin-nodocker"
make_shim "$nodocker" docker 'exit 1'
make_shim "$nodocker" go     'echo "go version go1.24.2 darwin/arm64"'
make_shim "$nodocker" node   'echo "v20.11.0"'
make_shim "$nodocker" curl   'exit 1'

# This case relies on pnpm being ABSENT, so the PATH must be hermetic — the
# shim dir plus only the system directories preflight genuinely needs — not
# additive. An additive PATH would still find a real pnpm from the caller's
# environment (e.g. installed globally via nvm/corepack), making this
# assertion pass or fail depending on the developer's machine rather than on
# preflight.sh's own logic. /usr/sbin (macOS) and /usr/bin (Linux) are where
# the port tool lives, which port_is_ours needs; sed/sort/head/ps come from /usr/bin
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

# A missing port-lookup tool must be reported, not silently treated as "no
# port holders found, so every port is free" — that would fail open exactly
# in the scenario #714 exists to catch (a foreign process quietly squatting
# on a port we need). Either `ss` or lsof satisfies preflight (repo-owns.sh's
# port_holders prefers ss on Linux, see its comment), so BOTH have to be off
# the PATH for this case to prove anything.
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
# compose_owns_port. cut: port_holders' ss parsing. uname: port_tool's
# platform selection. ls: the secrets-dir mode check. ps: the port-holder
# message. env: the shim shebangs.
for tool in bash env dirname sed sort head wc tr grep cut ps uname ls; do
  ln -s "$(command -v "$tool")" "$nolsof/$tool"
done
make_shim "$nolsof" docker 'exit 0'
make_shim "$nolsof" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$nolsof" node   'echo "v22.11.0"'
make_shim "$nolsof" pnpm   'echo "10.17.1"'

# Guard the guard: if either tool is reachable through this PATH the case
# below proves nothing, so say so loudly rather than reporting a green pass.
for tool in lsof ss; do
  if PATH="$nolsof" command -v "$tool" >/dev/null 2>&1; then
    t_fail "the no-port-tool PATH must not contain $tool"
  fi
done

out=$(env PATH="$nolsof" PREFLIGHT_PORTS="12345" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
# The tool, and therefore the message, is chosen by PLATFORM — see
# repo-owns.sh's port_tool. Asserting on the platform's own answer rather
# than on a hardcoded name is what keeps this test meaningful on both.
case "$(uname -s)" in
  Darwin) want_tool=lsof; want_fix='brew install lsof' ;;
  *)      want_tool=ss;   want_fix='apt-get install -y iproute2' ;;
esac
assert_status "missing port tool exits 1" 1 "$status"
assert_contains "missing port tool is reported" "$out" "$want_tool missing"
assert_contains "missing port tool names the fix" "$out" "$want_fix"

echo
echo "port_holders (the lsof-cannot-see-next-dev defect, #920):"

# THE regression test for the rewrite. Tool-absent vs tool-present, which is
# all the cases above cover, would stay green through a revert to lsof: the
# listener fixture at the top of this file is `python3`, a comm with no
# spaces or parentheses — precisely the shape lsof handles fine.
#
# lsof 4.95.0 omits a process whose comm has spaces and parens. So the
# fixture here is a copy of python3 renamed to the exact string Next.js uses
# for its dev server. Reproduced in a bare ubuntu:24.04 container: raw
# `lsof -nP -iTCP:PORT -sTCP:LISTEN -t` prints nothing and exits 1, while
# port_holders returns the pid.
#
# Not applicable on macOS, and that is stated out loud rather than skipped
# silently: port_tool selects lsof there deliberately, macOS lsof does not
# have this defect, and /usr/bin/python3 is an xcode-select shim that
# dispatches on argv[0] — renaming it breaks it, so the fixture cannot even
# be built. Linux is where the defect lives and Linux is where CI runs this.
if [ "$(uname -s)" = Darwin ]; then
  echo "  n/a   paren-named comm defect is lsof-on-Linux only; port_tool selects lsof on Darwin, which does not have it"
else
  nextdir="$TMP/nextbin"
  mkdir -p "$nextdir"
  nextbin="$nextdir/next-server (v16.2.12)"
  cp "$(command -v python3)" "$nextbin"
  nextport=$(free_port)
  ( cd "$TMP" && exec "$nextbin" -c "
import socket, sys, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', int(sys.argv[1])))
s.listen(1)
time.sleep(300)
" "$nextport" ) >/dev/null 2>&1 &
  nextpid=$!
  sleep 1

  # Guard the guard: if this listener is not actually running, everything
  # below would pass for the wrong reason (an empty answer matching an empty
  # expectation is not what is being asserted, but a dead fixture would still
  # make the whole case meaningless).
  kill -0 "$nextpid" 2>/dev/null || t_fail "the paren-named listener fixture did not start"

  assert_equals "port_holders sees a comm with spaces and parens" \
    "$nextpid" "$(port_holders "$nextport")"
  assert_equals "cwd_of works on it too" "$TMP" "$(cwd_of "$nextpid")"

  # And prove the test can fail: the tool the rewrite replaced must NOT see
  # it. If a future lsof fixes this, THIS assertion is the one that should be
  # revisited — not the two above.
  #
  # A MISSING lsof fails, rather than skipping. Skipping would drop the only
  # negative control in this file on exactly the machine most likely to lack
  # lsof (a slim Linux box), and the two assertions above would go on passing
  # with nothing proving they can fail — the same "reports healthy while
  # checking less" shape this whole case exists to catch.
  if command -v lsof >/dev/null 2>&1; then
    assert_equals "raw lsof cannot see it (the defect this guards)" \
      "" "$(lsof -nP -iTCP:"$nextport" -sTCP:LISTEN -t 2>/dev/null)"
  else
    t_fail "lsof is not installed — the negative control cannot run, so nothing here proves the two assertions above can fail (sudo apt-get install -y lsof)"
  fi

  kill "$nextpid" 2>/dev/null
fi

echo
echo "a listener this user cannot identify is not free (#927):"

# Every case above has an ATTRIBUTABLE holder or none. The fail-open #927
# removed lives between them: a listener that exists but cannot be named
# printed nothing, and nothing read as "free".
#
# Stubbed first, so the logic is pinned on every platform. The Linux row is
# the shape MEASURED from a non-root `ss -H -ltnp` against a root-owned
# listener: the socket line with no users:((…)) field.
foreign_row='LISTEN 0      1      127.0.0.1:5432 0.0.0.0:*'
own_row='LISTEN 0      1      127.0.0.1:5432 0.0.0.0:* users:(("python3",pid=4242,fd=3))'
linux="$TMP/bin-linux"
make_shim "$linux" uname 'echo Linux'

unattrib="$TMP/bin-unattrib"
make_shim "$unattrib" ss "echo '$foreign_row'"
out=$(PATH="$unattrib:$linux:$PATH" port_holders 5432); status=$?
assert_status "ss: a row without pid= is status 3" 3 "$status"
assert_equals "ss: and prints no pid for it" "" "$out"

PATH="$unattrib:$linux:$no_compose:$PATH" port_is_ours 5432
assert_status "port_is_ours false for a listener nobody can name" 1 "$?"

# docker-proxy runs as root, so OUR OWN published container port is exactly
# this row on a non-root shell. Re-running `make up` must still pass.
unattrib_compose="$TMP/bin-unattrib-compose"
make_shim "$unattrib_compose" docker \
  "echo '{\"Service\":\"postgres\",\"Publishers\":[{\"PublishedPort\":5432}]}'"
PATH="$unattrib:$linux:$unattrib_compose:$PATH" port_is_ours 5432
assert_status "port_is_ours true for it when our compose publishes the port" 0 "$?"

PATH="$unattrib:$linux:$PATH" port_free 5432
assert_status "port_free false for a listener nobody can name" 1 "$?"

mixed="$TMP/bin-mixed"
make_shim "$mixed" ss "echo '$own_row'; echo '$foreign_row'"
out=$(PATH="$mixed:$linux:$PATH" port_holders 5432); status=$?
assert_status "ss: one named and one unnamed listener is still status 3" 3 "$status"
assert_equals "ss: and the named pid is still printed" "4242" "$out"

named="$TMP/bin-named"
make_shim "$named" ss "echo '$own_row'"
out=$(PATH="$named:$linux:$PATH" port_holders 5432); status=$?
assert_status "ss: a fully attributed port is status 0" 0 "$status"
assert_equals "ss: with its pid" "4242" "$out"

emptyss="$TMP/bin-emptyss"
make_shim "$emptyss" ss 'exit 0'
PATH="$emptyss:$linux:$PATH" port_free 5432
assert_status "port_free true when ss lists nothing" 0 "$?"

brokenss="$TMP/bin-brokenss"
make_shim "$brokenss" ss 'exit 1'
PATH="$brokenss:$linux:$PATH" port_holders 5432 >/dev/null 2>&1
assert_status "ss: a failing ss is status 2, not an empty 'free'" 2 "$?"
PATH="$brokenss:$linux:$PATH" port_free 5432 2>/dev/null
assert_status "port_free false when ss failed" 1 "$?"

# Darwin: lsof shows a non-root user only their own processes, so netstat is
# what reveals the listener. The rows are macOS `netstat -an -p tcp` shape;
# this pins the parsing, it does not run on a Mac (see the spec, D2).
darwin="$TMP/bin-darwin"
make_shim "$darwin" uname 'echo Darwin'
make_shim "$darwin" lsof 'exit 1'
make_shim "$darwin" netstat 'cat <<NETSTAT
Active Internet connections (including servers)
Proto Recv-Q Send-Q  Local Address          Foreign Address        (state)
tcp4       0      0  127.0.0.1.15432        *.*                    LISTEN
tcp6       0      0  *.5432                 *.*                    LISTEN
tcp4       0      0  192.168.1.20.5432      10.0.0.9.61000         ESTABLISHED
NETSTAT'
out=$(PATH="$darwin:$PATH" port_holders 5432); status=$?
assert_status "darwin: a netstat LISTEN with no lsof pid is status 3" 3 "$status"
assert_equals "darwin: and prints no pid" "" "$out"
darwin_suffix="$TMP/bin-darwin-suffix"
make_shim "$darwin_suffix" uname 'echo Darwin'
make_shim "$darwin_suffix" lsof 'exit 1'
make_shim "$darwin_suffix" netstat 'echo "tcp4  0  0  127.0.0.1.15432  *.*  LISTEN"'
PATH="$darwin_suffix:$PATH" port_holders 5432 >/dev/null
assert_status "darwin: a listener on :15432 is not one on :5432" 0 "$?"
PATH="$darwin:$PATH" port_holders 61000 >/dev/null
assert_status "darwin: a non-LISTEN row is not a listener" 0 "$?"

darwin_own="$TMP/bin-darwin-own"
make_shim "$darwin_own" uname 'echo Darwin'
make_shim "$darwin_own" lsof 'echo 4242'
make_shim "$darwin_own" netstat 'echo "tcp4  0  0  127.0.0.1.5432  *.*  LISTEN"'
out=$(PATH="$darwin_own:$PATH" port_holders 5432); status=$?
assert_status "darwin: a listener lsof names is status 0" 0 "$status"
assert_equals "darwin: with its pid" "4242" "$out"

# No netstat at all: a PATH holding exactly the tools port_holders' Darwin
# branch needs, symlinked (never a system dir — see the no-port-tool case).
darwin_nonet="$TMP/bin-darwin-nonet"
mkdir -p "$darwin_nonet"
for tool in bash env; do
  ln -s "$(command -v "$tool")" "$darwin_nonet/$tool"
done
make_shim "$darwin_nonet" uname 'echo Darwin'
make_shim "$darwin_nonet" lsof 'exit 1'
PATH="$darwin_nonet" port_holders 5432 >/dev/null 2>&1
assert_status "darwin: no netstat is status 2, not an empty 'free'" 2 "$?"

# And for real: a listener owned by ROOT, read by this non-root user. This
# is what proves the premise — that the platform tool really does omit the
# owner — rather than trusting the stubbed row above. It needs passwordless
# sudo and a non-root user. CI sets PREFLIGHT_TEST_REQUIRE_FOREIGN=1 so it
# fails there instead of quietly saying n/a.
if [ "$(id -u)" != 0 ] && sudo -n true 2>/dev/null; then
  root_port=$(free_port)
  sudo -n python3 -c "
import socket, sys, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', int(sys.argv[1])))
s.listen(1)
time.sleep(60)
" "$root_port" >/dev/null 2>&1 &
  sleep 1
  # Guard the guard: a listener that never started would make "free" the
  # right answer, and the assertions below would fail for the wrong reason.
  if ! sudo -n python3 -c "
import socket, sys
socket.create_connection(('127.0.0.1', int(sys.argv[1])), 2).close()
" "$root_port" 2>/dev/null; then
    t_fail "the root-owned listener fixture did not start"
  fi

  out=$(port_holders "$root_port"); status=$?
  assert_status "a real root-owned listener is status 3" 3 "$status"
  assert_equals "and no pid is visible to this user" "" "$out"
  PATH="$no_compose:$PATH" port_is_ours "$root_port"
  assert_status "port_is_ours false for a real root-owned listener" 1 "$?"
  out=$(env PATH="$good:$PATH" PREFLIGHT_PORTS="$root_port" \
    bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
  assert_status "preflight exits 1 on a real root-owned listener" 1 "$status"
  assert_contains "and says the holder cannot be identified" "$out" \
    "port $root_port is held by a process this user cannot identify"
  assert_not_contains "and does not print ok for it" "$out" "ok    port $root_port"
  sudo -n pkill -f "time.sleep(60)" 2>/dev/null || true
elif [ "${PREFLIGHT_TEST_REQUIRE_FOREIGN:-0}" = 1 ]; then
  t_fail "PREFLIGHT_TEST_REQUIRE_FOREIGN=1 but the real foreign-listener case cannot run here (needs a non-root user with passwordless sudo)"
else
  echo "  n/a   real foreign-listener case needs a non-root user with passwordless sudo (CI runs it with PREFLIGHT_TEST_REQUIRE_FOREIGN=1)"
fi

echo
echo "zitadel secrets dir:"

# The daemon-created, root-owned bind-mount source (#920). Absent and
# owned-by-us are both fine — `make dev-infra` creates or chmods it. Only a
# directory owned by someone else without world-write is unfixable from
# here, which is exactly what `docker compose up` leaves behind.
secrets_ok="$TMP/secrets-ok"
mkdir -p "$secrets_ok"
out=$(HELIVANTA_SECRETS_DIR="$secrets_ok" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1)
assert_contains "a directory we own passes" "$out" 'zitadel secrets dir (owned by you'

out=$(HELIVANTA_SECRETS_DIR="$TMP/nope" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1)
assert_contains "an absent directory passes" "$out" 'zitadel secrets dir (absent'

# /usr stands in for the daemon-created directory: root-owned, 0755, present
# on both platforms. Running as root would make it "ours" and prove nothing,
# so say so rather than passing.
if [ -O /usr ]; then
  t_fail "this case cannot run as root — /usr must not be owned by the test user"
else
  out=$(HELIVANTA_SECRETS_DIR=/usr bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
  assert_status "a foreign non-writable directory fails" 1 "$status"
  assert_contains "and names the daemon as the cause" "$out" 'bind-mount source as root'
  assert_contains "and names the fix" "$out" 'sudo rm -rf /usr'
fi

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
echo "zitadel instance domain check:"

# The sibling of the masterkey case above, and it exists for the same reason:
# Zitadel does not fail legibly on this class of misconfiguration. A volume
# provisioned for one HELIVANTA_ZITADEL_HOST answers /debug/healthz 200 on ANY
# host — that endpoint is instance-independent — and then every management
# call dies with a bare `HTTP 404 {"code":5,"message":"Instance not found"}`,
# naming neither the host nor the fix. So the check must be proven able to
# FAIL, not merely present.
#
# curl is shimmed rather than reached over the network: these tests must run
# identically on a laptop with a dev stack up, a laptop with none, and a CI
# runner with no Docker at all.

# A Zitadel that is up (healthz 200) but was provisioned for a DIFFERENT
# host — the exact state `RESET_YES=1 make reset` exists to fix.
stale="$TMP/bin-stale"
make_shim "$stale" docker 'exit 0'
make_shim "$stale" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$stale" node   'echo "v22.11.0"'
make_shim "$stale" pnpm   'echo "10.17.1"'
make_shim "$stale" curl   'case "$*" in
  *debug/healthz*)          exit 0 ;;
  *openid-configuration*)   echo "{\"code\":5,\"message\":\"Instance not found\"}"; exit 0 ;;
esac
exit 1'

out=$(env PATH="$stale:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "a stale Zitadel instance domain exits 1" 1 "$status"
assert_contains "the unrecognised host is named" "$out" "does not recognise the host 'auth.example.localhost'"
assert_contains "the fix is named"               "$out" "RESET_YES=1 make reset"
assert_contains "the symptom it prevents is named" "$out" "Instance not found"

# The can-PASS half. Without it the case above would also be satisfied by a
# check that failed unconditionally.
live="$TMP/bin-live"
make_shim "$live" docker 'exit 0'
make_shim "$live" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$live" node   'echo "v22.11.0"'
make_shim "$live" pnpm   'echo "10.17.1"'
make_shim "$live" curl   'case "$*" in
  *debug/healthz*)        exit 0 ;;
  *openid-configuration*) echo "{\"issuer\":\"http://auth.example.localhost:20080\"}"; exit 0 ;;
esac
exit 1'

out=$(env PATH="$live:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "a Zitadel provisioned for the configured host exits 0" 0 "$status"
assert_contains "the healthy host is named" "$out" "zitadel instance domain (auth.example.localhost)"

# A Zitadel that is not running at all is a fresh clone, not a failure —
# demanding a live one here would block the very command that starts it.
out=$(run_preflight "$good"); status=$?
assert_status "no Zitadel answering exits 0" 0 "$status"
assert_contains "a cold stack is reported as such" "$out" "not running yet"

# The tightened fallback (final #916 review). A body that is neither a
# discovery document nor the known "Instance not found" error used to take
# the SAME branch as "still provisioning" and print `ok` — fail-open inside
# a fail-closed guard. An intercepting proxy's error page, or Zitadel
# rewording the not-found message, would have silently satisfied the very
# check that exists to translate that failure. Unrecognised is now a FAILURE,
# and this proves it.
unknown="$TMP/bin-unknown"
make_shim "$unknown" docker 'exit 0'
make_shim "$unknown" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$unknown" node   'echo "v22.11.0"'
make_shim "$unknown" pnpm   'echo "10.17.1"'
make_shim "$unknown" curl   'case "$*" in
  *debug/healthz*)        exit 0 ;;
  *openid-configuration*) echo "<html>502 Bad Gateway</html>"; exit 0 ;;
esac
exit 1'

out=$(env PATH="$unknown:$PATH" HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-instance-domain 2>&1); status=$?
assert_status "an unrecognisable discovery response exits 1" 1 "$status"
assert_contains "the unrecognisable response is not called provisioning" "$out" "was not recognisable"
assert_contains "the actual body is quoted back" "$out" "502 Bad Gateway"

# The one shape that IS still treated as provisioning: healthz answers but
# the discovery request produced no body at all (a connection reset while
# Zitadel is still coming up). Kept narrow deliberately — "Instance not
# found" is a body, so a stale volume can never land here.
booting="$TMP/bin-booting"
make_shim "$booting" docker 'exit 0'
make_shim "$booting" go     'echo "go version go1.26.5 darwin/arm64"'
make_shim "$booting" node   'echo "v22.11.0"'
make_shim "$booting" pnpm   'echo "10.17.1"'
make_shim "$booting" curl   'case "$*" in
  *debug/healthz*)        exit 0 ;;
  *openid-configuration*) exit 52 ;;
esac
exit 1'

out=$(env PATH="$booting:$PATH" HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-instance-domain 2>&1); status=$?
assert_status "an empty discovery response is still treated as provisioning" 0 "$status"
assert_contains "provisioning is reported as such" "$out" "still provisioning"

# `bash scripts/preflight.sh --only zitadel-instance-domain` is what
# `make dev-infra` runs between its healthz wait and
# scripts/zitadel-bootstrap.mjs — the one moment the stale instance is
# actually observable. Its exit status is the whole point, so it is asserted
# directly rather than inferred from the full-suite cases above.
out=$(env PATH="$stale:$PATH" HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-instance-domain 2>&1); status=$?
assert_status "--only zitadel-instance-domain exits 1 on a stale volume" 1 "$status"
assert_contains "--only reports the same cause" "$out" "RESET_YES=1 make reset"

out=$(env PATH="$live:$PATH" HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-instance-domain 2>&1); status=$?
assert_status "--only zitadel-instance-domain exits 0 on a healthy instance" 0 "$status"

out=$(env PATH="$good:$PATH" bash "$REPO_ROOT/scripts/preflight.sh" --only no-such-check 2>&1); status=$?
assert_status "--only with an unknown check name exits 2" 2 "$status"

# The remedy must not be blocked by the check that names it.
# scripts/reset-dev.sh runs preflight BEFORE it drops the volume, and the
# failure message above says to run `RESET_YES=1 make reset` — so without
# PREFLIGHT_SKIP_ZITADEL_INSTANCE the only documented fix would refuse to
# run for exactly the reason it was invoked. Asserted on reset-dev.sh's own
# behaviour, not just on the variable, so removing the variable from that
# script fails here.
out=$(env PATH="$stale:$PATH" PREFLIGHT_PORTS="$(free_port)" \
  HELIVANTA_ZITADEL_HOST="auth.example.localhost" \
  PREFLIGHT_SKIP_ZITADEL_INSTANCE=1 \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "PREFLIGHT_SKIP_ZITADEL_INSTANCE=1 lets a stale instance pass" 0 "$status"
assert_contains "the skip says so out loud" "$out" "skipped — this run re-provisions it"

assert_contains "reset-dev.sh sets the skip when it runs preflight" \
  "$(cat "$REPO_ROOT/scripts/reset-dev.sh")" \
  'PREFLIGHT_SKIP_ZITADEL_INSTANCE=1 "$REPO_ROOT/scripts/preflight.sh"'

echo
echo "zitadel-login health check (#923):"

# zitadel-login reads its login-client PAT once at container boot and can
# latch an invalid one — verified by experiment, not inferred (#923): a live
# wedged container was found with the PAT ON DISK returning HTTP 200 against
# Zitadel core, while the CONTAINER mounting that same file, read-only,
# reported `Errors.Token.Invalid (AUTH-7fs1e)` HTTP 401 — 2329 consecutive
# health-check failures (~19.4 hours — its entire life), with AUTH-7fs1e in
# every one of the (last five, all Docker retains) logged probe outputs —
# and a bare `docker restart` (no volume change) took it straight to
# healthy. The image already bakes in a HEALTHCHECK for exactly this; the
# gap #923 closes is that nothing above it used to read the signal.
#
# docker is shimmed rather than reached over the network, for the same
# reason as the instance-domain checks above: these must run identically on
# a laptop with a dev stack up, a laptop with none, and a CI runner with no
# Docker at all.
#
# Every shim below falls through to a bare `exit 0` for anything that is NOT
# a `docker inspect --format` call, because run_one() (preflight.sh) now
# calls check_docker before dispatching to the named check — `docker info`
# has to succeed for these cases to test the health check and not
# check_docker instead.

healthy_login="$TMP/bin-healthy-login"
make_shim "$healthy_login" docker 'case "$*" in
  *"inspect --format"*) echo "healthy"; exit 0 ;;
esac
exit 0'

out=$(env PATH="$healthy_login:$PATH"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "a healthy zitadel-login exits 0" 0 "$status"
# assert_equals, not assert_contains: the label "zitadel-login health" is
# printed by BOTH the ok() and fail() branches, and every other branch's ok
# message is "zitadel-login health (...)" — a substring check on the bare
# label alone passes even if the healthy) case is deleted entirely and
# healthy falls through to the "not running yet" branch. Proven: deleting
# the healthy) case made this exact assertion fail (review round #2 of
# #923), where the old assert_contains kept passing.
assert_equals "the healthy status line is exact" "$(printf '  ok    docker daemon\n  ok    zitadel-login health')" "$out"

# The branch that matters: the container is up and the image's own
# HEALTHCHECK has already caught the wedge. This must FAIL, and it must name
# both the cause and the remedy — an unreadable failure here is exactly what
# #923 was filed to stop happening again.
unhealthy_login="$TMP/bin-unhealthy-login"
make_shim "$unhealthy_login" docker 'case "$*" in
  *"inspect --format"*) echo "unhealthy"; exit 0 ;;
esac
exit 0'

out=$(env PATH="$unhealthy_login:$PATH"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "an unhealthy zitadel-login exits 1" 1 "$status"
assert_contains "the restart remedy is named" "$out" "docker restart helivanta-dev-zitadel-login-1"
assert_contains "the cause is named" "$out" "AUTH-7fs1e"

# A RUNNING container reporting no Health block at all is NOT the same as an
# absent one, and must not be reported the same way — this is the detection
# mechanism itself going blind (the image dropped/moved its HEALTHCHECK),
# which #923's review round #2 proved would otherwise print a false `ok`
# about a container that is actually up (`docker inspect
# --format '{{.State.Health.Status}}'` errors on a container with no
# healthcheck, rather than returning empty, and a bare `2>/dev/null`
# swallowed that). Must FAIL, and must name the mechanism it lost.
no_healthcheck_login="$TMP/bin-no-healthcheck-login"
make_shim "$no_healthcheck_login" docker 'case "$*" in
  *"inspect --format"*) echo "no-healthcheck"; exit 0 ;;
esac
exit 0'

out=$(env PATH="$no_healthcheck_login:$PATH"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "a running container with no HEALTHCHECK exits 1" 1 "$status"
assert_contains "the lost mechanism is named" "$out" "no Docker HEALTHCHECK"

# A container that does not exist yet is a fresh clone (or one that has not
# reached `make dev-infra` yet), not a failure — demanding a live container
# here would block the very command that creates one.
absent_login="$TMP/bin-absent-login"
make_shim "$absent_login" docker 'case "$*" in
  *"inspect --format"*) echo "Error: No such object: helivanta-dev-zitadel-login-1" >&2; exit 1 ;;
esac
exit 0'

out=$(env PATH="$absent_login:$PATH"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "an absent zitadel-login exits 0 (fresh stack must not fail)" 0 "$status"
assert_contains "absence is reported as not-yet-started" "$out" "not running yet"

# Docker's own transient in-between state, distinct from "wedged" — a fresh
# stack still inside the healthcheck's own start period must also pass.
starting_login="$TMP/bin-starting-login"
make_shim "$starting_login" docker 'case "$*" in
  *"inspect --format"*) echo "starting"; exit 0 ;;
esac
exit 0'

out=$(env PATH="$starting_login:$PATH"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "a still-starting zitadel-login exits 0" 0 "$status"
assert_contains "starting is reported as such" "$out" "still starting"

# run_one() now calls check_docker before dispatching (#923 review round
# #2): scripts/e2e.sh and scripts/verify-local.sh invoke this entry point
# directly, never through main(), so without this a down daemon would print
# a false `ok ... not running yet` about the health check rather than
# reporting the daemon itself as the problem.
nodocker_login="$TMP/bin-nodocker-login"
make_shim "$nodocker_login" docker 'exit 1'

out=$(env PATH="$nodocker_login:/usr/bin:/bin:/usr/sbin:/sbin"   bash "$REPO_ROOT/scripts/preflight.sh" --only zitadel-login-health 2>&1); status=$?
assert_status "--only reports docker being down, not the named check" 1 "$status"
assert_contains "docker itself is named as the problem" "$out" "Docker is not running"

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
