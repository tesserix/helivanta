#!/usr/bin/env bash
# Stops the whole local stack: docker infra AND the app processes.
#
# `docker compose down` alone is not enough. `make up` also starts the Go
# API and four `next dev` servers as ordinary host processes, and those
# survive a compose teardown — leaving five orphans holding ports
# 4301-4304 and 8080, pointed at a database that no longer exists. The
# next `make up` then fails in a confusing way ("port already in use", or
# an app that answers but cannot reach Postgres).
#
# Killing whatever happens to hold those ports would be reckless: 8080 in
# particular is a popular port, and a developer may well have an unrelated
# service on it. So every candidate is checked first — we only kill a
# process whose working directory is inside this repository.
set -uo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
APP_PORTS=(4301 4302 4303 4304 8080)

echo "Stopping infrastructure…"
docker compose -f "$REPO_ROOT/docker-compose.dev.yml" down

# cwd_of prints a PID's working directory, or nothing if it cannot be read.
cwd_of() {
  lsof -a -p "$1" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1
}

echo "Stopping app processes…"
killed=0
skipped=0
for port in "${APP_PORTS[@]}"; do
  for pid in $(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null); do
    cwd=$(cwd_of "$pid")
    case "$cwd" in
      "$REPO_ROOT"|"$REPO_ROOT"/*)
        kill "$pid" 2>/dev/null && killed=$((killed + 1))
        printf '  stopped  pid %-7s port %s\n' "$pid" "$port"
        ;;
      *)
        skipped=$((skipped + 1))
        printf '  SKIPPED  pid %-7s port %s — not this repo (cwd: %s)\n' \
          "$pid" "$port" "${cwd:-unknown}"
        ;;
    esac
  done
done

# Give them a moment to exit cleanly, then escalate only for our own.
if [ "$killed" -gt 0 ]; then
  sleep 3
  for port in "${APP_PORTS[@]}"; do
    for pid in $(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null); do
      cwd=$(cwd_of "$pid")
      case "$cwd" in
        "$REPO_ROOT"|"$REPO_ROOT"/*)
          kill -9 "$pid" 2>/dev/null
          printf '  forced   pid %-7s port %s\n' "$pid" "$port"
          ;;
      esac
    done
  done
fi

still_up=()
for port in "${APP_PORTS[@]}"; do
  lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1 && still_up+=("$port")
done

echo
if [ "${#still_up[@]}" -eq 0 ]; then
  echo "All HMS ports are free."
else
  echo "Still listening: ${still_up[*]}"
  [ "$skipped" -gt 0 ] && echo "(left alone because they do not belong to this repo — see SKIPPED above)"
fi
