# Staff lifecycle, identity and access: research and delivery design

Date: 2026-10-27. Status: researched implementation proposal, not a claim that
these workflows are deployed. Covers the product owner's requests for complete
Staff Management, identity/background verification, photographs, staff IDs/cards,
and the consequences of disablement, suspension, resignation and termination.

Read with [the complete workflow ledger](../../qa/staff-management-acceptance.md),
[scoped Staff access](2026-10-27-scoped-staff-access.md),
[the Workforce consumer design](2026-10-27-scoped-leave-consumer.md),
[organization access](../../domain/organization/access-model.md),
[ADR-0044](../../adr/0044-organization-structure-and-access-defaults.md), and
[Workforce ownership](../../api/domains/workforce.md). This work applies
ORG-AC-01/02/04/05/06/07/08; ORG-D2/D5/D6 remain open.

## 1. Keep the owners and meanings separate

| Fact/control                                                                                                          | Owner                                                                                 | Boundary                                                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Human login, authentication methods and identity-provider lock/deactivation                                           | Zitadel through the approved identity/platform administration boundary                | A global identity can belong to several tenants. One hospital's HR action must not implicitly disable that global identity.                                    |
| Helivanta sessions, tenant membership, scoped assignments, validity, revocation and authority epochs                  | IAM/authentication infrastructure                                                     | A valid JWT or membership alone does not authorize a hospital action.                                                                                          |
| Staff identity, employment periods, employment state, identity/background review evidence and staff-to-login bindings | Workforce                                                                             | Employment is not a login or clinical privilege. Preserve one staff owner and attributable history.                                                            |
| Departments/sub-units                                                                                                 | Platform organization structure                                                       | Workforce postings reference the owner; do not copy department masters.                                                                                        |
| Photo/evidence bytes and release                                                                                      | Existing private file/storage/scanning infrastructure, with Workforce as record owner | Quarantined, unreviewed, stale or unauthorized files cannot be disclosed or printed on a card.                                                                 |
| Staff card issue, replacement and validity                                                                            | Workforce, with facility/security consumers for physical access                       | A staff card is neither a login credential nor proof of clinical authority. Physical access removal needs its own verified integration or acknowledgment.      |
| Clinical privileges, prescribing/sign-off and outstanding care                                                        | Relevant clinical owners                                                              | HR exit must close the employment-based authority and initiate accountable handover; it does not erase clinical records or national professional registration. |
| Payroll, final settlement, payslips and statutory reporting                                                           | Workforce/payroll and approved financial integrations                                 | Configuration/preview is not posted payroll or payment. Human approval and jurisdictional policy remain necessary.                                             |

## 2. Actor capability matrix

These are target acceptance rules. Actual templates, scopes and any exceptions
must be declared and reviewed, not inferred from a job title.

| State or action                                                     | What the person may do                                                                                                           | What must stop                                                                                                                   | Required recovery/closure                                                                                                                               |
| ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Active employment, active login                                     | Only actions covered by current reviewed assignments, valid credentials and exact resource scope                                 | Self-approval, unassigned departments/hospitals, payroll or clinical access without separate authority                           | Normal audited amendments and periodic review                                                                                                           |
| Global identity security lock or deactivation                       | No authenticated Helivanta business or self-service action after the effective security boundary                                 | New login, renewal, old cookies/bearer tokens, exports/downloads, approval commands and account-linked machine authority         | Authorized security review, credential/session recovery where required, fresh authentication; business authority must still be current                  |
| Tenant/workspace access suspended                                   | No business access in that tenant                                                                                                | All tenant workspaces and dependent scoped commands; switching or renewing must not recreate access                              | Independent authorized restoration; other tenants remain isolated and unaffected                                                                        |
| Hospital employment suspended                                       | No employment-derived work/approval/clinical authority in the affected hospital                                                  | Own employment-derived assignments/delegations, posting-based work, card validity according to the reviewed facility policy      | Record scope, reason, effective time, reviewer, review/end date and applicable pay treatment; do not automatically restore old grants when time elapses |
| Resignation submitted or notice period                              | Only currently authorized duties under the approved notice/handover plan, until the accepted access cutoff                       | Privilege growth or bypass of an independent approval; no assumption that submission itself is the final employment-end decision | Authorized acceptance/withdrawal, agreed last working date, versioned handover and scheduled offboarding                                                |
| Resigned / terminated / contract ended at the effective cutoff      | No old employment-derived operational or approval access                                                                         | Old assignments, delegations, links and cards; existing sessions must not retain an affected scope through a stale graph/cache   | Finish attributed handover, property/card return, final settlement and retained records without keeping the former worker authorized                    |
| Removed from one hospital, still active elsewhere                   | Only independently authorized duties in the remaining scopes                                                                     | The removed hospital's assignments, staff link and card                                                                          | Reconcile postings, dependent work and scope epochs; do not globally delete or disable the identity                                                     |
| Former employee documents, if the organization permits this service | Only explicitly approved self-only disclosure, such as released payslips or letters, through a separate purpose-bound assignment | Directory, schedules, approvals, other staff records, clinical/operational work and arbitrary private files                      | Current identity verification and disclosure authorization; no automatic reuse of an old active-employment link                                         |
| Reactivated employment or rehire                                    | Nothing from historical grants automatically                                                                                     | Replaying old pending approvals, card IDs, employment bindings or stale projection acknowledgments as new authority              | New/reviewed employment period and current identity/link/credential checks; fresh independent assignment decisions and card issue                       |
| Archived or deleted record                                          | No access is conferred by retaining historical data                                                                              | Login or business authority cannot survive because a record is merely archived                                                   | Apply the owner's approved retention/privacy process. Keep required audit and completed business evidence; do not use deletion as offboarding           |

Global security disablement and employment separation are deliberately different
commands. Governance/ownership assignments also require explicit authority review;
an HR edit cannot silently appoint or remove a tenant owner. Existing unclassified
legacy grants need reviewed migration rather than assumed employment provenance.

## 3. End-to-end lifecycle

### Joiner and card issue

1. Create the canonical staff record and employment period with stable identifiers
   and duplicate checks. Distinguish a staff UUID, printable employee code, login
   subject, employment-period ID and card-issue ID.
2. Collect the identity/background evidence required by the applicable reviewed
   organization/country policy, plus a protected photograph. Record purpose,
   provider/check method, evidence revision and actual result. Email verification,
   a membership row and an uploaded file are not an identity/background decision.
3. An independently authorized reviewer records the evidence decision. Do not let
   the subject verify their own identity, photograph or background checks. Missing
   evidence/provider availability remains an explicit pending/unavailable state.
4. Create/resolve the identity through its owner, enroll the minimum IAM membership
   and independently review the hospital-local staff/login binding.
5. Propose the applicable scoped templates from the posting. Verify current
   preconditions and independent approval; activate only after acknowledged
   projection/current-state checks. No clinical permission is implied by HR.
6. Issue the card from the approved identity/photo revision, with issue/version,
   validity and minimal printed data. Audit preview, issue and sensitive downloads.
   A QR/barcode must not become an authentication bearer credential or disclose
   confidential HR/background information.

### Suspension or immediate security action

1. An authorized human selects the exact kind and scope of the action. Show its
   effects before confirmation; distinguish security lock, workspace suspension
   and hospital employment suspension.
2. Record a stable versioned command ID, affected employment/scope, effective
   instant, reason, actor and independent review where required. Confidential
   disciplinary/security evidence stays behind its own protected reads.
3. At the security boundary, write the authoritative deny/epoch/revocation state
   atomically before relying on asynchronous propagation. Do not report success
   while a cached/legacy path can continue the affected work.
4. Revoke/suspend employment-derived assignments and delegations, invalidate old
   request/graph evidence and session scope, stop sensitive downloads/exports and
   block new approvals. Commands queued on a product lock must recheck authority
   at their transaction boundary.
5. Notify the accountable supervisor/HR/security owners and hand pending work to
   currently qualified authorized people. Preserve the original actor and history;
   do not allow unauthorized work to continue merely to clear a queue.
6. Record physical-card/security and external-system acknowledgments. Failed
   downstream work stays durably pending/retryable with clear escalation, while
   the application's affected authority remains denied.

### Resignation, termination, contract end and exit

1. Record the requested event separately from its accepted outcome: request,
   review, accepted last working date and effective access cutoff. A termination
   decision is made by the authorized human process, not inferred by software from
   background data or a failed upload.
2. Create a versioned exit plan: supervisor handover, schedules/leave/duty/OT,
   patient/clinical-owner handovers where applicable, equipment/property/card,
   private documents/letters and payroll settlement. Give each obligation an owner,
   state and evidence; define justified not-applicable cases.
3. Before the cutoff, permit only the approved notice/handover duties. Use the
   owning facility's timezone and an unambiguous effective instant. Do not hardcode
   `23:59` as a universal end-of-employment rule.
4. At the cutoff, atomically deny the affected employment authority even if a
   projection worker, identity provider or downstream consumer is unavailable.
   Reconcile memberships carefully when other valid scopes or former-staff
   disclosure purposes remain. Never delete a shared identity as a hospital exit
   side effect.
5. Clear/reassign pending approvals and scheduled duties under an accountable
   supervisor. Leave, attendance and payroll owners apply their existing correction
   and compensation rules; completed ledgers and clinical decisions are retained.
6. Payroll creates a reviewed final-settlement result from authorized source
   revisions. Jurisdictional rules, deductions, encashment, tax/reporting and
   payment/ERP settlement require their own current policy and reconciliation.
   A successful preview is not payment or exit clearance.
7. Close the exit only when required acknowledgments/evidence are complete or an
   authorized exception is recorded. Keep pending external obligations visible.
   Restrict any later self-service disclosure through a separately reviewed purpose.

### Reactivation and rehire

1. Verify the authority and reason for restoration, current employment period,
   identity/background policy, credential validity and any unresolved restriction.
2. A new employment incarnation must prevent historical assignments or queued
   events from becoming current again. The IAM membership generation alone is not
   enough when the person remains a member through another hospital or purpose.
3. Re-review the staff/login binding, propose current postings/templates, complete
   independent approval and projection acknowledgment. Issue a new card/version;
   a revoked card identifier never becomes valid again.
4. Preserve prior employment/exit/disciplinary/audit records under their owner
   permissions. Rehire is not an edit that erases the earlier separation.

## 4. Current implementation evidence and gaps

| Current source evidence                                                  | What it proves                                                                                                                                                                                                                                                                                              | What it does not prove                                                                                                                       |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `workforce/handlers.go`, `schema.sql`, enrollment contracts              | Hospital-local staff UUID/code and `active`/`inactive` employment fields; private enrollment and explicit legacy reader/manager grants                                                                                                                                                                      | Full suspension, notice, resignation, termination, employment-period or exit-clearance lifecycle                                             |
| `workforce/login_links.go` and `login_links.sql`                         | Staff inactivation revokes pending/active reviewed links in the owner transaction; history, audit and outbox are retained                                                                                                                                                                                   | Removal of every legacy grant, IAM assignment, session, card or downstream clinical authority                                                |
| IAM membership/assignment generation, epoch and projection code          | Final-role removal invalidates an old tenant membership incarnation; scoped assignment provider checks current acknowledged authority                                                                                                                                                                       | Hospital employment incarnation when other membership remains; universal legacy-route offboarding                                            |
| `authn.Middleware`, IAM revocation and current-identity guard (ADR-0047) | Signed credentials, idle deadline, fresh watermark and current IdP state guard ordinary requests; synthetic local UI and real-DB regressions verify denial and fresh-authentication recovery                                                                                                                | Deployment, upstream capacity/availability, in-flight cancellation and employment-scoped offboarding are not proved by this local checkpoint |
| IAM renewal/user-state wiring                                            | Renewal still checks IdP state and membership; the same state reader now guards ordinary traffic independently of browser renewal                                                                                                                                                                           | The rest of Staff lifecycle, tenant/hospital authority and legacy-grant convergence remain open                                              |
| Current revocation checker                                               | Direct invalidation/broadcast with a five-minute cache backstop for a missed broadcast                                                                                                                                                                                                                      | The full #417 cutoff guarantee under a lost broadcast or unavailable downstream propagation                                                  |
| Private staff files, identifiers and credential review                   | Reusable protected evidence, scanning, sensitive identifiers and professional validity foundations                                                                                                                                                                                                          | Completed general identity/background review, approved photo or staff-card issue lifecycle                                                   |
| Scoped leave consumer in development                                     | Scoped-only approval/replay, self-decision denial, link revocation, purpose-bound RLS/standing checks, shared-transaction guard and signed-session Global API/IAM/Postgres/NATS/OpenFGA integration pass locally; controlled withdrawal/expiry races and missed-invalidation credential cutoff are verified | A released reviewer discovery/queue/UI, universal legacy-route cutoff or the whole Staff lifecycle                                           |

Do not silently reinterpret old grants as reviewed employment assignments. Before
claiming offboarding completeness, enumerate every actual protected route and
consumer, including legacy clinical/operational routes, private file release and
already-issued session paths. Review and migrate ambiguous legacy authority.

## 5. Durability, security and recovery invariants

- PostgreSQL owner state is authoritative. Stable command IDs, content/expected
  version checks, immutable transitions and minimized outbox events commit together.
- Scope/end-date checks are server enforced. A browser timer, hidden button,
  successful IdP call, graph tuple or published event is not completion evidence.
- The effective deny state precedes asynchronous cleanup. Retry/replay rereads the
  current owner state, is idempotent and cannot reactivate an old incarnation.
- Multi-owner exit work uses durable orchestration/acknowledgments. Avoid network
  calls under database locks. Compensation preserves history and must never restore
  an unsafe employment/access state because a later notification failed.
- A pending operation must not outlive its authorization while waiting on a lock.
  Use an owner-controlled command guard in the same transaction as protected product
  writes; a second check on an independent connection is insufficient if that
  connection fails and releases its locks before the product commits.
- Separation causes, background findings, identifiers and photos are minimized,
  purpose scoped and audited. Ordinary coworkers see only permitted operational
  availability, not a confidential dismissal or disciplinary reason.
- Security disablement denies every old credential path. Recovery requires current
  authority and fresh authentication; neither identity unlock nor employment
  reactivation restores historical privileges automatically.
- Permanent deletion is a separate privacy/retention operation with its required
  authorization and named live approval. Never erase completed clinical, stock,
  payroll or security history to make an exit look complete.

## 6. Synthetic acceptance and release sequence

Continue the ordered Staff workflow ledger; do not jump between completed-looking
screens. Implement bounded, correct vertical slices with their UI and integration
evidence, retaining all remaining acceptance items.

1. Complete Staff access: protected enrollment, independent binding/assignment
   administration and bootstrap, current command guards, reviewer queue, revocation
   and least privilege. Prove existing-session behavior, not only new-login refusal.
2. Complete Staff directory/onboarding: identifiers, protected identity/background
   review and photo, employment-period transitions, supervisor/posting relationships,
   card lifecycle and safe offboarding entry points.
3. Finish the HR, roster, leave, duty, attendance and payroll areas one at a time,
   exercising each exit/suspension/transfer/recovery dependency when its owner is
   delivered. Payroll processing and final settlement remain separate from setup.
4. Complete cross-owner lifecycle orchestration and legacy authority migration.
   Verify revocation under dependency failure and effective-time races before
   labelling #417/#1053/#1132/#1107 and related issues end-to-end complete.

Required scenarios include: same person active in two hospitals/tenants;
suspension while an approval waits on a lock; removal while a cached token/graph
still allows; missed broadcasts; unreadable identity state; future cutoff; notice
withdrawal; expired/changed photo/check; departed clinical signatory; unreconciled
payroll; card reissue/loss; failed external cleanup; repeated commands; and rehire
with old grants/events. Every scenario uses synthetic data through Staff Admin
and independent reviewer/employee UIs, with server/ledger/audit assertions.

## 7. Primary research and applicability

- [NIST SP 800-53 OSCAL catalogue, revision 5.2.0](https://github.com/usnistgov/oscal-content/blob/main/nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog.json),
  retrieved 2026-10-27; verified Git blob
  `55eb9f2506cab43e051eeac6b48496246afcfd2c`. AC-2 links account management to
  personnel termination/transfer; AC-6 requires least privilege; PS-4 covers ending
  access/authenticators, property and retained organizational information; PS-5
  requires reviewing/modifying access after transfer; IA-4 covers identifiers;
  AU-11 requires an organization-defined retention period. These are security
  design references, not a claim that federal controls are automatically Indian
  hospital law or that the product is certified. Their timing/retention parameters
  are organization defined, not defaults invented here.
- [Zitadel user service API source](https://github.com/zitadel/zitadel/blob/main/proto/zitadel/user/v2/user_service.proto),
  retrieved 2026-10-27; Git blob `10d901124087a14a4c697684184b2c44c7f483d2`.
  Deactivation, reactivation, temporary lock/unlock and deletion are distinct
  operations. The source describes their IdP login consequences; it does not prove
  revocation of Helivanta's independently signed application cookies.
- Apply the [India source register](../../domain/india/sources.md) and current
  qualified HR/legal/payroll review to actual country/state, employment/service
  scope, background screening, identity collection, notice, final pay, leave
  encashment, statutory reporting and retention. No mandatory government identifier,
  police check, notice period, automatic employment decision or settlement rate is
  activated by this research note.

## Cross-tab session disclosure fix, 2026-10-28

Implemented in development after local Staff UI QA found an old administrator
view persisting in another tab after a different user signed in. Assets are
cached staff/private query results, enrollment drafts and session hints on a
shared workstation. The boundary is the shared same-origin session cookie versus
each tab's independent React memory. A later workstation user must not inherit
rendered data from the previous subject or tenant. This change applies ORG-AC
revocation/isolation intent; it is not a new source of authorization.

The shared API package publishes a data-free invalidation marker on explicit
sign-out/idle ending, terminal renewal rejection, successful login and successful
tenant switching. Storage events and BroadcastChannel are complementary. The
broadcast carries an ephemeral page identifier solely to ignore the publisher's
own message, since separate BroadcastChannel instances in the same tab otherwise
receive it too. No user, tenant, staff identifier or credential is transported.
Receivers cannot grant authority; malformed/unrelated messages do not activate
anything. There is no acknowledgement graph, polling, new backend endpoint or
infrastructure: one small signal per transition and O(open tabs) local handling.

Private workspaces cancel/clear queries, remove the rendered subtree, clear local
permission/renewal hints and navigate to sign-in. A receiver does not call logout
or end the IdP session, which would race and revoke the new user's login. The
sign-out publisher notifies before awaiting remote logout. Expired or rejected
login attempts and transient renewal outages do not publish a successful session
change. Browser-history restoration of a private page also clears/rechecks it;
public pages clear stale query state without redirecting or discarding local
form drafts. The application wrapper reuses the renewal loop's public-route
classification. Renewal scheduling is still per-tab, without leader election.

A regression reproduced old private text surviving a storage event. Regressions
also cover storage-disabled broadcasting, publisher self-message exclusion,
query clearing, public draft preservation, browser-history restoration and the
real sign-out/login/tenant-switch/terminal-renewal publishers. Actual two-tab
local UI QA verified open enrollment removal on sign-out and on another subject's
successful login, with the new login still usable; the public signup page stayed
open during Staff sign-out. The current signup/ambulance pages do not have the
synthetic form used by the component draft-preservation regression.

If both browser transports are unavailable, immediate cross-tab notification
cannot be promised; server authorization/renewal checks remain authoritative.
These notifications do not solve external IdP-disablement timing, remote-device
revocation, every legacy grant or employment offboarding. Inactivation still
revokes reviewed staff links; local UI reactivation testing confirmed the old
link stayed revoked and no leave-review hospital reappeared. The wider lifecycle
and new employment-incarnation model above remain required. No production
application, new migration or broad Staff-completion claim follows from this fix.

## Access renewal implementation slice — 2026-10-29

A05 in the [delivery tracker](../../qa/staff-management-progress.md) uses the existing IAM proposal/decision/activation contract; it adds no endpoint, permission, migration or automatic extension. A manage-capable actor can use an activated assignment (including one whose validity ended) to prepare a new proposal for the same account, hospital and fixed template. Starts-at, expires-at and a fresh reason are mandatory. The reason includes the previous ID/version for human traceability; this text is not an authorization proof or a server-enforced predecessor relation. The user may use ordinary proposal intake for other changes.

The source assignment is unchanged. The new UUIDv7 proposal captures current membership generation and must pass current proposer hospital verification, independent reviewer verification and acknowledged activation. A withdrawn/rejected/unactivated record has no renewal shortcut. Historical authority never bypasses current membership, Staff/login binding, employment generation or current permission checks. An expired source is only preparation data; submitting the new proposal does not restore access. The immutable history of each proposal remains separate.

The preparation UI pins the source account/hospital/template, clears dates and reason, keys state by account/tenant/family/source and retains the same new ID and payload after an uncertain outcome. Adding the source reference must not exceed the canonical 500-character reason limit. Provider/owner outages retain existing pending states and retry handling; no remote write or new infrastructure is introduced. Read/write volume and payload bounds are unchanged from the current at-most-50-row page and 8 KiB proposal intake. Source volume does not add an unbounded lookup. Existing IAM authorization latency/failure behavior remains authoritative.

Implementation plan: reproduce the missing renewal UI through component tests; reuse current proposal intake without modifying old history; test required fields, canonical reason bounds, retry identity, role/state/family boundaries; run frontend lint/format/type-check/test/build; exercise synthetic maker, independent reviewer and recipient UI; record evidence in the tracker. No full account-lifecycle or production acceptance is implied by component tests.
