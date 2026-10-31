# Staff Access and RBAC completion

Status: active implementation. Baseline: `293259d`, 2026-10-30.
The objective is full acceptance of A01–A12 in the
[delivery tracker](../../qa/staff-management-progress.md). Individual fixes do
not close the objective. Existing production approvals cover their named source
and migrations; new production candidates need their own approval.

## Required end state and evidence

| Gate                            | Required implementation and acceptance evidence                                                                                                                                                            |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A01 Identity provisioning       | Identity-owner create/resolve and verified-contact results; bounded duplicate/conflict and uncertain-provider reconciliation; authorized Admin UI; no manufactured verification or credential disclosure.  |
| A02 Membership                  | Explicit least-privilege enrollment, target/account/tenant preconditions, safe retries, current-generation recovery and no operational access from membership alone.                                       |
| A03 Staff/login binding         | Independent current-person binding, immutable contributor/subject history, approve/reject/withdraw/revoke, one current link, current actor checks and new-hire/non-digital-worker support.                 |
| A04 Scoped access               | Current subject/manager/reviewer and employment basis; independent review, validity, acknowledged current projection, all actual operational consumers and reviewed legacy-grant classification.           |
| A05 Renewal                     | Fresh proposal and independent review; fixed validity and predecessor evidence; before/after/future-start boundaries, stale/withdrawn generations and uncertain retries.                                   |
| A06 Security disable/recovery   | Current IdP checks and credential watermark on old/new sessions, renewals, reads, commands and file release; provider outage denies; recovery requires fresh authentication.                               |
| A07 Suspension/resumption       | Reviewed hospital-local effective transition; immediate authoritative cutoff without asynchronous cleanup; other hospitals/tenants and governance preserved; fresh decisions on resumption.                |
| A08 Notice/resignation          | Attributable request, acceptance/withdrawal, last-working/cutoff instant and handover; submission alone does not end access; queued stale grants cannot grow authority.                                    |
| A09 Termination/contract expiry | Effective denial of assignments, links and cards; durable cleanup and visible owner acknowledgments; clinical, payroll and audit records preserved.                                                        |
| A10 Rehire                      | New reviewed employment generation and current identity/evidence/link; fresh assignments; old events, grants, cards and approvals cannot restore authority.                                                |
| A11 Membership/access removal   | Distinct role, tenant membership, employment and global-identity actions; incarnation-bound Admin commands; current-source denial despite stale graph; other roles/scopes preserved; explicit recovery UI. |
| A12 Retention                   | Offboarding precedes separately authorized retention/privacy actions; preserve completed clinical, financial and security history; no hard-delete shortcut.                                                |

Every applicable gate needs real UI Admin/reviewer/recipient journeys, server and
forced-RLS isolation, stale account/source/generation and historical self-review
denial, duplicate/concurrent/uncertain retries, dependency outages, persistence,
race tests, contracts/generators, lint/build, full CI and deployed acceptance.
Record local, CI and production evidence separately. A denial-only reviewer
walkthrough does not prove independent approval.

## Execution order

1. Close authoritative revocation and replay gaps, preserving self-service
   recovery/sign-out and unrelated scopes. Start with tenant-role permission
   resolution: current production middleware uses graph permissions and graph
   membership while IAM owns the underlying membership rows. Reproduce stale
   graph behavior before changing it; see the
   [current-role design](../specs/2026-10-30-current-tenant-role-authority.md).
2. Deliver incarnation-bound membership removal/recovery UI and identity-owner
   provisioning/contact verification, including provider reconciliation. The
   [membership design](../specs/2026-10-30-membership-removal-and-recovery.md)
   records current source gaps, durable commands, legacy-client migration,
   projection ordering and Admin/recipient acceptance. Local implementation is
   active; it is not an accepted/deployed API or a closed acceptance gate. The
   [incarnation graph design](../specs/2026-10-30-membership-incarnation-graph.md)
   adds current-row proof to admission, discovery, projection and boot pruning.
3. Inventory all HR intake/document authorization phases, introduce scoped HR
   intake without broadening shared enrollment consumers, then implement safe
   current-person basis and immutable independent employment review. Follow the
   [existing continuation plan](2026-10-30-staff-access-and-review-continuation.md).
4. Inventory/classify legacy operational grants and implement owner eligibility
   projections and transaction/file-release guards for every actual consumer.
   Preserve explicit governance/recovery authority. Complete reviewed migration
   reconciliation before activating enforcement.
5. Deliver suspension, resumption, notice/resignation, termination, dated contract
   expiry and rehire with fresh generations and durable owner acknowledgments.
6. Complete A01–A12 requirement-by-requirement acceptance, recovery and retention
   checks; publish exact tested source/migration evidence and request the exact
   production rollout when ready. Mark complete only after deployed acceptance.

## Boundaries

IAM owns memberships, assignments, epochs, session revocation and authorization.
Workforce owns Staff, employment, identity cases and links. Zitadel owns global
identities and credentials. Each clinical, pharmacy, supply, emergency, payroll,
file and physical-access owner retains its facts and action authorization. The
Global API remains the UI entry point; no cross-owner SQL is permitted.

Follow ADR-0044/0047/0048/0049 and ORG-AC-01/02/04/05/06/07/08. Do not invent
answers to ORG-D2/D5/D6 or statutory retention periods. Single-hospital employment
changes cannot disable the global identity, appoint/remove governance owners or
revoke valid authority in another hospital or tenant.
