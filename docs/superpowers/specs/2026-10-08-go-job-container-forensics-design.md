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

### D5: testcontainers' own log is on in every test binary

The second real occurrence (run 37731855071, on #966) carried Ryuk's log.
- 22 clients connected over the run, and none lived past 05:23:54, when the
  last other package (`pkg/events`) exited.
- Ten seconds later Ryuk pruned, removing 24 containers. iam was still
  running.
- Grouped by the time they closed, every connection matches another package.
  None matches iam, whose only container at that point was its Postgres.

Two causes were tested locally and excluded:
- **GC finalizing a dropped container's reaper connection:** a dropped
  `tcnats.Run` handle under 20 forced GCs kept its connection.
- **iam not connecting at all locally:** run alone, iam holds its connection
  from its first container until it exits.

What iam's process did about the reaper on the runner is therefore the
missing record. testcontainers logs it ("Creating container for image
testcontainers/ryuk", "Reaper obtained from Docker", each container's
create and start), but its default logger is a no-op unless the binary runs
with `-v`, and CI does not.

`internal/testinfra` now installs a stderr logger for testcontainers in
`init`, prefixed `testcontainers[pid=N ppid=M]` with microsecond timestamps,
so the lines line up with Ryuk's. `go test` prints a package binary's output
only when that package fails, so a green run prints nothing more. A failing
iam prints its whole reaper history.
`TestTestcontainersLogIsOnWithoutVerbose` pins it, and fails with the init
removed.

## Root cause and fix

The third occurrence (run 37735082190, on #968) was the first with D5's
log, and it named the cause in four lines from the iam binary:

```
testcontainers[pid=16534 ppid=6614] 05:59:57.195 🐳 Creating container for image testcontainers/ryuk:0.14.0
testcontainers[pid=16534 ppid=6614] 05:59:57.341 ⏳ Waiting for Reaper "86ea912d" to be ready
testcontainers[pid=16534 ppid=6614] 05:59:57.358 🔥 Reaper obtained from Docker for this test session 86ea912d
testcontainers[pid=16534 ppid=6614] 05:59:57.362 Reaper handshake failed: read ack: EOF
```

iam raced another package binary to create the session's Ryuk, lost, and
reused the winner's. In testcontainers-go v0.43.0 the reuse path
(`reaperSpawner.fromContainer`) waited only for Docker's port proxy to
accept, not for Ryuk to be listening. So the handshake read EOF.
`Reaper.connect` only logs that failure and hands back the connection's
termination channel as if connected. Ryuk never counted iam as a client,
and pruned the session 10s after every other binary had exited, with iam
still running. That is every observation above:
- the prune burst;
- no connection attributable to iam;
- 24 containers removed against 22 connections;
- the same point in the iam sequence each time, since iam is the longest
  binary.

**Fix: testcontainers-go v0.44.0**, whose reuse path also waits for Ryuk's
`Started` log line, as the create path already did. Measured with a
harness of six processes started at once under one parent: v0.43.0 failed
the handshake in every reusing process (30 of 30 over six trials), and
v0.44.0 in none (0 of 30).

**Regression test: `internal/testinfra`'s
`TestConcurrentBinariesAllConnectToTheReaper`.** It re-executes its own
binary four times concurrently, so the four share one parent and one Ryuk
session, and each starts a container. It fails if any copy logs a failed
handshake. It also fails if no copy took the reuse path, so the test cannot
pass without exercising the race. It fails 3 of 3 runs with go.mod pinned
back to v0.43.0, and passes 5 of 5 on v0.44.0.

## Not covered

- The handshake failure is still only logged by testcontainers (v0.44.0 does not return it). With the reuse path now waiting for `Started` there is no known path to it, and if one appears, the regression test and D5's log both surface it.

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
