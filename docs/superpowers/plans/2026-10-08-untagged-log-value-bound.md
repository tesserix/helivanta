# Plan — bound every log value (#943)

Spec: `docs/superpowers/specs/2026-10-08-untagged-log-value-bound-design.md`

- [x] `phisize.go`: `untaggedOversize` (string / error-by-`Error()` /
      `renderBound`), `omitOversize` marker, `LogValueOversizeCount`.
- [x] `phitag.go`: `attrNeedsMask` and `maskAttr` check `KindString` and
      untagged `KindAny` sizes; tagged values keep the #904 path.
- [x] `phitag.go` package doc: the narrowness invariant's size-only exception.
- [x] `untagged_bound_test.go` per spec §Tests, plus
      `BenchmarkLogUntaggedStruct`.
- [x] Benchmark against `main`, recorded in spec D4.
- [x] Mutations: string kind unbounded; error measured by fields; bound too
      tight. Each failed the suite.
- [x] Gates: `make lint-go`, `go test -race ./...`,
      `./scripts/coverage-gate.sh`. Frontend untouched.
