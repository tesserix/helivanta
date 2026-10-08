# Go formatting is part of the lint gate

**Issue:** [#919](https://github.com/tesserix/helivanta/issues/919)

## The problem

`.golangci.yml` enabled no formatter, so `make lint-go` and the CI
golangci-lint step both reported "0 issues" on files `gofmt` would change.
When this change started, four files on `main` were unformatted:

- three test files the issue names;
- `loginclient/client.go`, made unformatted by #961 the same day, which proves
  the point: every documented gate passed it.

## Decisions

### D1: `gofmt`, enabled as a golangci-lint v2 formatter

`.golangci.yml` gains `formatters: enable: [gofmt]`. `golangci-lint run`
reports any file a formatter would change. That is the command `make lint-go`
runs, and also what CI's `golangci/golangci-lint-action` runs. One
configuration therefore covers both, with no second command to remember or
document.

`gofmt` rather than `gofumpt`: it is the toolchain's own formatter and the
style every file was written to, so enabling it restyles nothing. `gofumpt`'s
stricter rules would reformat code across the tree for no correctness gain,
which the issue puts out of scope.

### D2: Every unformatted file is listed

golangci-lint shows at most three issues with the same text by default, and
every formatting issue has the same text. `issues.max-same-issues: 0` lifts
the cap, so a run names every file to fix. Without it, the run that started
this change listed three of the four.

### D3: The four files are formatted by the tool in the same change

`gofmt -w` on exactly those files, with no manual edits. Their diff is
whitespace only.

### D4: The documented gate says what it covers

`docs/standards/backend.md` §9 and `CLAUDE.md` now state that `make lint-go`
includes `gofmt` and how to fix a finding.

## Not covered

- `goimports` import grouping. It is not requested, and it is not a
  formatting gap the issue observed.

## Verification

- `make lint-go` lists all four files before the fix and reports 0 issues
  after it.
- Re-misformatting a file (an extra space in a `func` line) makes it fail
  again.
