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
    "")
      # No body at all: the request did not complete (a connection reset
      # mid-boot, a proxy that hung up). healthz answered a moment ago, so
      # this is a Zitadel still coming up, not a host it does not know —
      # "Instance not found" is a body, and a 404 with one would have
      # matched the case above. This is the ONLY shape treated as
      # provisioning.
      ok "zitadel instance domain (still provisioning on ${ZITADEL_HOST_CHECK})"
      ;;
    *)
      # Anything else: a body that is neither a discovery document nor a
      # recognised error. Review finding — this used to share the
      # provisioning branch, which made the default answer of a fail-closed
      # guard "assume it is fine". That is fail-OPEN inside a control whose
      # entire job is to catch one unreadable misconfiguration: a Zitadel
      # that starts answering the not-found case with different wording, or
      # an intercepting proxy returning its own error page, would silently
      # print `ok` and hand the developer back the bare 404 this check
      # exists to translate. Unrecognised now means FAIL, and the message
      # quotes what actually came back so the next reader can decide
      # whether it is a new error shape or a new success shape.
      fail "zitadel instance domain" \
        "Zitadel answered /debug/healthz on '${ZITADEL_HOST_CHECK}' but its OIDC discovery document at ${base}/.well-known/openid-configuration was not recognisable (no \`issuer\` field, and not the 'Instance not found' error either). This check cannot confirm the instance is provisioned for this host, and it refuses to guess — see check_zitadel_instance_domain in this file. Response body was: $(printf '%s' "${body}" | head -c 200)"
      ;;
  esac
}

# The sibling of check_zitadel_instance_domain above: another way Zitadel's
# hosted login goes wrong without failing legibly on its own. zitadel-login
# reads its service-user PAT (ZITADEL_SERVICE_USER_TOKEN_FILE) exactly ONCE,
# at container boot, and caches it for the life of the process. A container
# that starts before Zitadel's first-instance provisioning has written that
# file latches an invalid token forever — verified by experiment, not
# inferred (#923): a live wedged container was found with the PAT ON DISK
# returning HTTP 200 against Zitadel core, while the CONTAINER mounting that
# same file, read-only, reported `Errors.Token.Invalid (AUTH-7fs1e)` HTTP 401
# over 2329 consecutive health-check failures (~19.4 hours — its entire
# life), with AUTH-7fs1e present in every one of the (last five, which is
# all Docker retains) logged probe outputs. A bare `docker restart` (no
# volume change, no file rewrite) took it straight to healthy, which rules
# out a genuinely bad credential and confirms the stale-read latch.
#
# The image already bakes in a HEALTHCHECK for exactly this
# (node /app/healthcheck.mjs /ui/v2/login/ready, 30s interval, 3 retries) —
# a wedged container reports `unhealthy` correctly and continuously. Nothing
# above it used to read that signal: `restart: unless-stopped` does not
# restart a merely-unhealthy container, so it can sit wedged for its entire
# life. Helivanta's own login talks to Zitadel core directly and never
# touches zitadel-login on the common path, which is exactly why this can go
# unnoticed for a day rather than failing immediately.
#
# docker inspect, not curl: unlike check_zitadel_instance_domain, HTTP
# health is not really what changed here — the container's own healthcheck
# already ran the equivalent probe and recorded the verdict, and reading
# that verdict is strictly more accurate than re-deriving it from outside.
#
# The template asks explicitly whether `.State.Health` exists, rather than
# just reading `.State.Health.Status` and swallowing the error. Docker does
# NOT report an empty status for a running container with no healthcheck —
# it errors ('map has no entry for key "Health"'), and a bare `2>/dev/null`
# makes that indistinguishable from "container does not exist". Proven live
# (review round #2 of #923): pointed at a running container with no
# HEALTHCHECK, a running container under the wrong name, and a PATH with no
# docker at all — all three printed `ok ... not running yet` about
# something that was, in two of the three cases, actually up. The entire
# mechanism this check relies on is a HEALTHCHECK baked into a pinned
# upstream image (ghcr.io/zitadel/zitadel-login:v4.15.3) that this repo does
# not control and docker-compose.dev.yml does not itself declare — if that
# pin moves and the probe is dropped, renamed, or restructured upstream,
# swallowing the template error would make this check permanently green,
# reproducing #923's own defect in a new shape: a signal that stopped being
# published, with a check reporting `ok` about it. Explicit no-healthcheck
# handling below closes that.
ZITADEL_LOGIN_CONTAINER=${ZITADEL_LOGIN_CONTAINER:-helivanta-dev-zitadel-login-1}
ZITADEL_LOGIN_HEALTH_FORMAT='{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}'

check_zitadel_login_health() {
  local login_status
  login_status=$(docker inspect --format "$ZITADEL_LOGIN_HEALTH_FORMAT" "$ZITADEL_LOGIN_CONTAINER" 2>/dev/null || true)
  case "$login_status" in
    healthy)
      ok "zitadel-login health"
      ;;
    unhealthy)
      # Not "silently restart it": a container reporting unhealthy is, on
      # THIS specific failure mode, always fixable by 'docker restart' — but
      # this check only knows the SYMPTOM (a cached, now-invalid PAT), not
      # the cause. A genuinely invalid credential (a Zitadel DB reset that
      # dropped the login-client machine user without rewriting
      # login-client.pat, say) would restart, cache the SAME invalid PAT,
      # and go unhealthy again — an automatic restart-loop that never
      # surfaces the real problem, exactly the "silent self-heal that hides
      # a genuinely bad credential forever" #920 already ruled out. Detect
      # and name the remedy; a human decides whether to run it.
      fail "zitadel-login health" \
        "${ZITADEL_LOGIN_CONTAINER}'s Docker health status is unhealthy (a container Docker has stopped also reports this, but 'docker restart' is the correct remedy either way). This is what a login-client PAT read once at container boot and now stale looks like (Errors.Token.Invalid AUTH-7fs1e — verified by experiment in #923, not inferred). Nothing self-heals it: 'restart: unless-stopped' does not restart a merely-unhealthy container, and zitadel-login never re-reads the file. Fix: 'docker restart ${ZITADEL_LOGIN_CONTAINER}'"
      ;;
    starting)
      ok "zitadel-login health (still starting)"
      ;;
    no-healthcheck)
      # See the "template asks explicitly" note above: a RUNNING container
      # with no Health block is not the same as an absent one, and must not
      # be reported the same way. This is the mechanism itself going blind,
      # not a fresh stack — fail loudly rather than silently trusting a
      # container this check can no longer see into.
      fail "zitadel-login health" \
        "${ZITADEL_LOGIN_CONTAINER} is running but reports no Docker HEALTHCHECK at all. This check depends entirely on the HEALTHCHECK baked into ghcr.io/zitadel/zitadel-login:v4.15.3 (node /app/healthcheck.mjs /ui/v2/login/ready) — if the image pin moved or upstream dropped/restructured that probe, this check is now blind to a wedged login the same way #923 found the rest of the stack to be. Confirm with 'docker inspect --format {{.Config.Healthcheck}} ${ZITADEL_LOGIN_CONTAINER}' and update this check (or docker-compose.dev.yml) to match whatever the image publishes now."
      ;;
    *)
      # Empty covers both "container does not exist yet" (a fresh clone, or
      # one that has not reached 'make dev-infra' yet — not a failure) and
      # docker itself being unreachable (check_docker, called above in both
      # main() and run_one(), already reports that). Demanding a live,
      # healthy container here would block the very command that creates
      # one.
      ok "zitadel-login health (not running yet — 'make dev-infra' will start it)"
      ;;
  esac
}

# Nothing here checks a registry token. @tesserix/web moved to the PUBLIC npm
# registry (#866/#868) and the repo-local .npmrc that pinned the scope to
# GitHub Packages is gone, so `pnpm install` on a fresh clone needs no
# credentials at all. A check that demanded one would block onboarding for a
# token nobody needs — the opposite of what this script is for.

# port_is_ours (scripts/lib/repo-owns.sh) needs one specific tool per
# platform — `ss` on Linux, lsof on macOS; see port_tool there for why the
# Linux answer is not "whichever is installed". Without it every port check
# would pass open: a foreign Postgres on 5432 would print "ok port 5432"
# right before compose dies with exactly the cryptic error #714 exists to
# eliminate. Must run before check_ports, and check_ports must not run its
# real logic if this failed.
check_port_tool() {
  local tool fix
  tool=$(port_tool)
  if have_port_tool; then
    ok "$tool"
    return
  fi
  case "$tool" in
    ss) fix="sudo apt-get install -y iproute2 (Debian/Ubuntu)" ;;
    *)  fix="brew install lsof (macOS; present by default)" ;;
  esac
  fail "$tool" \
    "$tool missing — port checks can't detect what holds a port without it on $(uname -s), and this platform must use $tool specifically (see scripts/lib/repo-owns.sh's port_tool) — $fix"
}

# The bind mount Zitadel writes its PAT files into. `make dev-infra` creates
# it 0777 before compose can, because a MISSING bind-mount source is created
# by the Docker daemon as root:0755 — and Zitadel, which runs as its own
# non-root user, then dies with `open /secrets/helivanta-seed.pat: permission
# denied` AFTER it has already pushed the instance-domain event. Every
# restart afterwards reports the consequence instead
# (`Errors.Instance.Domain.AlreadyExists`), forever, on a container that
# never serves a request. Diagnosed from exactly that log on a Linux CI
# runner (#920).
#
# This check exists because that fix lives on ONE path. Any other route to
# `docker compose up` — a developer running compose directly, a future
# script, a rebased branch that loses the mkdir — recreates the root-owned
# directory and reproduces the undiagnosable loop. Enforcement order says
# boot failure beats documented convention: this makes every path either fix
# it or name it.
#
# Absent is NOT a failure: that is a fresh clone, and dev-infra creates it.
# Present-and-ours is not a failure either — dev-infra's chmod can still fix
# the mode. Only a directory this user cannot chmod AND that is not already
# world-writable is unfixable from here, and that is precisely the
# daemon-created case.
HELIVANTA_SECRETS_DIR=${HELIVANTA_SECRETS_DIR:-$REPO_ROOT/dev/zitadel/secrets}
check_zitadel_secrets_dir() {
  local dir=$HELIVANTA_SECRETS_DIR
  if [ ! -e "$dir" ]; then
    ok "zitadel secrets dir (absent — 'make dev-infra' creates it writable)"
  elif [ ! -d "$dir" ]; then
    fail "zitadel secrets dir" \
      "$dir exists but is not a directory — docker-compose.dev.yml bind-mounts it into three containers; remove it"
  elif [ -O "$dir" ]; then
    ok "zitadel secrets dir (owned by you — dev-infra can set the mode)"
  elif [ "$(ls -ld "$dir" | cut -c9)" = w ]; then
    ok "zitadel secrets dir (world-writable)"
  else
    fail "zitadel secrets dir" \
      "$dir is owned by another user and is not world-writable, so the Zitadel container cannot write its PAT files there. This is what the Docker daemon leaves behind when it creates a missing bind-mount source as root. Zitadel does not fail legibly on it: it dies mid-provisioning with 'permission denied' and then crash-loops on 'Errors.Instance.Domain.AlreadyExists' forever. Fix: 'sudo rm -rf $dir' and re-run 'make dev-infra', which recreates it writable"
  fi
}

check_ports() {
  # Without the platform's port tool, port_is_ours answers "not ours" for
  # every port and this would print a wall of failures that all say the same
  # thing. check_port_tool above already recorded the real cause.
  have_port_tool || return 0

  local port holder rc
  for port in $PREFLIGHT_PORTS; do
    if port_is_ours "$port"; then
      ok "port $port"
      continue
    fi
    holder=$(port_holders "$port" 2>/dev/null) && rc=0 || rc=$?
    holder=$(printf '%s\n' "$holder" | head -1)
    if [ "$rc" = 3 ]; then
      # A listener this user cannot name (#927), possibly beside one it can.
      # The old message rendered it as "pid unknown (unknown)", which reads
      # like a bug in this script rather than what it is: another user's
      # process holding the port.
      fail "port $port" \
        "port $port is held by a process this user cannot identify — it belongs to another user (a system Postgres or other root-owned service, typically). $(who_holds_hint "$port") names it. Stop it, or move Helivanta off the port with the matching HELIVANTA_*_PORT in .env"
    elif [ -z "$holder" ]; then
      fail "port $port" \
        "port $port could not be checked: $(port_tool) gave no answer for it, so preflight will not call it free"
    else
      fail "port $port" \
        "port $port held by pid $holder ($(ps -p "$holder" -o comm= 2>/dev/null || echo unknown)) — stop it, or 'make down' if it is a stale Helivanta process"
    fi
  done
}

# who_holds_hint PORT — the root command that names a listener this user
# cannot see, on this platform.
who_holds_hint() {
  case "$(port_tool)" in
    ss) echo "'sudo ss -ltnp \"sport = :$1\"'" ;;
    *)  echo "'sudo lsof -nP -iTCP:$1 -sTCP:LISTEN'" ;;
  esac
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
  check_zitadel_login_health
  check_zitadel_secrets_dir
  check_port_tool
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
#
# check_docker runs first, unconditionally: check_zitadel_login_health's own
# "docker itself being unreachable" fallback message only holds if something
# actually checked that. scripts/e2e.sh and scripts/verify-local.sh both
# call this entry point directly (never through main()), so without this
# call they would see docker being down reported as "not running yet" — the
# same class of false 'ok' #923's review round #2 flagged for the health
# check itself.
run_one() {
  check_docker
  case "$1" in
    zitadel-instance-domain) check_zitadel_instance_domain ;;
    zitadel-login-health) check_zitadel_login_health ;;
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
