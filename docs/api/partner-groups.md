# Hospital-published partner groups — implementation candidate

Shared IAM owns group identity, immutable nominations/decisions, cohort epochs and
retained receipts. Workforce owns hospital existence, Staff, reviewed account
links, employment generations and approved postings. This candidate is not a
supplier grant, licence, stock movement or deployed integration.

Canonical wire inputs: [`contracts/partner-groups.json`](../../contracts/partner-groups.json).
Run `node scripts/generate-partner-groups.mjs` and its `--check` mode to generate
IAM Go, API-package Zod/types and identifier-only IAM/Workforce event contracts.
Never hand-edit their generated files. The [implementation specification](../design/staff-and-rbac/partner-membership-implementation.md)
defines the authority matrix, state machine, source ceiling and recovery rules.

## Routes and admission

All browser traffic enters Global API and carries the verified expected tenant
and subject preconditions. These are product-owner paths, not direct browser
upstream URLs.

| Path under `/v1/iam`                                                                    | Methods  | Admission                                                                                                                                            |
| --------------------------------------------------------------------------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/partner-groups`                                                                       | GET      | Current IAM governance administrator and assignment-management permission                                                                            |
| `/partner-groups/:id`                                                                   | PUT, GET | Current governance administrator; preparation is immutable                                                                                           |
| `/partner-groups/:id/commands/:command`                                                 | PUT, GET | Independent governance publication review or current unilateral reduction authority; recovery reauthorizes                                           |
| `/hospitals/:hospital/partner-groups`                                                   | GET      | Current hospital-only membership-reader role and reviewed digital Staff actor                                                                        |
| `/hospitals/:hospital/partner-member-capabilities`                                      | GET      | Current hospital membership-reader role and reviewed Staff actor; prepare/review flags are scoped UI hints, never command authority                  |
| `/hospitals/:hospital/partner-member-candidates`                                        | GET      | Same current membership-reader authority; minimal Workforce-owned candidate projection                                                               |
| `/hospitals/:hospital/partner-groups/:group/member-proposals`                           | GET      | Same current authority; exact hospital/group scope                                                                                                   |
| `/hospitals/:hospital/partner-groups/:group/member-proposals/:member`                   | PUT, GET | Prepare requires membership-preparer role and current nominee source; GET requires membership-reader role                                            |
| `/hospitals/:hospital/partner-groups/:group/member-proposals/:member/commands/:command` | PUT, GET | Approve/reject/suspend require membership-reviewer role; withdrawal requires the original preparer; GET requires current membership-reader authority |

Every operational admission jointly checks its IAM permission and the hospital
`workforce.partner_member_source.use`, `workforce.posting_source.use` and
`workforce.staff_actor.use` grants. Legacy administrator status alone does not
supply these scoped grants. Object queries bind the verified tenant and exact
hospital; wrong-scope records are `404`, missing current permission `403`, revoked
credentials `401`, changed intent/version `409` and unavailable authority/evidence
`503`. Responses are not cacheable. Sensitive reads require committed audit.

Collections use signed cursor pagination with bounded limits. Candidate pages
are limited to 50 and explicitly say `source_state:candidate`. They expose only
named-account/posting selection facts, exclude the requester and require live
source revalidation when preparing or approving. A historical `approved`
membership is not a positive observation of current external access.

Retain a UUIDv7 before sending preparation or review. After an unknown outcome,
recover using GET or retry the same identity and exact intent. Do not invent a
new command. Recovery precedes nominee revalidation but never bypasses the
requester's current authority. Approval binds the exact Staff/link/generation,
employment epoch and IAM membership incarnation; rehire or relink does not revive
it. A different Staff reviewer must independently confirm source and purpose
eligibility. Rejection, withdrawal and suspension can reduce an inactive nominee.

IAM writes audit, immutable receipt, current projection, cohort epoch and outbox
atomically. Workforce restores its private source frame before invoking IAM.
No IAM handler queries private Workforce tables. Source observations are evidence,
not reusable authorization or an external disclosure capability.

## Consumer and release boundary

Typed external recipient grants, bilateral supplier relationships, current access
admission, partner object/field disclosure and both stock-owner fulfilment journeys
remain separate required consuming work. Track their actual acceptance and exact
Kargo release in the [delivery ledger](../design/staff-and-rbac/independent-collaboration-delivery.md).
Maharashtra and Odisha are the initial India jurisdictions; no statutory policy
or real participant licence is activated by these metadata endpoints.

Tenant-wide `/iam/me/permissions` identifies the account and governance navigation;
it does not advertise every hospital-scoped role. Operational membership screens
use current Staff hospital discovery and the hospital capability read. The latter
fails closed when its authority dependency is unavailable. Each command still
reauthorizes its full scoped permission/source bundle; a displayed capability
never authorizes a later mutation.
