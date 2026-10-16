# Supplier licence review — local acceptance

Issue #1135; epic #1133; parent #979. This is the independent licence-review
slice. Organisation lifecycle, reviewed country/category policy and bilateral
trading remain distinct acceptance. No production migration has been applied.

## Behaviour exercised

A synthetic maker submits a released image for the exact organisation, premises
and licence revisions. An independent scoped compliance reviewer records an
actual authority-check method, reference and reason. The current state rechecks
source revisions, evidence availability, local validity dates and premise state;
an old verified decision cannot authorise a changed or expired source. Every
response keeps `trading_approved=false`. Rejected or suspended cases can be
resubmitted for a new independent decision. Decisions and source history remain
immutable. The SQL guard independently rejects self-review and forged transitions.

API tests use real disposable PostgreSQL and NATS/JetStream with isolated test
identities and third-party GCS/ClamAV protocol fixtures. No real licence, bank,
patient, employee or customer data is copied from legacy. India and Pakistan
fixtures use separate private country/premises/licence sources. Actual government
verification, production OIDC and country transaction activation are not inferred
from the fixtures.

## Actual local browser

The production Next.js build on loopback `127.0.0.1:4312` connects to a separate
loopback API fixture on `:8092`. The temporary fixture and synthetic session
entry are outside the committed app. They exercise the real Supply handlers,
private scopes, PostgreSQL persistence and NATS dispatcher. The pre-released
synthetic image uses the protocol fixtures; this does not claim a new live-cloud
scanner test.

The final rebuilt browser app performed maker submission, maker self-review
exclusion, independent verification and suspension under another synthetic
principal. A full reload preserved Suspended and all three original decisions.
The reusable evidence and verification-method dropdowns and inline required-reference
validation were exercised. A 390x844 viewport had `scrollWidth=clientWidth=390`;
the normal viewport was restored. No browser console errors were recorded in
the final acceptance run.

The disposable database recorded exactly **3 decisions, 3 insert audit entries,
3 outbox entries and 3 published entries**. A foreign-tenant read returned **404**.
These counts came from the running browser fixture's database. Publication requires
the real local JetStream acknowledgement before the outbox is marked published.
[Database proof](evidence/licence-review-2026-10-16/database-proof.json),
[verified UI](evidence/licence-review-2026-10-16/verified.png),
[suspension after reload](evidence/licence-review-2026-10-16/suspended.png) and
[mobile UI](evidence/licence-review-2026-10-16/mobile.png) contain only synthetic records.

## Regressions observed before fixes

- Initial review API returned 404; the first public-API scenario then passed.
- Initial review panel was absent; the rendered heading/state test then passed.
- A receipt for another source revision was accepted despite the same command ID;
  client validation now checks actor, all source versions and review fields.
- The real browser rejected API timestamps carrying local `+11:00` offsets.
  Fixed `+11:00` and `-05:00` JSON fixtures and the HTTP response test failed
  before canonical UTC serialization, then passed without loosening the client.
- A declaration label still said verification pending after a verified decision;
  its regression failed before the neutral Declaration recorded label.
- Initially an unsubmitted expired/future/inactive source just said Not submitted.
  The public-API regressions failed before specific blockers and disabled actions.
- A stale expected subject could record under the current session's user. The API
  now refuses subject drift before touching review records; official clients send
  both subject and tenant preconditions. The UI gives an explicit refresh action.
- An RLS-hidden source author was incorrectly treated as independent: verification
  returned 201 and its action read returned 200. Both regressions were observed
  failing before the fix. SQL now requires all three exact revision authors to be
  known before verification/rejection; the state read fails closed when this
  history is inaccessible. The same tests then passed with 409 and 404 respectively,
  without broadening database privileges.

The first full Go gate correctly rejected missing declarations in the independent
permission matrix, event-field privacy inventory and singleton-GET registry.
Those inventories now list the new controls with their reasons; no assertion or
security boundary was removed. The focused structural checks pass.

## Required commands and final results

The candidate includes main's attendance-timesheet change `7e6923c`. Final combined
checks already passed: Go 1.27.2 `go vet ./...`, `go build ./...`,
`golangci-lint run ./...` (0 issues); sequential `pnpm turbo build`, then
`pnpm turbo lint type-check test format:check` (19 tasks, API174/UI90/shell283 tests).
`GOMAXPROCS=4 GOFLAGS=-p=2 ./scripts/coverage-gate.sh` in `backend/` passed;
it runs `go test -race -count=1 -cover ./...` and enforces every package's
coverage floor. Disposable PostgreSQL/NATS ran through the selected remote
Docker socket, with the testcontainers Ryuk socket override and reconnect timeout.

`make lint-shell` could not bind-mount this macOS worktree into the remote Docker
daemon. The same script's discovered 11 tracked files and `.shellcheckrc` were
copied into the exact digest-pinned Shellcheck image, then checked from `/mnt`;
exit 0. No shellcheck findings were suppressed. Supply contract regeneration and
`git diff --check` also completed without contract drift or whitespace findings.

The first web run lacked required public OIDC build settings. A concurrent
build/typecheck then encountered partial generated Next route types. Sequential
build followed by the other checks passed; no type or environment assertion was
suppressed. The configured public issuer and synthetic client ID are build/test
settings, not a claim of real OIDC acceptance.

## Release and recovery

`0004_supply` adds empty review tables, forced RLS, immutable history and insert
guards with 5s lock and 60s statement timeouts. Existing evidence/declaration
migrations are unchanged. Keep data and repair forward; destructive down migration
is unsupported. Rolling the app back retains review history but removes its new
UI/API. A specific production migration/application rollout approval is required
once the candidate and hosted checks are concrete.

[Workflow preview](diagrams/14-supply-licence-review.svg) ·
[Editable draw.io diagram](diagrams/14-supply-licence-review.drawio).
