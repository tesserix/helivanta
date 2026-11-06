# Independent pharmacy collaboration delivery

Requested scope, 6 November 2026: India, initially Maharashtra (`IN-MH`) and
Odisha (`IN-OD`), with both a pharmacy's own supplying outlet and a distributor's
SupplyConnect premise. The supplier remains the contracting/invoice party.
PharmaConnect and SupplyConnect retain their respective stock ownership; the
receiving pharmacy alone records its own receipt. No customer is represented by
an invented hospital, shared login or duplicate employer record.

This is an implementation work record. It does not activate trade, grant access,
approve an operating licence or establish that an unreleased route is deployed.
The accepted boundaries in ADRs 0024, 0026, 0031, 0049, 0054 and 0055 remain in
force. The [partner design](partner-collaboration.md) describes the complete
journey and required negative cases.

## Dependency order and completion evidence

| Work                                                       | Owner                                                                 | Required final evidence                                                                                                                                                                          | Current state                                                                                 |
| ---------------------------------------------------------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------- |
| Publishable hospital groups and direct reviewed membership | Shared IAM; Workforce retains Staff/link/posting eligibility          | Real-owner group proposal/review, current source-bound membership, immediate reductions, no nesting/self-enrolment, private directory isolation, UI and recovery                                 | Candidate publication, direct membership, owner choices and UI; broader/release gates pending |
| Named-person/group partner assignments                     | Shared IAM                                                            | Typed recipient, exact relationship/action/field/validity scope, independent approvals, current home eligibility, revocation/epoch and graph-failure tests                                       | Missing                                                                                       |
| Bilateral commercial relationship                          | Pharmacy trading boundary; Supply retains premise/legal/licence facts | Both exact legal identities and locations, same immutable terms and revisions, live identity, reviewed current category/activity/jurisdiction evidence; both approvals and unilateral suspension | Missing                                                                                       |
| Authorized partner disclosure                              | Source product                                                        | Audience/purpose/actor/object-bound machine contract; minimized hospital-specific projections, reauthorized pages and files; no foreign-tenant SQL or browser tenant switch                      | Missing                                                                                       |
| Orders and fulfilment                                      | Hospital procurement, supplier's source owner, receiving stock owner  | Exact units/lines, independent order approval, source dispatch, receiving discrepancy/quarantine, safe receipt, duplicate/reordered event and ambiguous-commit recovery                          | Existing stock foundations; composition missing                                               |
| Invoice/credit/settlement reconciliation                   | Existing source invoice and payable owners                            | Traceable source references and amounts, independent approval, unresolved/partial states, no inferred payment from receipt                                                                       | Missing composition                                                                           |
| Merge and production delivery                              | Application CI, Kargo `kargo-helivanta/prod`                          | Required CI green, exact merged tree and image revision, verified freight, completed promotion and healthy API/Global API/shell, production-safe smoke evidence                                  | Pending implementation                                                                        |

Every increment must include its canonical contract, owning handlers, migrations,
Global API route coverage, SDK/UI, audit/outbox/recovery, negative tests and design
updates. A dependency-only increment must remain unavailable to trade consumers
until its consuming journey passes acceptance; it is not completion of this work.

## State and category boundary

Supported initial state identifiers are ISO 3166-2 `IN-MH` and `IN-OD`. A location
in one supported state does not authorize another premise, a new activity or every
medicine category. Source and destination retain their actual jurisdiction; a
Maharashtra-to-Odisha relationship needs both applicability assessments.

Current primary-source verification and accountable applicability review remain
necessary for the exact premises, business activities, categories, tax and
restricted/controlled medicines. Unknown or missing category/activity authority
blocks a commitment. A feature rollout does not manufacture a legal policy or
approve a real participant's licence. Access-management development and synthetic
acceptance can proceed while these gates remain closed.

## Operational release target

Read-only discovery identified Kargo namespace/project `kargo-helivanta`, stage
`prod`. The authenticated cloud account and cluster context are retained in the
local execution record rather than this public document. The stage controls `helivanta-api`,
`helivanta-global-api` and `helivanta-shell`. User authorization includes Kargo
deployment of the completed work. Recheck the active target, exact freight,
application health, schema compatibility and recovery instructions immediately
before the named promotion; do not change the promotion policy or promote an
unrelated latest freight.

## Current candidate membership work

See [the ownership tree and membership contract](partner-membership-implementation.md)
and the [editable draw.io diagram](diagrams/09-independent-membership.drawio)
([SVG preview](diagrams/09-independent-membership.svg)). The SVG preview has been
rendered and visually inspected; the native XML passed geometry and connector checks. Candidate IAM migration `0034_iam_partner_members`
retains direct Staff nominations, independent review commands, source references,
cohort epochs and guarded current projections. It does not grant external access.

Current verification: actual IAM/Workforce/Global API membership and candidate
selection, followed by department transfer/cutoff regression cases, passed with
real Postgres, OpenFGA, NATS and the race detector (280.921 seconds). Nine UI tests
pass, including both lost-response recovery paths and removal of retained Staff
details after authority is withdrawn. See the [validation record](validation.md#independent-membership-candidate--6-november-2026)
for exact scope and remaining checks. No changes have been committed, merged or
promoted from this candidate worktree.
