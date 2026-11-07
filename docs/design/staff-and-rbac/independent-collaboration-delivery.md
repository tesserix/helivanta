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

## Supplier grant preparation — released checkpoint, 7 November 2026

The [new preparation contract](../../api/partner-grants.md) stores an immutable,
typed named-account or published-team proposal under the supplier's verified
tenant. PharmaConnect and SupplyConnect source locations remain distinct. Its
Go/Zod source, owner-only IAM routes, forced-RLS migration, Global API consumer,
SDK and governance UI explicitly report `pending_verification` and
`operating_access_enabled:false`. It does not resolve or approve the submitted
recipient, relationship, licence or category references.

Observed test cycles cover missing route → real-owner preparation/recovery,
canonical source-ID refusal, cross-tenant/cursor isolation, changed intent,
invalid scope and expiry, audit failure, exact SDK response/timestamp matching
and browser-component recovery of a deliberately lost response without a second
PUT. The [validation record](validation.md#supplier-grant-preparation--7-november-2026)
records the actual browser run and package checks.
[PR #1615](https://github.com/tesserix/helivanta/pull/1615) merged as `a9ed32e`
and its exact freight was promoted after explicit migration approval. See the
[production evidence](../../deployment/releases/2026-11-07-supplier-grant-preparation.md).
Required PR CI and both image security/publication workflows passed.

Source discovery also confirmed that SupplyConnect has no wholesale stock ledger
and that independent pharmacy Workforce routes currently register the employer
only. These dependencies remain part of the complete requested journey. At read-only discovery, the
production API was `62f3f7fd`; no organization-identity environment group was
configured, and canonical onboarding-service #12 remains open. No live change was
made during discovery. The remaining rows below are not closed by preparation.

## Dependency order and completion evidence

| Work                                                       | Owner                                                                                 | Required final evidence                                                                                                                                                                          | Current state                                                                           |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| Publishable hospital groups and direct reviewed membership | Shared IAM; Workforce retains Staff/link/posting eligibility                          | Real-owner group proposal/review, current source-bound membership, immediate reductions, no nesting/self-enrolment, private directory isolation, UI and recovery                                 | Released hospital team publication/membership; supplier activation gates remain open    |
| Named-person/group partner assignments                     | Shared IAM                                                                            | Typed recipient, exact relationship/action/field/validity scope, independent approvals, current home eligibility, revocation/epoch and graph-failure tests                                       | Missing                                                                                 |
| Bilateral commercial relationship                          | SupplyConnect relationship owner; each source retains its premise/legal/licence facts | Both exact legal identities and locations, same immutable terms and revisions, live identity, reviewed current category/activity/jurisdiction evidence; both approvals and unilateral suspension | Missing                                                                                 |
| Authorized partner disclosure                              | Source product                                                                        | Audience/purpose/actor/object-bound machine contract; minimized hospital-specific projections, reauthorized pages and files; no foreign-tenant SQL or browser tenant switch                      | Missing                                                                                 |
| Orders and fulfilment                                      | Hospital procurement, supplier's source owner, receiving stock owner                  | Exact units/lines, independent order approval, source dispatch, receiving discrepancy/quarantine, safe receipt, duplicate/reordered event and ambiguous-commit recovery                          | Existing stock foundations; composition missing                                         |
| Invoice/credit/settlement reconciliation                   | Existing source invoice and payable owners                                            | Traceable source references and amounts, independent approval, unresolved/partial states, no inferred payment from receipt                                                                       | Missing composition                                                                     |
| Merge and production delivery                              | Application CI, Kargo `kargo-helivanta/prod`                                          | Required CI green, exact merged tree and image revision, verified freight, completed promotion and healthy API/Global API/shell, production-safe smoke evidence                                  | Preparation checkpoint released; full collaboration implementation/release remains open |

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
rendered and visually inspected; the native XML passed geometry and connector checks. Candidate IAM migration `0036_iam_partner_members`
retains direct Staff nominations, independent review commands, source references,
cohort epochs and guarded current projections. It does not grant external access.

Current verification: actual IAM/Workforce/Global API membership and candidate
selection, followed by department transfer/cutoff regression cases, passed with
real Postgres, OpenFGA, NATS and the race detector (280.921 seconds). Nine UI tests
pass, including both lost-response recovery paths and removal of retained Staff
details after authority is withdrawn. See the [validation record](validation.md#independent-membership-candidate--6-november-2026)
for exact scope and remaining checks. The reviewed-membership checkpoint merged through PR #1614 as `0aadd558`.
That membership foundation is included in the released `a9ed32e` image. Supplier
grant preparation remains a separate non-operating checkpoint; effective external
assignments and the full collaboration journey are not implemented by it.

### Latest integration verification — 7 November 2026

The candidate integrates the newer department leave-review work and dependencies
from upstream main. Its migration sequence follows those upstream changes:
IAM `0034_iam_partner_groups`, `0035_iam_partner_membership_templates`,
`0036_iam_partner_members`; Workforce `0090_workforce_partner_group_scope` and
`0091_workforce_partner_member_candidates`.

The integrated actual-owner/race run passed in 231.519 seconds. Its browser used
a hospital-scoped staff role, real Global API/IAM/OpenFGA/Workforce/Postgres,
committed a membership suspension, lost the successful response, and recovered
its receipt with a single PUT. Integrated validation also passed 84 template
migration tests, 81 affected UI tests, targeted IAM checks, API typechecks and the
production shell build. The preceding feature checkpoint passed the full IAM,
authorization and platform packages, 909 API tests and 1,712 shell tests; the
later scoped UI correction was additionally covered by the real-browser run.

These checks validate hospital recipient-team management. The external supplier
relationship, typed independent employer operations, bilateral disclosure,
physical stock/financial composition and production release entries above remain
open. No readiness marker or operating licence has been activated.
