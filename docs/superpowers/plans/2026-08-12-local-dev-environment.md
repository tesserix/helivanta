# Local Development Environment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the four unmet acceptance criteria on issue #714 — a preflight that names conflicts and their fixes, a `make reset` returning a clean seeded state, a stack that survives sleep and restart, and documentation of both the new commands and the stack's deviation from the issue text.

**Architecture:** Three new bash scripts alongside the existing `verify-local.sh` / `dev-down.sh` family, sharing one ownership helper so "is this process ours?" has a single definition. `preflight.sh` runs as the first step of `make up` and reports every failure at once. `reset-dev.sh` tears down volumes behind a confirmation prompt and re-seeds. Compose gains restart policies and a pinned emulator version.

**Tech Stack:** bash (portable to macOS bash 3.2 — no associative arrays, no `${var,,}`), Docker Compose v2, GNU/BSD `lsof`, GitHub Actions.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-12-local-dev-environment-design.md`. Read it before starting.
- Branch `feat/714-local-dev-environment` already exists with the spec committed. Work there. Do not branch again.
- Commit messages: single line, conventional-commit prefix, no signatures, no AI attribution.
- **`make up` must remain safe to re-run.** A port held by this repo's own containers or processes is not a preflight failure.
- **Scripts must be portable to bash 3.2** (macOS system bash). No `declare -A`, no `${var,,}`, no `readarray`. Avoid `${arr[@]}` on possibly-empty arrays under `set -u` — this plan uses newline-delimited strings instead.
- **Tests must not require Docker or a network.** External commands are shimmed onto `PATH`.
- Every script gets `#!/usr/bin/env bash`, `set -uo pipefail`, and a comment block explaining *why* it exists — match the voice of `scripts/dev-down.sh`.
- Ports owned by this stack: `5432 4222 8222 6379 8090 9099 8080 4301 4302 4303 4304`.
- Pin `firebase-tools` to exactly **13.35.1**.
- Toolchain floors: Go **1.26** (`backend/go.mod` says 1.26.5), Node **22** (`package.json` engines `>=22`).

---

### Task 1: Shared ownership helper and the script test harness

Extract the "does this process belong to this repo?" logic that `dev-down.sh` already implements into a sourced helper, so `preflight.sh` can reuse it and both agree. Ship the test harness and CI wiring in the same task, because the helper is the first thing that needs testing.

**Files:**
- Create: `scripts/lib/repo-owns.sh`
- Create: `scripts/preflight.test.sh`
- Modify: `scripts/dev-down.sh` (replace inline `cwd_of` with the helper)
- Modify: `Makefile` (add `test-scripts` target)
- Modify: `.github/workflows/ci.yml` (add a `scripts` job)

**Interfaces:**
- Consumes: nothing.
- Produces, all sourced from `scripts/lib/repo-owns.sh`:
  - `cwd_of PID` → prints the process's working directory, empty if unreadable
  - `pid_is_ours PID` → exit 0 when that process's cwd is inside `$REPO_ROOT`
  - `port_holders PORT` → prints listening PIDs, one per line
  - `compose_owns_port PORT` → exit 0 when the `hms-dev` compose project publishes that port
  - `port_is_ours PORT` → exit 0 when the port is free, or held only by our processes or our containers
  - `$REPO_ROOT` → absolute repo path, overridable by presetting the variable

- [ ] **Step 1: Write the failing tests**

Create `scripts/preflight.test.sh`:

```bash
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
" "$2" ) &
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
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `bash scripts/preflight.test.sh`
Expected: FAIL — `scripts/lib/repo-owns.sh: No such file or directory`

- [ ] **Step 3: Write the helper**

Create `scripts/lib/repo-owns.sh`:

```bash
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
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `bash scripts/preflight.test.sh`
Expected: PASS — `7 passed`

- [ ] **Step 5: Point dev-down.sh at the helper**

In `scripts/dev-down.sh`, delete the local `cwd_of` definition and its comment:

```bash
# cwd_of prints a PID's working directory, or nothing if it cannot be read.
cwd_of() {
  lsof -a -p "$1" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1
}
```

and source the helper immediately after the `REPO_ROOT` assignment:

```bash
REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
. "$REPO_ROOT/scripts/lib/repo-owns.sh"
APP_PORTS=(4301 4302 4303 4304 8080)
```

Leave the rest of the file alone — the `case "$cwd" in` blocks still work, because `cwd_of` behaves identically.

- [ ] **Step 6: Verify dev-down still works**

Run: `bash -n scripts/dev-down.sh && ./scripts/dev-down.sh`
Expected: exits 0, prints "All HMS ports are free." (or reports containers it stopped)

- [ ] **Step 7: Add the make target**

In `Makefile`, add `test-scripts` to the `.PHONY` line and this target after `test-web`:

```make
test-scripts:
	bash scripts/preflight.test.sh
```

- [ ] **Step 8: Wire it into CI**

In `.github/workflows/ci.yml`, add a third job after `web`:

```yaml
  scripts:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Ensure lsof is present
        run: command -v lsof || sudo apt-get install -y lsof
      - run: make test-scripts
```

- [ ] **Step 9: Run everything and commit**

Run: `make test-scripts`
Expected: PASS

```bash
git add scripts/lib/repo-owns.sh scripts/preflight.test.sh scripts/dev-down.sh Makefile .github/workflows/ci.yml
git commit -m "test: shared repo-ownership helper with a dependency-free script test harness"
```

---

### Task 2: Preflight checks

**Files:**
- Create: `scripts/preflight.sh`
- Modify: `scripts/preflight.test.sh` (append preflight cases)
- Modify: `Makefile` (`dev-infra` gains a preflight dependency)

**Interfaces:**
- Consumes: `pid_is_ours`, `port_is_ours`, `$REPO_ROOT` from Task 1.
- Produces:
  - `scripts/preflight.sh` — exit 0 when everything passes, 1 otherwise
  - `PREFLIGHT_PORTS` — space-separated port list, overridable for tests
  - `version_at_least HAVE WANT` — semver comparison, exit 0 when `HAVE >= WANT`
  - Sourcing the script does **not** run the checks; `main` is guarded so tests can call individual functions.

- [ ] **Step 1: Write the failing tests**

Append to `scripts/preflight.test.sh`, immediately before the final summary block (`echo` / `if [ "$failed" -gt 0 ]`):

```bash
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
  env PATH="$dir:$PATH" NODE_AUTH_TOKEN=token PREFLIGHT_PORTS="$(free_port)" \
    "$@" bash "$REPO_ROOT/scripts/preflight.sh" 2>&1
}

out=$(run_preflight "$good"); status=$?
assert_status "clean environment exits 0" 0 "$status"

out=$(run_preflight "$good" NODE_AUTH_TOKEN=); status=$?
assert_status "missing NODE_AUTH_TOKEN exits 1" 1 "$status"
assert_contains "missing NODE_AUTH_TOKEN names the fix" "$out" 'gh auth token'

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

out=$(run_preflight "$nodocker" NODE_AUTH_TOKEN=); status=$?
assert_status "several problems exit 1" 1 "$status"
assert_contains "reports docker"    "$out" 'Docker is not running'
assert_contains "reports go"        "$out" 'Go 1.26+'
assert_contains "reports node"      "$out" 'Node 22+'
assert_contains "reports pnpm"      "$out" 'corepack enable'
assert_contains "reports the token" "$out" 'NODE_AUTH_TOKEN'

busy_port=$(free_port)
busy_pid=$(listen_from "$TMP" "$busy_port")
out=$(env PATH="$good:$PATH" NODE_AUTH_TOKEN=token PREFLIGHT_PORTS="$busy_port" \
  bash "$REPO_ROOT/scripts/preflight.sh" 2>&1); status=$?
assert_status "occupied foreign port exits 1" 1 "$status"
assert_contains "names the occupied port" "$out" "$busy_port"
assert_contains "names the fix"           "$out" "make down"
```

- [ ] **Step 2: Run to verify it fails**

Run: `make test-scripts`
Expected: FAIL — `scripts/preflight.sh: No such file or directory`

- [ ] **Step 3: Write preflight.sh**

Create `scripts/preflight.sh`:

```bash
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

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
. "$REPO_ROOT/scripts/lib/repo-owns.sh"

PREFLIGHT_PORTS=${PREFLIGHT_PORTS:-"5432 4222 8222 6379 8090 9099 8080 4301 4302 4303 4304"}
GO_MIN=1.26
NODE_MIN=22

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

check_node_auth_token() {
  if [ -n "${NODE_AUTH_TOKEN:-}" ]; then
    ok "NODE_AUTH_TOKEN"
  else
    fail "NODE_AUTH_TOKEN" \
      "NODE_AUTH_TOKEN unset (needed for @tesserix/web from GitHub Packages) — export NODE_AUTH_TOKEN=\$(gh auth token)"
  fi
}

check_ports() {
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
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `make test-scripts`
Expected: PASS — all repo-owns and preflight cases

- [ ] **Step 5: Wire preflight into `make up`**

In `Makefile`, add `preflight` to `.PHONY`, add the target, and make `dev-infra` depend on it:

```make
preflight:
	@bash scripts/preflight.sh

dev-infra: preflight
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
```

`dev-infra` rather than `up`, so `make dev-infra` on its own is also protected — and `up` gets it transitively.

- [ ] **Step 6: Verify by hand**

Run: `make preflight`
Expected: every check `ok`, exit 0.

Then prove the failure path:

```bash
NODE_AUTH_TOKEN= bash scripts/preflight.sh; echo "exit=$?"
```

Expected: `FAIL  NODE_AUTH_TOKEN`, the `gh auth token` fix printed under "Cannot start", `exit=1`.

- [ ] **Step 7: Commit**

```bash
git add scripts/preflight.sh scripts/preflight.test.sh Makefile
git commit -m "feat: preflight naming every conflict and its fix before make up"
```

---

### Task 3: `make reset`

**Files:**
- Create: `scripts/reset-dev.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `scripts/dev-down.sh`, `make dev-infra`, `make seed`.
- Produces: `make reset`; honours `RESET_YES=1` to skip confirmation.

- [ ] **Step 1: Write the script**

Create `scripts/reset-dev.sh`:

```bash
#!/usr/bin/env bash
# Returns the local stack to a clean, freshly seeded state.
#
# Confirms first, because `docker compose down -v` is unrecoverable and
# those volumes may hold a day of hand-entered test data. dev-down.sh
# already sets this precedent — it refuses to kill processes it does not
# own rather than assuming the developer meant it. Set RESET_YES=1 to skip
# the prompt from a script.
#
# Ends with seeded infrastructure but does NOT start the API and zone apps:
# `make up` runs those in the foreground, and a reset that blocked the
# terminal could not be used from a script.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)

if [ "${RESET_YES:-}" != "1" ]; then
  if [ ! -t 0 ]; then
    echo "reset needs confirmation but stdin is not a terminal." >&2
    echo "Re-run with RESET_YES=1 to proceed non-interactively." >&2
    exit 1
  fi
  echo "This destroys the local Postgres and NATS volumes (pgdata, natsdata)."
  echo "Anything you entered by hand is lost. Seeded data is recreated."
  printf 'Type "reset" to continue: '
  read -r reply
  if [ "$reply" != "reset" ]; then
    echo "Aborted — nothing was changed."
    exit 1
  fi
fi

echo "Stopping the stack…"
"$REPO_ROOT/scripts/dev-down.sh"

echo "Removing volumes…"
docker compose -f "$REPO_ROOT/docker-compose.dev.yml" down -v || exit 1

echo "Starting infrastructure…"
make -C "$REPO_ROOT" dev-infra || exit 1

echo "Seeding…"
make -C "$REPO_ROOT" seed || exit 1

echo
echo "Clean seeded state. Run 'make up' to start the API and zone apps."
```

- [ ] **Step 2: Make it executable and syntax-check**

Run: `chmod +x scripts/reset-dev.sh && bash -n scripts/reset-dev.sh`
Expected: no output, exit 0

- [ ] **Step 3: Add the make target**

In `Makefile`, add `reset` to `.PHONY` and this target after `down`:

```make
# Destroys the local volumes and re-seeds. Prompts first; RESET_YES=1 skips.
reset:
	@./scripts/reset-dev.sh
```

- [ ] **Step 4: Prove the confirmation guard**

Run: `echo "no" | ./scripts/reset-dev.sh; echo "exit=$?"`
Expected: `Aborted — nothing was changed.` and `exit=1`. Volumes still present — confirm with `docker volume ls | grep hms-dev`.

- [ ] **Step 5: Prove the happy path end to end**

This is the determinism check from the spec — seed, mutate, reset, confirm the mutation is gone:

```bash
make up   # in another terminal, or: make dev-infra && make seed
psql postgres://hms:hms@localhost:5432/hms -c \
  "UPDATE iam_members SET role_key = 'tampered' WHERE role_key = 'pharmacist';"
psql postgres://hms:hms@localhost:5432/hms -c \
  "SELECT count(*) FROM iam_members WHERE role_key = 'tampered';"   # expect > 0
RESET_YES=1 make reset
psql postgres://hms:hms@localhost:5432/hms -c \
  "SELECT count(*) FROM iam_members WHERE role_key = 'tampered';"   # expect 0
psql postgres://hms:hms@localhost:5432/hms -c \
  "SELECT count(*) FROM iam_members WHERE role_key = 'pharmacist';" # expect 2
```

Expected: `tampered` is 0 and `pharmacist` is 2 after the reset.

- [ ] **Step 6: Commit**

```bash
git add scripts/reset-dev.sh Makefile
git commit -m "feat: make reset returning the stack to a clean seeded state"
```

---

### Task 4: Survive sleep and restart

**Files:**
- Modify: `docker-compose.dev.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: a `npmcache` named volume, referenced only by `firebase-auth`.

- [ ] **Step 1: Add restart policies**

Add `restart: unless-stopped` to all five services in `docker-compose.dev.yml`. `unless-stopped` rather than `always`, so a container someone deliberately stopped stays stopped. For example, `postgres` becomes:

```yaml
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: hms
```

Do the same for `nats`, `redis`, `openfga` and `firebase-auth`.

- [ ] **Step 2: Pin the emulator and cache its download**

Replace the `firebase-auth` service's `command` and `volumes` so it reads:

```yaml
  firebase-auth:
    image: node:22-alpine
    working_dir: /workspace
    # firebase-tools is pinned exactly. `firebase-tools@13` resolved a
    # floating minor on every container start, so two machines could run
    # different emulator builds and every start needed the network —
    # directly against #714's "seeded data identical across machines".
    # The npm cache volume makes restarts fast and offline-capable.
    command: sh -c "npx -y firebase-tools@13.35.1 emulators:start --only auth --project demo-hms"
    restart: unless-stopped
    volumes:
      - ./dev/firebase:/workspace
      - npmcache:/root/.npm
    ports: ["9099:9099"]
```

- [ ] **Step 3: Declare the volume**

Extend the `volumes:` block at the bottom of the file:

```yaml
volumes:
  pgdata:
  natsdata:
  npmcache:
```

- [ ] **Step 4: Validate and restart cleanly**

Run: `docker compose -f docker-compose.dev.yml config >/dev/null && echo valid`
Expected: `valid`

Then:

```bash
RESET_YES=1 make reset
docker compose -f docker-compose.dev.yml ps --format '{{.Service}} {{.Status}}'
```

Expected: all five services `Up`, with `postgres`, `nats`, `redis` and `openfga` reporting `(healthy)`.

- [ ] **Step 5: Prove restart survival**

```bash
docker restart hms-dev-postgres-1 hms-dev-firebase-auth-1
sleep 30
make verify-local
```

Expected: every check `ok`. (Container names come from the `hms-dev` project; confirm with `docker compose -f docker-compose.dev.yml ps` if they differ.)

- [ ] **Step 6: Prove the cache works offline**

```bash
docker compose -f docker-compose.dev.yml restart firebase-auth
sleep 20
curl -fsS http://localhost:9099/ >/dev/null && echo "emulator up"
```

Expected: `emulator up`, and noticeably faster than the first start because `npx` resolves from the cache volume.

- [ ] **Step 7: Commit**

```bash
git add docker-compose.dev.yml
git commit -m "fix: restart policies and a pinned firebase-tools so the dev stack survives sleep"
```

---

### Task 5: Documentation and ADR

**Files:**
- Create: `docs/adr/0003-local-dev-stack.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: everything above.
- Produces: no code.

- [ ] **Step 1: Write the ADR**

Create `docs/adr/0003-local-dev-stack.md`, matching the house style of `0002-gip-not-keycloak.md`:

```markdown
# ADR-0003: Docker Compose is the local development stack

- **Status:** Accepted (2026-08-12)
- **Context:** Issue #714 asks for a one-command local stack and names
  "CNPG, Redis, NATS, Keycloak, OpenFGA". Two of those names predate
  decisions already taken. Keycloak was superseded by GIP in ADR-0002.
  CNPG is a Kubernetes operator and has no meaning in a Compose stack.
  Separately, `sandboxctl` (tesserix/sandboxctl) can stand up a local kind
  cluster with Argo CD and Istio, which raised the question of whether the
  local environment should be Kubernetes-shaped instead.
- **Decision:** Compose is the inner loop. Postgres is `postgres:16-alpine`
  running with the same non-superuser `hms_app` role and forced RLS that
  production uses — the property the story actually depends on. Auth is the
  Firebase Auth (GIP) emulator on `:9099`; there are no realms to stand up.
  Redis, NATS with JetStream and OpenFGA are unchanged. Where #714 says
  Keycloak, read GIP; where it says CNPG, read Compose Postgres.
  A Kubernetes-shaped environment is issue #7's, not this one's: sandboxctl
  requires a Dockerfile and a Helm chart, HMS has neither yet, and it ships
  no OpenFGA and no GIP emulator — HMS's two most distinctive dependencies.
- **Consequences:** the edit-to-see cycle stays sub-second (`next dev` HMR
  and `go run`) rather than the build-push-sync minutes a GitOps loop costs.
  Two environments will eventually exist, and the deployment artifacts
  produced under #7 will need their own verification path. OpenFGA runs on
  the in-memory datastore, so tuples are lost on restart and rebuilt from
  Postgres by the boot reconciler — Postgres remains the system of record.
```

- [ ] **Step 2: Document the commands in the README**

In `README.md`, after the paragraph beginning "Seeding is idempotent, so re-running `make up` is safe.", insert:

```markdown
`make up` runs `scripts/preflight.sh` first. It checks Docker, Compose v2,
Go, Node, pnpm, `NODE_AUTH_TOKEN` and all eleven ports the stack uses, and
reports **every** problem at once with the fix for each — a fresh machine
usually has more than one. A port held by this repo's own containers or
processes is not a conflict, so re-running `make up` on a stack that is
already running still works.

`make reset` returns the stack to a clean seeded state: it stops
everything, drops the Postgres and NATS volumes, restarts infrastructure
and re-seeds. It prompts first, because dropping those volumes is
unrecoverable; `RESET_YES=1 make reset` skips the prompt for scripts.
```

- [ ] **Step 3: Note the pinned emulator**

In the README's `### Notes` list, add:

```markdown
- `firebase-tools` is pinned to an exact version in `docker-compose.dev.yml`
  and its npm download is cached in the `npmcache` volume. The previous
  floating `@13` resolved a different minor on every container start, so
  two machines could run different emulator builds.
- All five containers use `restart: unless-stopped`, so the stack comes
  back after a laptop sleep or a Docker restart. A container you stopped
  deliberately stays stopped.
```

- [ ] **Step 4: Add the new targets to the useful-targets line**

In `README.md`, change:

```markdown
- Useful targets: `make test` (Go + web), `make lint-go`, `make coverage-go`,
  `make e2e` (Playwright, needs the stack up), `make new-module NAME=<name>`.
```

to:

```markdown
- Useful targets: `make test` (Go + web), `make test-scripts` (shell tests),
  `make preflight`, `make reset`, `make lint-go`, `make coverage-go`,
  `make e2e` (Playwright, needs the stack up), `make new-module NAME=<name>`.
```

- [ ] **Step 5: Verify the docs against reality**

Run: `make preflight && make test-scripts`
Expected: both pass — confirms the README describes commands that exist.

Read `README.md` top to bottom and check every command it names is a real target in the `Makefile`.

- [ ] **Step 6: Commit**

```bash
git add docs/adr/0003-local-dev-stack.md README.md
git commit -m "docs: record the Compose dev stack decision and the new preflight and reset commands"
```

---

### Task 6: Full verification and pull request

**Files:** none — verification only.

- [ ] **Step 1: Run the whole gate**

```bash
make test-scripts
make lint-go
cd backend && ./scripts/coverage-gate.sh && cd ..
pnpm turbo lint type-check test build
```

Expected: all green. If `pnpm turbo` fails on a missing `@tesserix/web`, `export NODE_AUTH_TOKEN=$(gh auth token)` first — which is exactly what preflight now tells you.

- [ ] **Step 2: Prove the full journey from cold**

```bash
make down
RESET_YES=1 make reset
make up          # separate terminal
make verify-local
```

Expected: `verify-local` reports every check `ok` and prints the login table.

- [ ] **Step 3: Open the pull request**

```bash
git push -u origin feat/714-local-dev-environment
gh pr create --title "feat: local development environment preflight, reset and restart survival" --body "$(cat <<'BODY'
Closes #714.

## Summary

Closes the four acceptance criteria on #714 that were not yet met. The rest of the story — one-command start, seeded tenants and users, FGA relationships, two-tenant isolation — already worked via `make up` and `scripts/seed-dev.mjs`.

- **Preflight** (`scripts/preflight.sh`) runs before `make up` touches Docker and reports *every* problem at once with a named fix: Docker, Compose v2, Go 1.26+, Node 22+, pnpm, `NODE_AUTH_TOKEN`, and all eleven ports. A port held by this repo's own containers or processes is not a conflict, so `make up` stays safe to re-run.
- **`make reset`** returns the stack to a clean seeded state, prompting before dropping volumes (`RESET_YES=1` skips).
- **Restart survival**: `restart: unless-stopped` on all five services.
- **A determinism bug fixed in passing**: `firebase-tools@13` resolved a floating minor on every container start, so two machines could run different emulator builds and every start needed the network — against the "seeded data identical across machines" criterion. Now pinned to 13.35.1 with a cached npm volume.
- **Docs**: README plus ADR-0003 recording that Keycloak means GIP and CNPG means Compose Postgres, and why the Kubernetes-shaped environment belongs to #7.

## Test evidence

- `make test-scripts` — new dependency-free bash harness; no Docker, no network. Covers the ownership helper (including the re-runnability guarantee) and every preflight failure path.
- CI gains a `scripts` job running it.
- Reset verified by seeding, tampering with a seeded row, resetting, and confirming the seeded state returns.
- `make verify-local` green from cold after `make down && make reset && make up`.

## Known limitations

- OpenFGA still uses the in-memory datastore; tuples are rebuilt from Postgres by the boot reconciler. Existing documented behaviour, unchanged.
- Preflight cannot prevent a port being taken between the check and container start. Accepted race.
- `make reset` destroys local data by design; there is no backup step.
BODY
)"
```

- [ ] **Step 4: Confirm CI**

Run: `gh pr checks --watch`
Expected: `go`, `web` and `scripts` all pass. If Actions is blocked by org billing (it has been before — see the phase-3 note), record local verification in a PR comment instead.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
| --- | --- |
| Preflight, all checks, all-failures-at-once | 2 |
| Re-runnability (our own ports are not conflicts) | 1 (helper), 2 (use) |
| `make reset`, confirmation, `RESET_YES` | 3 |
| Restart policies | 4 |
| Pinned `firebase-tools` + cache volume | 4 |
| Ownership test extracted to a shared helper | 1 |
| README + ADR-0003 | 5 |
| `scripts/preflight.test.sh`, cases listed in the spec | 1, 2 |
| Reset integration check (seed → mutate → reset → assert) | 3 step 5 |
| CI wiring | 1 step 8 |

No spec requirement is unmapped.

**Placeholder scan:** none. Every step carries the literal file content or command.

**Type consistency:** `pid_is_ours`, `port_is_ours`, `port_holders`, `cwd_of`, `compose_owns_port` and `$REPO_ROOT` are defined in Task 1 and used with those exact names in Task 2. `PREFLIGHT_PORTS`, `RESET_YES` and `npmcache` are each declared once and referenced consistently.

**Deviation from the spec, resolved:** the spec proposed a test asserting "a port held by our own *container* passes", which would need a running container in CI. Task 1 shims `docker` on `PATH` instead — same guarantee, no Docker required. The spec's known limitations are carried into the PR body unchanged.
