# Plan: enforce Go formatting (#919)

Spec: `docs/superpowers/specs/2026-10-08-gofmt-enforced-design.md`

- [x] `.golangci.yml`: `formatters: enable: [gofmt]` and
  `issues.max-same-issues: 0`.
- [x] `gofmt -w` on the four unformatted files. The diff is whitespace only.
- [x] Docs: `docs/standards/backend.md` §9 and `CLAUDE.md` state that lint-go
  includes `gofmt`.
- [x] Proof:
  - lint lists all four files, then reports 0 issues after formatting;
  - a re-misformatted file fails it.
- [x] Gates: `make lint-go`, `go build ./...`. The changed test files are
  whitespace only. `go test` on `pkg/events` and `pkg/pagination` passes.
