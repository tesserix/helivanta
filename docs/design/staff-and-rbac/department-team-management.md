# Department team management

Implementation and validation record, reviewed 2026-11-06 against the accepted owner
boundaries and base `f4cb4838`, synchronized with main `86bff1a6`. Scope: hospital department assignments, reviewed
heads/supervisors, current team observations, endings and coordinated scheduled
transfers. Contract/ADR review and production release remain separate.

## Ownership and team tree

```text
Hospital establishment — employer and hospital IAM scope
├── Canonical Staff employment and account link [Workforce]
├── Department tree and current structure revisions [Organization]
│   ├── Department A
│   │   ├── Reviewed managerial posting / department lead
│   │   ├── Reviewed department supervisors
│   │   └── Reviewed department members
│   └── Department B
│       ├── Reviewed managerial posting / department lead
│       └── Reviewed supervisors and members
├── Scoped role assignments and independent grant review [IAM]
└── Product-owned operating / clinical decisions [each product owner]
```

A posting belongs to canonical Staff, its employing hospital, exact department
revision and reviewed employment generation. It declares a responsibility and
supervisor sources; it is not an IAM role assignment. The accepted kinds are
`hospital_management`, `department_supervisor`, `department_member`. A member
cannot supervise. A head/lead is a reviewed managerial responsibility, not a bare
`head_staff_id`. Unique or statutory HOD designation requires an explicit policy;
the implementation does not infer professional credentials or clinical privileges.

Organization owns current descendants. Department grants cover only their current
subtree through the owner guard, never copied IAM ancestry. Hospital administration,
a title, Staff employment or placement alone grants no business/clinical access.

Independent PharmaConnect establishments, their own Staff and branch scope, and
hospital/supplier collaboration grants remain defined in the
[overall ownership design](README.md) and [collaboration scenarios](partner-collaboration.md).
This hospital implementation does not manufacture hospital Staff for a supplier or
implement a cross-employer transfer.

## Staff and team journeys

1. **Prepare:** choose an authorized employing hospital, canonical Staff, exact
   current employment and department revision. Prepare the interval and rationale.
   Preparation grants no access.
2. **Submit:** declare the managerial/member responsibility and exact eligible
   supervisor reference, or explicitly request a hospital management root.
3. **Review:** an independent eligible reviewer examines the retained source and
   live employment, department, supervisor chain and cutoff observations. Root
   management requires its additional reviewed capability. Reject/withdraw retain
   history and contributor rules.
4. **Observe teams:** inspect a hospital or held current department subtree. Each
   bounded page lists up to eight approved placements with a live `current` or
   `not_current` observation and check time. Private HR reasons/pay/documents are
   omitted. Old approval receipts never become active-team authority by themselves.
5. **End:** immediate or scheduled monotonic cutoffs stop future source use while
   preserving audit and history, including inherited effects on descendants.
6. **Transfer:** prepare/submit the future destination placement, then review it
   with the original responsibility in the paired command below.
7. **Grant operating access separately:** use independently reviewed IAM assignments
   and each product's current source checks. Placement approval grants none.

## Current roster contract

`GET /workforce/hospitals/{hospital}/department-team` enters through the Global API.
It composes proposal-read, current Staff actor and separately held hospital
source-use authority. The hospital source gate is acquired before department/actor locks while retaining
the strict read-only disclosure context. A department hint narrows the current subtree; it cannot
expand the held grant. Internal ancestor verification saves/restores the caller's
department frame and releases only the protected current-source observation.

The projection includes Staff ID/code/name, exact posting/revision/version,
department reference, declared responsibility, supervisor identifiers and nominal
interval. `current` also requires the current employment generation, reviewed
account link, source proof, active Organization revisions and every ancestor
cutoff. Unavailable authority fails the page; it is not an empty-team success.

Pagination retains explicit non-current rows rather than silently filtering a scan
page to empty. The UI shows continuation, distinguishes observations from retained
non-current placements and labels expired observations separately from confirmed non-current placements. Every
operating command must authorize again. Sensitive reads are audited.

## Atomic scheduled transfer

The first slice is within one tenant/hospital/canonical Staff. The pending
replacement already fixes an explicit future start and exact reviewed lifecycle.
The server must not silently replace a past draft start with a guessed instant.

The separate `PUT .../posting-proposals/{replacement}/transfers/{command}` pins
original posting/revision/version/cutoff version and the exact replacement decision.
It holds five independently checked reservations: review, Staff actor, cutoff,
source use and either posting-read lifecycle support or root management review.
A department-only reviewer cannot combine narrower grants into hospital-wide
transfer authority.

Under one Workforce transaction and existing hospital supervision gate:

- Reauthorize and recover an existing exact paired receipt before rechecking the
  original source. Changed intent conflicts.
- Verify the independent reviewer, pending replacement, exact source observations
  and current original appointment through the future start.
- Reject a replacement chain that depends on the ended original, and reject a
  prior paired transfer already consuming that original.
- Write the original scheduled cutoff exactly at the replacement start, replacement
  approval/lifecycle evidence, audit and both existing outbox intents atomically.
- Return both retained receipts. Their command UUID, actor and fingerprint match.
  Other concurrent responsibilities remain unchanged; no operating access is added.

Stale revisions/cutoffs, owner dependency failure, source changes, audit/outbox
failure or a competing command leave neither half committed. The normal posting
decision fingerprint/response remains unchanged. No new mutable transfer master,
permission family or migration is needed. See
[ADR-0061](../../adr/0061-department-team-and-atomic-transfers.md) and the
[canonical API contract](../../api/workforce-postings.md).

Scheduled correction preserves evidence: ending the destination posting does not
restore or extend the original. Continuing/returning requires a newly reviewed
placement covering the needed interval. The UI explains this before approval;
it does not offer a misleading undo. Unresolved commands retain their exact ID,
input and examined observations for explicit recovery rather than a new write.

## Workspace and delivery boundary

`/workforce/department-teams` exposes current teams, preparation, head/supervisor
review and ending responsibilities. The selected task alone mounts. Account
admission, tenant transitions and the shared unsaved-change guard clear old context
and hold task changes while a write/recovery remains unresolved. Transfer selection
is available during hospital-wide lifecycle review; it preserves the original
appointment and decision snapshots while the command is unresolved.

The postings page also unmounts account/Staff discovery immediately during a tenant
transition and performs fresh discovery on completion or cancellation; it cannot
reuse another tenant's Staff choices.

[The editable diagram](staff-and-rbac.drawio) now includes
[department teams and atomic transfers](diagrams/08-department-team.svg).
Validation evidence is recorded below after the local gates complete. No live migration or production rollout is implied. GitHub records the implementation
commit and merge status.

## Validation record

Commands actually run in the isolated worktree:

```sh
pnpm --filter @helivanta/shell test
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/ui test
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/shell lint
pnpm --filter @helivanta/api lint
node scripts/generate-workforce-postings.mjs --check
node --check scripts/verify-department-team-owner.mjs
```

The production build used `next build --webpack` with the local test issuer,
client ID and Global API destination. The final changed UI tests were also run
explicitly: posting review, current roster, department workspace and postings-page
context transition. The owner checks run from `backend/`:

```sh
go test ./internal/modules/workforce ./pkg/authz -count=1 -timeout 30m
go test -race ./pkg/authz ./internal/modules/workforce -run 'TestScopedCommandEvidence|TestDepartmentTeam|TestPostingTransfers|TestPostingReview|TestPostingInbox|TestPostingSupervisorDiscovery' -count=1 -timeout 10m
go test ./internal/archtest -count=1 -timeout 20m
go build ./...
go vet ./...
golangci-lint run ./internal/modules/workforce/... ./pkg/authz/... ./internal/globalapi/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
```

For the real browser/owner acceptance, first start the built local shell. Set
`HELIVANTA_TEAM_BROWSER_URL` to its URL, an installed Chrome executable if needed,
and a functioning local Testcontainers Docker host; Colima also needs
`TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`. Then run:

```sh
go test -race ./internal/globalapi -run 'TestPreEmploymentPersonReviewPreservesInactiveJoiner/(activation_posting_root_reviewer|activation_posting_root_approval|activation_posting_dept_reviewer|activation_posting_chain_child)$' -count=1 -timeout 20m -v
```

The fixture starts real Postgres, OpenFGA and NATS, creates independently approved
Staff/role/posting sources and supplies the signed test session to the browser
through stdin. Browser requests enter the real fixture Global API and product
owners. No roster, decision or receipt response is fabricated. The test deliberately
drops the first successfully committed transfer response and recovers the exact
paired command through the UI. It also verifies the retained pair directly.

Scope of that acceptance: department business flows, owner boundaries and retained
commands. External OIDC login and unrelated account profile/session-activity
services are not covered; those optional services are unconfigured in the fixture
and return explicit failures. No load/SLO, regulatory or production readiness is
claimed from small-fixture timing.

Final local results:

| Gate                              | Evidence                                                                                                                                                                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Shell suite                       | 1,703 tests / 190 files passed on combined main; final changed UI group: 78 tests passed                                                                                                                                  |
| API SDK suite                     | 895 tests / 88 files passed                                                                                                                                                                                               |
| Shared UI suite                   | 308 tests / 29 files passed on combined main                                                                                                                                                                              |
| Workforce and authz               | Full suites passed; final changed-case race checks passed                                                                                                                                                                 |
| Real browser + owner race journey | Passed in 288.742s on combined main, including the following department-review scenario, with real Global API, IAM/OpenFGA, Organization, Workforce/Postgres and NATS                                                     |
| Isolation/currentness             | Foreign hospitals hidden; missing action refused; department-only roster excludes sibling scope and validates an external management ancestor without disclosing its data; ended placements remain explicitly non-current |
| Transfer recovery/concurrency     | Concurrent exact requests return one retained pair; changed input and an earlier second consumer conflict; exact replay survives a later original-source ending                                                           |
| Failure atomicity                 | Audit and outbox fault injection roll back original ending, replacement approval and source-use evidence; original pending state/receipts are checked                                                                     |
| Build/static contracts            | Production webpack build, shell/API types and lint, Go build/vet/lint, architecture checks, canonical generation and registered-contract checks passed                                                                    |
| Documentation diagrams            | 78 local links resolve; eight native pages, 62 nonoverlapping cards and 52 connected edges validate; eight SVGs render within bounds; the new page was visually inspected                                                 |

The source-use read keeps the strict read-only RLS frame while taking the hospital
gate before department/actor locks. The final normal-role owner race/browser run
exercises that path. The new success callback is checked after lost-response
recovery, and expiry presentation is checked separately from an actual source
refusal. Initial failing tests exposed and corrected these behaviors; failing
assertions were retained as regression coverage.

Known build warnings remain the existing Next middleware/proxy deprecation and
server reexports from `react-hook-form`; they did not fail the build. Native
editor round-trip, representative load/SLO certification, external OIDC login,
unrelated account services and production rollout are outside this acceptance.
No database migration was introduced. The local test server was stopped after QA.

Original documentation was merged in
[PR #1600](https://github.com/tesserix/helivanta/pull/1600); the source, test and contract changes travel together in the department-team
implementation PR. Production activation remains separate. Do not close unrelated workforce,
clinical, pharmacy or credentialing work from these bounded department workflows.

### Merge handoff checks

The combined-main run adds the following department-review scenario after the new
team/transfer scenario. It originally exposed a temporary reviewer grant left
active by the fixture. Cleanup now withdraws each temporary grant through the
normal IAM command and checks that its assignment can no longer authorize. The
existing no-authority discovery assertion was retained, and the expanded real
browser/owner race run passes in 288.742s. This is test-fixture isolation; no
application permission or policy was relaxed.

The source-drift inventory check also required re-review after additive route
registration changed whole-file authority hashes. Sixteen unchanged existing
surfaces were re-reviewed, only two changed source files were repinned, and the
two new guarded routes were classified. All 15 inventory-tool tests and normal
checking pass; the global inventory remains 846 surfaces / 25 source-classified /
821 unclassified, with no runtime-ready marker. See the
[source-review note](../../qa/staff-operational-access-inventory.md#department-team-source-review--2026-11-06).
