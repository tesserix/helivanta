# Employment activation acceptance — 2026-11-01

Status: local non-digital browser acceptance and signed digital API regression.
Operational activation remains incomplete and disabled by default in production.
Tracks #1035, #417 and ADR-0049; neither umbrella issue is closed.

Workforce owns approved sources, immutable source consumption, identity counters
and generation-bound links. IAM owns current employment bases and scoped proposal
bindings. The actor needs separately reviewed activation and enrollment grants
plus a current reviewed Staff/login actor. Activation grants no membership,
login link, business role, card or clinical privilege.

## Local browser evidence

Used `helivanta.localhost:4399`, Global API 8199 and the candidate owner on 8399,
with the existing local synthetic hospital/applicant. All three candidate
migrations were applied locally. Temporary role fixtures were independently
proposed/approved through public IAM APIs, bounded to four hours. Existing
applicant, terms and files came from the earlier normal UI/scanner journey.
Fresh person/employment submissions were explicitly prepared through signed local
API fixtures to capture new identity provenance; do not count that setup as
fully UI-created source acceptance. No production records or grants were changed.

| Browser action                                          | Verified result                                                                         |
| ------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Independent reviewer opens fresh pending person finding | Exact synthetic applicant, policy and scanner-released evidence                         |
| Reviewer records examination, independence and reason   | Approved receipt persisted                                                              |
| Reviewer opens frozen employment terms and approves     | Approved v2 persisted; Staff remained inactive until separate activation                |
| Switch to separately assigned HR preparer role          | Protected approved detail and activation eligibility available                          |
| Submit activation without fields                        | Required reason and examination errors; no activation                                   |
| Supply reason and examination; activate                 | Verified receipt says activation recorded and login/roles remain separate               |
| Owner rereads source                                    | Staff active v2/employment epoch2; approved source consumed and cannot activate         |
| Close/reopen and refresh eligibility                    | Already-recorded message; no second activation control                                  |
| Withdraw only temporary activation assignment; refresh  | Immediate access-changed alert; protected detail, evidence and activation state cleared |

The actor used the existing reviewer sign-in with separately approved HR and
activation assignments; the applicant/source preparer was a different account.
This verifies the separate permissions and source independence without pretending
that changing a UI role creates authority. The non-digital applicant received no
login through this journey. Signed digital API tests separately cover fresh
link/assignment binding and invalidation, but digital-recipient browser admission
remains unaccepted.

Screenshots and raw receipts are private, outside Git, under the existing
`reviewer-ui-acceptance-20261101` evidence directory. Retained images include
`operational-activation-browser-receipt.jpg` and
`operational-activation-browser-cutoff.jpg`. No private file bytes, credentials or
personal identifiers are attached to the public repository.

## Automated checks

- API: `pnpm --filter @helivanta/api exec vitest run --maxWorkers=2` — 548 tests,
  71 files.
- Shell: `pnpm --filter @helivanta/shell exec vitest run --maxWorkers=2` — 1,351
  tests, 167 files. Focused activation/page rerun after lint corrections: 12 tests.
- API/shell type-check and lint; shell production build with local Global API;
  affected Go build/vet and golangci-lint — pass, zero Go lint issues.
- `go test -race -count=1 -timeout=30m -p=1` affected packages: full IAM 365.913s,
  Global API 123.458s pass. Host tenantdb/Workforce runs hit collisions between
  Colima forwarded ports and an unrelated local app; no assertion bypass used.
  Tenantdb VM rerun passes 16.443s; full Workforce VM rerun passes 434.152s.
- Canonical API type-check runs every published generator check. Contractdocs
  generation/check and inventory parity pass. Inventory tooling: 15 tests pass.
  Strict classification correctly refuses activation with 710/727 unclassified
  surfaces and zero complete cutoff acceptances.

The browser UI lint caught an unsupported custom card and ref access during
render; it now uses the shared Panel and error state. Earlier red/green signed
regressions cover missing activation PUT, restored identity laundering, stale
actor authority, UUIDv4 Staff IDs, receipt consumption, audit/outbox rollback,
nullable wire dates and mandatory examination. The full script suite also caught a stale test-only append-template SQL fixture.
It is regenerated from the current canonical catalog; a new regression proves
every current template survives an additive extension. All 75 script tests pass.
Architecture gates caught the guard port living in a data-only contract package,
the missing permission-matrix declaration and a missing singleton-route rationale.
The guard now lives with shared authorization interfaces; the exact permission
and bounded eligibility receipt are recorded without changing the gates.
No skipped assertions count as acceptance.

## Remaining release gates

Keep `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED=false`. Complete deterministic lock
races, digital-recipient/full UI-source acceptance, attributable legacy/governance
cohorts, external-read completion and every suspension/exit/contract-expiry/rehire
consumer cutoff before enabling it. A disabled schema/preparation deployment is
not completed operational activation. Candidate migrations are persisted in the
[release manifest](../deployment/releases/2026-11-01-employment-activation-candidate.json).
