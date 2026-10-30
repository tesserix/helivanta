# Test isolation keys survive `go test -count=N`

**Issue:** [#929](https://github.com/tesserix/helivanta/issues/929)

## The problem, stated precisely

`go test -race -count=3 ./pkg/events/ ./internal/modules/reference/` on
`main` failed nine times across five tests:
- `TestNamespacedBusesDoNotShareSubjects`
- `TestOutboxPublishDispatchConsume`
- `TestHandleMsgDeadLettersAfterMaxDeliver`
- `TestPanickingConsumerDoesNotKillTheProcess`
- `TestPingFullWiring`

The same packages pass when run as separate invocations. Containers are
shared per test binary (deliberately). NATS namespaces and OpenFGA store
names came from `t.Name()`, which `-count` repeats in one process against
the same containers. Iteration 2 consumed iteration 1's events and reused
its stores.

## Decisions

### D1: Make the harness `-count`-safe rather than refuse `-count`

`-count` is the standard tool for proving a flake fixed. Refusing it would
remove the tool. Making the key per-iteration keeps the tool and changes one
name.

### D2: `testinfra.IsolationKey(t)` is the only isolation key

The key is `t.Name()`, then `_i`, then a process-unique sequence number.
- **Within a test:** it is cached per `testing.TB`, so it is stable, and two
  buses or clients in one test that are meant to share state still do.
- **Across iterations:** a new iteration is a new `*testing.T`, so it gets a
  new key.
- **Across processes:** the sequence restarts at 1, but a new process starts
  new containers (`sync.Once` is per process), so nothing can collide.
- **Readability:** the name prefix keeps NATS subjects and OpenFGA store
  names readable.

All 25 call sites change:
- NATS namespaces in `pkg/events`, `iam`'s sign-out tests and
  `testutil.ModuleHarness`;
- OpenFGA stores in `pkg/authz`, `internal/platform` and `internal/archtest`.

### D3: Enforced by archtest, not convention

`TestTestNameIsNeverAnIsolationKey` parses every backend `.go` file and fails
on any `t.Name()`, `tb.Name()` or `b.Name()` call outside
`internal/testinfra/isolation.go`. Today `t.Name()` has no other use, so the
rule is total rather than a list of call shapes to watch.
`TestSourceCallsTestNameDetectsTheForbiddenShapes` pins the detector: it
fires on a namespace argument, a concatenated store name and a benchmark
handle, and not on a module's `Name()` or a field.

### D4: Documented where testing conventions live

`docs/standards/backend.md` §9 explains the shared containers, why
`t.Name()` is not a key, and that `-count` is the supported way to re-run.
`harness.go`'s comment, which claimed `t.Name()` uniqueness without the
`-count` caveat, is corrected.

## Not covered

- Un-sharing the containers. Sharing is deliberate (out of scope per #929).

## Tests

- **End to end.** The same `-race -count=3` command over `pkg/events`,
  `reference` and `pkg/authz` is green, against nine failures on `main`.
  The whole backend, `go test -race -count=2 ./...`, is green too.
- **`IsolationKey`.** It is stable within a test. Two `testing.TB`s
  reporting the same `Name()`, standing in for two iterations, get different
  keys, both prefixed with the name.
- **Mutations, each failing:**
  - reintroducing `t.Name()` in `pkg/events/bus_test.go` (the archtest);
  - a key without the sequence number (the iterations test);
  - no per-test cache (the stability test).
