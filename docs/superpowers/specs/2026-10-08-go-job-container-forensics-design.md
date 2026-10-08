# The go job records who removed a test container

**Issue:** [#963](https://github.com/tesserix/helivanta/issues/963). This
change covers the issue's diagnosability step; it does not close the issue.

## The problem

Twice on CI, the shared Postgres container of the `internal/modules/iam` test
binary has vanished mid-run, both times at the same point in the test
sequence (`hms_test_45`). Every remaining test then fails with
`No such container`.

It does not reproduce locally on a machine the same size as the runner (4
CPUs, 15.7 GiB). A full `coverage-gate.sh` run there peaked at about 3.6 GiB
used with 26 containers, with no container death and no OOM kill. Nothing in
the test code stops containers; the only remover in the system is
testcontainers' reaper, Ryuk.

The test log can say only that the container is gone. It cannot say who
removed it, so any fix chosen today would be a guess. Disabling Ryuk, or
limiting parallelism, would each "fix" one hypothesis and hide the other.

## Decisions

### D1: Record Docker's event stream for the whole test step

A step before `coverage-gate.sh` starts
`docker events --format '{{json .}}'` in the background, writing to
`$RUNNER_TEMP`. Every kill, die, OOM and destroy during the tests is then on
record, with timestamps, signal and exit code.

### D2: On failure, print the evidence that tells the causes apart

The dump steps use the e2e job's `!success() || cancelled()` condition. They
print:

- every container `kill`/`die`/`oom`/`destroy` event;
- `docker ps -a`, plus `State.OOMKilled`/`ExitCode` for each exited container;
- the kernel log's OOM lines.

The candidate causes leave different traces:

| Cause | Evidence |
|---|---|
| Ryuk pruning the session | `kill`/`die`/`destroy` across many containers in the same second; no OOM line |
| Kernel OOM killer | one `die` with exitCode 137, `OOMKilled=true`, and an "Out of memory: Killed process" line |
| Something else | whatever the events show; at minimum, the exact second and the actor |

An empty result prints an explicit line, such as "no container died" or "no
OOM kill in the kernel log". These are findings, so the step does not use
`|| true`.

### D3: The full event log is uploaded

It is uploaded as an artifact (`go-docker-events`, kept 7 days) for anything
the summary does not print.

### D4: Ryuk's own log is followed for the whole test step

The first real occurrence after D1 landed (run 37727687289, on this PR's
own head) settled the first question:

- Every container of the session (Postgres, OpenFGA and NATS, from every
  package) got `kill` (signal 9), `die` (137) and `destroy` within 04:33:45–47.
- No container died before that burst.
- The kernel log had no OOM line.

That is Ryuk's session prune, and the iam binary was still running when it
happened (its first failure is the test that was in flight). Ryuk prunes
only when its prune check fires. That check is armed when its client count
reaches zero and lasts `RYUK_RECONNECTION_TIMEOUT` (10s). So the live iam
binary held no connection to Ryuk for those ten seconds. A local
`coverage-gate.sh` run with Ryuk's log followed never let the count reach
zero while iam ran, so the cause is not in the events.

Ryuk logs every `client connected` and `client disconnected` with the count,
and every `prune check`. Ryuk removes itself after a prune, so its log is
gone by the time a failure step runs. A background loop therefore follows
every `reaper_*` container from the moment it appears, writing to
`$RUNNER_TEMP/ryuk-<id>.log`. On failure the log is printed, or the explicit
line "no reaper container was seen", and it is uploaded with the events.

## Not covered

- The fix itself. It waits for the evidence; #963 stays open.

## Verification

- **Locally.** The workflow's own `run:` blocks were extracted and run against
  a container killed with SIGKILL. They printed its `kill` (signal 9) and
  `die` (exit 137) events, `OOMKilled=false ExitCode=137`, and "no OOM kill in
  the kernel log".
- **On CI.** A temporary commit made the go job fail deliberately after
  killing a canary container. The dumps were observed in that run, and the
  commit was then reverted. The run is linked in the PR.
- **Ryuk follower, locally.** The workflow's `run:` blocks were run as they
  are. With no reaper, the dump printed "no reaper container was seen". A
  container named `reaper_canary963` was then started, and its timestamped
  output was captured and printed by the dump.
