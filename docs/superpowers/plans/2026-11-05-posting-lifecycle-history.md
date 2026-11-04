# Retained posting lifecycle history implementation

Status: local candidate in progress, branch
`codex/posting-lifecycle-history-20261105`, based on UI draft #1577 at `d80b6eaf`.
All five reusable Staff requirements and the parent delivery tracker remain open.

## Implemented behavior and ownership

See [ADR-0059](../../adr/0059-retained-posting-lifecycle-history.md) and the
[canonical contract](../../api/workforce-postings.md). The existing Workforce
context route accepts an exclusive recorded-history selector. It preserves old
wire envelopes and uses current independent review/Staff actor authority and
purpose RLS to disclose immutable0085 evidence. The selected approved revision,
original observed cutoff versions, full bounded source count and original
provenance remain attributable after later cutoffs. Missing proof is explicit
null; incomplete source visibility and audit failure withhold the entire view.
No schema, permission, event, migration, dependency or deployment cost is added.

The canonical JSON generates both Go and Zod. A dedicated SDK verifies exact
snapshot, source ordering/count/uniqueness, original selected absence, supervisor
pin, support policy, deadline bounds and supported calendar, with abort checks.
The UI routes approved selections to a separate historical reader, reusing the
same proposal-rationale component. It labels original evidence explicitly and
withholds cached private data during failed/pending access rechecks. Existing
pending approval/recovery remains on its current lifecycle path.

## TDD and validation evidence

- Selector test first fails on the intended recorded request, then all relevant
  context-selector tests pass1.242s.
- Real Global API test first fails98.67s with409
  `posting_supervision_required` for the approved recorded-history selector.
  Root/chain history then passes110.378s; expanded audit/incomplete-source/
  contributor/sibling/object/stale and older-null coverage passes117.648s.
  Exact retained history remains unchanged after the ancestor ending.
- SDK first fails for the missing history method, then its proposal suite passes.
  A year0000 examination test exposes a real acceptance gap and fails before the
  explicit calendar guard; all93 proposal tests then pass. Full SDK passes
  **737 tests /81 files**.
- UI first fails because the retained-history region is absent. The expiry view
  passes after implementation. The first expanded run passes58 and fails only a
  test-harness rerender that removed its query provider. Correcting the harness
  to switch account through component state preserves abort/privacy assertions;
  all **59 reviewer tests /12.04s** pass. All scoped API/UI/shell web checks pass
  **9 tasks /19.994s**. The microsecond display regression then fails for
  truncation and passes after a shared UTC formatter preserves all six fractional
  digits. Final focused reviewer coverage is **60 /17.83s** and scoped web checks
  pass **9 tasks /31.343s**.
- Initial scoped Go lint catches an unchecked row-close error and two unchanged
  baseline `sqlclosecheck` findings in `posting_supervisor_choices_plan_test.go`.
  The history reader now propagates deferred close errors. Lint with
  `--new-from-rev d80b6eaf` passes0 issues. CI later exposes those same two baseline cursor-cleanup findings. The
  plan test now defers checked cleanup and drains the single EXPLAIN result
  before the next query. The 100,000-row actual plan regression passes
  **5.793s** and unfiltered repository-wide `golangci-lint run ./...` passes
  **0 issues**. No suppression or test skip is added.
- Final scoped owner/API checks pass **136.702s**, including the corrected
  deferred row-close handling and EXPLAIN(ANALYZE,BUFFERS) probes for both indexed
  history queries under the actual small-fixture RLS frame. Sample header/source
  reads take0.025/0.048ms; this does not establish production latency or p99.
- Native Go vet/build and contractdocs generation/check pass. Inventory generation/
  check passes, with no existing classifications promoted. Source pins require
  no updates for the modified handler/generated file.
- The initial full Workforce race run passes518.238s. It predates the final
  row-close error propagation; final scoped and full-source runs are being
  recorded separately. Broad Global API then passes **539.115s**; that run also predates the final
  row-close correction. Exact final-source architecture/Global API and Workforce
  runs remain in progress. The initial production build passes; final display
  verification and full shell evidence are being recorded separately.

Commands actually run include:

```sh
node scripts/generate-workforce-postings.mjs
node scripts/generate-workforce-postings.mjs --check
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/api exec vitest run src/staff-posting-proposals.test.ts
pnpm --filter @helivanta/shell exec vitest run 'app/(workspace)/workforce/posting-review-workspace.test.tsx'
pnpm --filter @helivanta/shell test
pnpm turbo lint type-check format:check --filter=@helivanta/api --filter=@helivanta/ui --filter=@helivanta/shell
node scripts/staff-operational-access-inventory.mjs
node scripts/staff-operational-access-inventory.mjs --check
```

Backend commands use cached local tooling, GOPROXY/GOSUMDB off and bounded workers.
`go vet ./internal/modules/workforce ./internal/globalapi`,
`go build ./cmd/api ./cmd/global-api`, and
`golangci-lint run --new-from-rev d80b6eaf ./internal/modules/workforce/... ./internal/globalapi/...`
were run from backend. `go run ./cmd/contractdocs` and its `-check` mode pass.
The focused API race script is
`/tmp/helivanta-posting-lifecycle-history-api-focused-20261105.sh`; exact logs use
`/tmp/helivanta-posting-lifecycle-history-*20261105.*`.

## Local environment and remaining gates

The original local UI1846565c was copied into a21MiB immutable cached standalone
artifact before further builds. The local history candidate now runs UI6d541321
→ Global/owner e1fd098f on loopback4398 →8198 →8398. Health/login return200;
anonymous Staff, cutoff and recorded-history requests through the UI return401.
Runtime hashes, prior processes and immutable artifacts are retained privately
under the existing fixture directory. These are HTTP probes, not UI acceptance. The local database ledger237
and its prior230-entry backup are unchanged by this increment. The final build
uses the fixture's existing registered public sign-in client and Global API
origin, without installs, downloads or credential changes.

Explicit bounded browser evaluation still fails through the connection; DOM,
screenshot and navigation are unavailable. These unit/HTTP results are not
normal-role admin/reviewer UI acceptance. No merge, production rollout/migration
or release tag is claimed. Actual department/Pharma operating guards, credential/
unit specialization, standalone-employer decisions and all five reusable Staff
requirements remain open. Counts remain10 partial,10 awaiting audit,1 research,
0 fully accepted.

## Published owner increment and UI follow-up

Owner/API/SDK source is committed as `e1fd098f` in draft
[PR#1578](https://github.com/tesserix/helivanta/pull/1578), stacked on #1577.
The following UI source is on `codex/posting-lifecycle-history-ui-20261105`.
Approved proposals use a distinct history query, preserve rationale and display
source/reviewer references in expandable details. Pending approval/replay remains
unchanged. Access rechecks hide retained evidence while pending or unavailable;
403 clears it and account switches abort in-flight responses.

The precise-time UI regression fails when JavaScript Date drops microseconds.
A shared UTC formatter now preserves the owner's six-digit fractional precision
in history, current lifecycle review and proposal interval display. Final focused
reviewer tests pass **60 /17.83s** and scoped API/UI/shell web checks pass
**9 /31.343s**. The final production Webpack build, using the existing fixture
sign-in client and Global API8198, passes. The earlier complete shell run passes
**1,553 /183 files /377.54s**, before the added precise-time test; the final
complete shell run is still live and no final result is inferred from that
previous run. Final architecture passes **84.921s**. Exact final full Workforce
and Global API runs remain live.

The existing UI1846565c remains served from the cached immutable artifact;
neither candidate activation nor normal-role browser acceptance is claimed yet.
All five parent requirements and the owner/consumer/standalone release gates
remain open. No production actions occurred.
