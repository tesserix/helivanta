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

# Pure predicate: accepts only the exact typed word "reset". No TTY, no I/O,
# no globals — kept separate from confirm_reset so the test harness can
# exercise the one line that actually gates an unrecoverable
# `docker compose down -v` without needing a pseudo-terminal.
reset_reply_accepts() {
  [ "$1" = "reset" ]
}

confirm_reset() {
  if [ "${RESET_YES:-}" = "1" ]; then
    return 0
  fi
  if [ ! -t 0 ]; then
    echo "reset needs confirmation but stdin is not a terminal." >&2
    echo "Re-run with RESET_YES=1 to proceed non-interactively." >&2
    return 1
  fi
  echo "This destroys the local Postgres and NATS volumes (pgdata, natsdata)."
  echo "Anything you entered by hand is lost. Seeded data is recreated."
  printf 'Type "reset" to continue: '
  read -r reply
  if ! reset_reply_accepts "$reply"; then
    echo "Aborted — nothing was changed."
    return 1
  fi
  return 0
}

main() {
  confirm_reset || exit 1

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
}

# Guarded so the test harness can source this file and call
# reset_reply_accepts directly without running the whole script.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
  exit $?
fi
