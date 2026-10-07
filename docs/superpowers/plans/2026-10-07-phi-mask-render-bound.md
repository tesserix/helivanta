# Plan — bound PHI masking before marshalling (#904)

Spec: `docs/superpowers/specs/2026-10-07-phi-mask-render-bound-design.md`

## Task 1 — the bound

- [x] `pkg/logging/phisize.go`: `renderBound`, `stringCost`, cycle detection
      on the current path, self-rendering types asked via their marshaller.
- [x] `oversizeMarker`, `PHIOversizeCount`.

## Task 2 — wiring

- [x] `maskPHIValue` calls `renderBound` before `marshalForMask`; the
      post-marshal length check stays as the backstop and uses the oversize
      marker.
- [x] `phitag.go`'s package and constant comments corrected (the "linearly"
      claim).

## Task 3 — tests

- [x] Replace the wall-clock DAG test with byte assertions at depth 40.
- [x] Internal tests per spec D5.
- [x] Mutations: escapes as 1 byte; self-rendering opaque (value and pointer
      receiver); limit ×1000; pre-flight disabled. All four fail the suite.
- [x] Benchmark: `BenchmarkMaskLargeSlice` unchanged (9.4 ms vs 9.9 ms on
      main); `BenchmarkMaskSharedReferenceDAG` (depth 20) is about 17 ms and
      1.7 KB per call, down from 792 ms and 142 MB on main.

## Task 4 — follow-up

- [x] File the untagged-value bound as its own issue (spec, "Not covered"):
      #943.

## Gates

- `make lint-go`, `go test -race ./...`, `./scripts/coverage-gate.sh`.
