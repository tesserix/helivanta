# A listener nobody can attribute is not a free port

**Issue:** [#927](https://github.com/tesserix/helivanta/issues/927)
**Amends:** the port-ownership helpers from #714 and #920
(`scripts/lib/repo-owns.sh`).

## The problem, stated precisely

`port_holders PORT` prints the pids listening on a port. Every caller reads an
empty answer as "nothing is listening". Two situations print nothing:

1. nothing is listening;
2. something is listening, but this user cannot see which process it is.

On Linux, a non-root `ss -ltnp` prints the socket row for another user's
listener but leaves out its `users:((…pid=…))` field. On macOS, a non-root
`lsof` does not list another user's processes at all. In both cases
`port_holders` prints nothing, `port_is_ours` answers "free", and
`preflight.sh` prints `ok port 5432` for a root-owned Postgres squatting on
the port. Compose then dies with exactly the cryptic bind error #714 exists to
prevent.

That is a fail-open inside a control whose whole purpose is to fail closed.

## Decisions

### D1: `port_holders` reports an unattributable listener as exit status 3

stdout keeps its contract: pids, one per line. The exit status carries what
stdout cannot:

| Status | Meaning |
|---|---|
| 0 | every listener on the port was attributed, including when there are none |
| 2 | no answer: the platform's tool is missing or failed (#920) |
| 3 | at least one listener on the port could not be attributed to a pid |

A new status, rather than a placeholder pid such as `?` in stdout. Every
caller passes stdout straight to `kill`, `ps -p` or `pid_is_ours`, and a
non-pid there is a type error that fails in whichever way each tool happens
to fail. A status code is an answer a caller has to handle on purpose.

Status 3 is not exclusive with output. On a port where this user holds one
listener and another user holds a second (IPv4 and IPv6, say), the pid that
can be seen is printed, and the status is still 3.

A failing `ss` is now status 2. Its stderr and exit status used to be
discarded, which made a crashed `ss` read as a free port: the same fail-open,
by another route.

### D2: How each platform detects an unattributable listener

- **Linux (`ss`).** Any row of `ss -H -ltnp "sport = :PORT"` that has no
  `pid=` is a listener that cannot be attributed. This is measured, not
  inferred. The CI case in Tests starts a root-owned listener and reads it as
  the non-root runner user.
- **macOS (`lsof` + `netstat`).** `lsof` cannot make the distinction, because
  it shows a non-root user only that user's own processes. `netstat -an -p tcp`
  lists every listening socket on the machine, with no owner, and ships with
  macOS. If `netstat` shows a `LISTEN` socket on the port and `lsof` attributes
  none, the status is 3. A missing `netstat` is status 2, the same as a
  missing `lsof`.
- **macOS residual.** If this user holds a listener on the port, `lsof` names
  it. A second, foreign listener on the same port, which BSD permits on a more
  specific address, is then not reported as status 3. `pid_is_ours` still
  judges the visible pid, and the foreign listener goes unnamed. This is
  stated rather than solved because counting sockets across `lsof` and
  `netstat` would rely on output fields that are not verifiable from CI, and
  nothing in this repo binds that way.

### D3: What each caller does with status 3

- **`port_is_ours`** answers "not ours", unless our compose project publishes
  the port. That exception matters. A container's published port is held by
  the Docker daemon's `docker-proxy`, which runs as root, so on a non-root
  Linux shell our own Postgres is itself an unattributable row. Without the
  compose check, a re-run of `make up` would refuse a stack that is already
  healthy, which `repo-owns.sh` names as the other worst bug.
- **`preflight.sh check_ports`** fails the port with its own message. The old
  message named a pid, which here is "unknown (unknown)". The new one says the
  holder belongs to another user, gives the root command that will name it on
  this platform, and points at the `HELIVANTA_*_PORT` override.
- **`e2e.sh stop_port_if_ours`** refuses, as it already does for a foreign
  pid. Its wait loop uses the new `port_free`, so a listener it cannot see
  into is never read as "stopped".
- **`dev-down.sh`** reports a port with an unattributable listener as
  `SKIPPED`, alongside foreign pids, and counts it in "Still listening". It
  never reports "All Helivanta ports are free" while that listener exists.

### D4: `port_free PORT`, one definition of "free"

`port_free` is true only when the status is 0 and no pid was printed. The
callers that asked "is anything still here?" by testing for empty output now
call this instead: `e2e.sh`'s wait loop and `dev-down.sh`'s final sweep. Both
used to read status 3, and status 2, as free.

`e2e.sh restore_web` keeps its existing test. It decides whether to restart
the zone apps, and if a foreign listener holds the shell port, attempting the
restart is what fails loudly. Skipping the restart would report teardown done
over a port that is not ours.

## Not covered

- The `ss`/`lsof` platform selection itself (#920).
- The macOS residual in D2.

## Tests (`scripts/preflight.test.sh`)

- **Stubbed `ss`.** The row shape measured on CI, without `users:((…))`,
  gives:
  - `port_holders` status 3 with empty stdout;
  - `port_is_ours` false;
  - `port_is_ours` true when compose publishes the port (the `docker-proxy`
    case);
  - `port_free` false;
  - a row with `pid=` and a row without on one port gives the pid and status 3;
  - a failing `ss` gives status 2.
- **Stubbed `uname`/`lsof`/`netstat` for Darwin.**
  - a `netstat` `LISTEN` row on the port with no `lsof` pid gives status 3;
  - a `netstat` row on a different port with the same suffix (`:15432` vs
    `:5432`) does not;
  - no `netstat` gives status 2.
  These are checked against recorded `netstat` output shape, not on a Mac.
- **Real foreign listener.** A listener is started as root through
  `sudo -n`, and the suite, running non-root, asserts:
  - `port_holders` status 3;
  - `port_is_ours` false;
  - preflight exits 1 and names the cannot-identify cause.
  This proves the premise, that `ss` omits the field, on the CI runner. CI sets
  `PREFLIGHT_TEST_REQUIRE_FOREIGN=1`, so the case fails rather than reporting
  "n/a" if it cannot run there. On a laptop without passwordless sudo it says
  "n/a" out loud.
- **Mutation.** Each of these is reverted in turn, and the suite must fail
  each time:
  - the status-3 branch;
  - the compose exception;
  - `port_free`'s status check.
