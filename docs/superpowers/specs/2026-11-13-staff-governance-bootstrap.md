# Scoped Staff reviewer bootstrap

Status: implementation design; no deployed permission or approved migration.
Implements the accepted ORG-D7 policy in ADR-0044. It does not settle an open
ORG-D decision. Protected ordinary account initiation was merged in PR #1668; the governance ledger and independent decision implementation are in draft; consumer adoption and remote acceptance remain unfinished.

## Source findings and boundary

Account creation and `staff_member` enrollment grant no business authority.
`iam/assignment_decisions.go:assignmentAdministrator` currently requires a
current `tenant_admin` row. Workforce's `loginLinkTransaction` requires
`workforce_staff_allowed(...,true)` and the login-link RLS policies repeat that
legacy manager requirement. The existing template catalog has identity-check
reviewers, but no separately delegable hospital account-link reviewer.
Adding a UI button or an FGA tuple cannot resolve these source requirements.

Do not create another tenant owner, grant QA users `tenant_admin`, insert legacy
hospital-manager rows, fabricate an independent decision, or reuse an operational
Staff/posting snapshot as governance authority. In particular, do not change the
existing SQL second-person decision guard to accept its proposer. A provisional
single-owner grant is a distinct policy outcome, never an independent approval.

IAM owns governance delegation and review. Workforce owns Staff and account
links. Organization and the independent employer owners retain their canonical
scope identities. Application owners are current IAM `tenant_admin` memberships;
native organization-management roles are a separate identity-provider concern.
The protected product does not let a caller select a native organization or
supply actor/tenant authority.

## Full lifecycle consumers and remaining implementation gates

The two initial contract bundles cover account-link review and governance audit
only. They do not complete Staff onboarding. Additional fixed templates must be
added as separately named/versioned bundles with their actual consumers and
scope tests, rather than broadening either existing bundle.

| Boundary                                              | Current source requirement                                                                                                                                                              | Required integration before full acceptance                                                                                                                                                |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Ordinary assignment proposal read/review              | `assignmentAccessFor` and asynchronous `decisionScopeStatus` require `assignmentAdministrator`                                                                                          | Exact hospital scoped-access reviewer, rechecked both at decision and later owner verification settlement; preserve target/proposer separation and product prerequisites                   |
| Person findings and identity-source review            | `personReviewTransaction` now has a candidate explicit owner/grant union; ordinary `identityScopeTransaction` retains reviewed Staff                                                    | Exact nonclinical person-review governance actor; current native human/membership, contributor exclusions, protected document/evidence checks and audit; no invented Staff link            |
| Employment preparation/review                         | Targeted commands, separate authority discovery and WF0107 inbox have candidate current-governance alternatives; selected findings/draft authoring and remote UI acceptance remain open | Separate employer-bound nonclinical preparation and review authority with explicit governance provenance in audit/receipts; retain evidence and independent-person rules                   |
| First employment activation                           | WF0106 candidate admits the scoped activation/enrollment pair or IAM's exact-Staff governance activation writer; current-head acceptance is pending                                     | Employer-bound nonclinical activation authority retaining current approved person/employment and native account checks; no operating authority follows from the activation decision itself |
| Hospital department posting and pharmacy relationship | `scopedPostingTransaction` requires `StaffActorUse` and `WithCurrentStaffActor`                                                                                                         | Complete the reviewers' own reviewed employment first, then ordinary reviewed posting/relationship assignments; preserve Staff, department, professional and product guards                |
| Independent pharmacy/supplier operating duty          | `operatingAdministrationRole` and Workforce `employerAccess(...,"manager",...)` require legacy administrative roles/access                                                              | Exact employer-scoped operating-access reviewer through both IAM and Workforce source checks; retain target identity/location eligibility and subject/proposer separation                  |
| Legacy privileged enrollment                          | `memberHandlers.grant` admits known roles                                                                                                                                               | Coordinated admission change preventing new privileged rows from bypassing reviewed delegation, preserving first-owner onboarding and safe retained-row repair                             |

Do not broaden `assignmentAdministrator`: its callers include customer workspace
onboarding and owner MFA settings, which must stay owner-only. Add separately
scoped admission for assignment consumers. A decision awaiting asynchronous
Workforce or Organization verification must revalidate its delegate's authority
at settlement; an old decision cannot become effective after revocation.

### Native hospital assignment admission and settlement — implementation target

The new regression `TestGovernanceAssignmentProposalAdmissionUsesExactHospitalWithoutLegacyManager`
describes this consumer's admission gate. Its initial real-service run rejected
an unknown template with 400 instead of 202. The candidate now implements the
new admission and private settlement path, but has not passed that acceptance
gate or been merged/deployed. The fixed hospital-only template is
`iam.scoped_access_reviewer.v1`, containing `iam.governance.assignment.manage` and
`iam.governance.assignment.review`. Its authority kind is nonclinical access governance.
The existing current-owner or exact-grant, credential, membership-incarnation,
owner-count and canonical topology requirements apply. A provisional grant
remains subject to the accepted single-owner policy and independent review;
neither the grant nor an approved operational assignment fabricates Staff,
professional eligibility, a posting or a hospital-manager access row.

The separate candidate route family is
`/v1/iam/hospitals/:hospital/assignment-proposals`. A UUIDv7 child identifies the
canonical proposal, with its existing history, decisions and decision command.
The selected hospital must equal the proposal's actual hospital on reads and
writes. Expected tenant and subject headers remain preconditions on the verified
principal. The legacy owner-only family, onboarding and owner security helpers
retain their existing admission. Native admission must preserve canonical input
validation, target membership epoch, proposer/target separation, immutable
origin/history, compare-and-set versions, original-command replay and snapshot
projection. A new governance template alone must not enable the legacy family.

Native commands retain their original governance source in a private request
ledger bound to canonical proposal/history versions, binding owner membership or exact grant/version,
actor membership incarnation and selected hospital. Public proposal/decision
payloads must exclude this proof. A different source must not adopt a retained
command. Proof retention, provenance immutability and scoped database admission use
candidate IAM0049, with fail-closed support checks; no deployment of that
migration is authorized by this design.

The native Workforce verification protocol must have separate versioned event
subjects. It reports canonical hospital existence/type for the exact proposal and
request; it does not redefine the legacy `Eligible` result, carry bearer
authority or create a legacy manager. Organization's current department check
and both-owner conjunction remain required for department proposals. Each answer
is retained once for its request. Stale, duplicate and wrong-scope answers cannot
settle another proposal or later request. The guarded settlement also holds Organization's current active department path through commit before a positive transition; an old positive answer cannot stand in for that current resource proof.

Settlement must acquire fresh IAM authority before locking the exact pending
proposal. Existing event handlers already own a transaction, so a native
settlement must not nest a guarded command there. Use the consumer's retryable
after-commit phase to enter a fresh guarded command, compare the original source
identity and current owner answers, then perform the canonical transition and
outbox publication atomically. The after-commit phase must also run on redelivery
after an event is already claimed. Revoked, expired or replaced authority returns
the proposal to review without creating a snapshot; unavailable authority leaves
it pending and retries. Successful settlement is the approval pivot. Subsequent
projection retains existing snapshot/version/scope-epoch and target-membership
checks; this design does not silently add reviewer dependency to already
effective assignments. Withdrawal and target/source lifecycle revocation preserve
their existing histories and immediate denial rules.

Reuse the capacity, five-second authority budget and latency targets below.
No new infrastructure is needed. Rollback must retain admitted proposals,
decisions and owner answers and disable new native admission; do not drop proof
ledger or relabel native rows as legacy commands. Release evidence must include
actual PostgreSQL/FGA/broker tests for independent decisions, both department
answer orders, duplicate/stale answers, source revocation/replacement between
decision and settlement, dependency outage/retry, rollback and immutable private
provenance. Admission tests alone do not complete this consumer or the full
Staff/pharmacy journey.

Governance actor provenance must be represented explicitly. Never supply nil,
fabricated or borrowed Staff/link IDs to make existing HR audit or RLS contracts
appear satisfied. HR governance access is limited to the named nonclinical
purpose; it does not exempt operational product consumers from employment,
posting, licensing or narrower resource checks. Lists and capability discovery
must use the same current authority as the corresponding writes.

## Capacity and operational assumptions

Reuse the existing 50/150-tenant planning envelope at 12/36 months, 100 Staff per
tenant, 10 peak requests/second, 90:10 reads:writes and 4 KiB command limits.
At 50 privileged grants per tenant/year, this is 2,500/7,500 grants per year;
12 history/notification rows per grant at roughly 1 KiB are about 30/90 MiB per
year before indexes and audit. One existing Postgres writer is sufficient.
Target 99.9% availability, bounded five-second authorization, p99 read 300 ms and
write two seconds. These are design targets, not measured production results.
No additional service, broker, cache, worker fleet or standing infrastructure.

## Actor and template rules

| Actor                 | Authority and scope                                                              | Preconditions and approval                                                                                                            |
| --------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Issuing owner         | Tenant governance, exact selected employer root                                  | Current verified tenant/session, native human, current owner membership and credential watermark; cannot issue to self                |
| Account-link reviewer | Fixed `workforce.account_link_reviewer.v1`, one hospital or independent employer | Active verified tenant-owned human and ordinary membership; governance intentionally does not depend on the Staff link it must review |
| Governance auditor    | Fixed `iam.governance_auditor.v1`, tenant governance review only                 | Same identity/membership requirements; may not approve a grant they issued or receive                                                 |
| Staff member          | Ordinary membership; no operating authority from this flow                       | Recipient completes private verification/password; reviewed Staff link, employment, postings and product grants remain separate       |

Templates compose code-declared permissions only. Account-link review grants
minimal directory identity/link disclosure and independent review, not broad HR,
payroll, patient, prescribing, dispensing, stock or financial authority. The
auditor reads/reviews governance evidence, without issuing grants or editing Staff.
No tenant-defined permissions, arbitrary role strings or global user adoption.

## Admission, review and notification

Use a canonical IAM governance ledger separate from operational assignment
snapshots. It records immutable command/contact-free intent, target membership
incarnation, canonical template/scope, effective interval, issuer and explicit
provenance. A UUIDv7 command and identical input replay the original receipt;
changed intent conflicts. Versioned decisions require the observed version.

For one current owner and a single-hospital or independent-employer tenant, a
valid privileged grant takes effect as `provisional_active`, records that it
requires independent review, and creates durable in-product notifications for
every current owner in the same transaction. The owner dashboard shows those
notifications and the review queue. Notification evidence means an inbox entry
was recorded; it must never claim an email was delivered. A subsequent auditor
may approve another person's grant or reject it, never self-approve.

Multi-hospital tenants require at least two current owners and independent
approval before effect. Topology/owner state that cannot be verified denies
admission. The hospital-creation boundary must enforce the same owner-count
invariant so a single-owner tenant cannot become multi-hospital while retaining
an invalid exception. No native owner-account creation or recovery bypass is
included; the established verified support path remains responsible for that.

`active`/`provisional_active` never override valid-from, expiry, current membership,
credential revocation, employer state or exact scope. Rejection, owner withdrawal,
revocation and membership ending deny subsequent commands immediately while
retaining admitted-command, notification and decision history. Reviewer authority
can be bootstrapped for two distinct auditors if each other's grants need review;
there is no hidden system reviewer or simulated human decision.

## Source enforcement and projections

Add one fixed governance-delegate routing role. Its asynchronous FGA projection
is coarse route admission, not scope authority; `staff_member` remains role-free
of business permissions. Every affected Workforce read/write also requires a
current IAM governance grant for the exact action and employer.

Reuse the existing `tenantdb` guarded-command composition and ADR-0052's closed
authorization-provider seam. IAM locks its current grant, target membership and
credential authority; the owning Workforce implementation joins the command and
locks its exact Staff/link source before returning or changing it. No cross-module
table reads, SQL callbacks, product-private grant vocabulary or HTTP under locks.
Workforce's narrowly scoped login-link purpose must cover these RLS reads/writes
without adding general `workforce_staff_allowed` manager access. Canonical scope
verification is supplied by its owner, never inferred from a UI option or ID.
Lock order stays IAM authority, Workforce source, Organization/employer gate and
product resources; all source writers must preserve the same order.

Audit, retained commands, review state, owner notifications and projection intent
commit atomically. Use the existing transactional outbox; no publish-after-write
window. Projection duplicates/staleness cannot revive a source-denied grant.
Authorization or audit failure rolls back/denies. Identity/topology source outage
returns 503; missing route permission 403; invisible foreign scope/object 404;
stale or changed command/version 409. Keep current v1 error envelopes.

## Ledger admission and projection composition

The routing projection reuses IAM's existing `MemberGranted`/`MemberRevoked`
transactional outbox and current-membership reconciler for one fixed
`governance_delegate` role. This role carries only coarse route admission;
ordinary membership, exact current source grant, scope, interval, provenance and
credential checks remain necessary at every consumer. The legacy role API cannot
create this managed routing membership. Native identity eligibility is observed
before acquiring database locks. Replaying a retained creation receipt does not
repeat native provisioning or claim the grant is still effective.

IAM owner membership changes take an exclusive owner-count gate. Governance
commands hold its shared counterpart before querying current owner membership,
then take selected governance row locks in sorted UUID order, target membership
locks and the Workforce topology gate. Auditor review locks the union of its
bounded candidate source grants and target before taking membership locks; it
revalidates the source after locking. This prevents opposing reviews from taking
target/source locks in opposite orders and avoids inversion with revocation's
routing-membership removal. The private audit purpose is set only after current
source validation and remains inside the guarded transaction. Source expiry
bounds the guarded command deadline. Owner inbox reads remain recipient-only;
auditors may insert transition notices for current owners but cannot read them.
Native eligibility for approval runs between preflight and final locked
reauthorization. Approve/reject excludes issuer and recipient; a current owner
may revoke its own issued grant. Decisions retain their original receipts,
source version and membership epoch; creation evidence remains immutable. Under the
owner-count gate, read owner membership without row locks: an owner removal may
already hold its tuple before entering its row trigger, and waiting on that
tuple while retaining the shared gate would invert the lock order. Recheck the
actor's credential watermark and keep target incarnation and source locks until
commit. A target already holding owner membership does not need a subordinate
governance delegation.

Store original receipt bytes as immutable creation evidence separately from the
mutable current status/version. The same command and intent returns that original
receipt after later independent decisions; changed intent conflicts. Persist one
durable notice per current owner in the same transaction as grant, audit and
routing projection intent. A notice row establishes inbox delivery only.

## Candidate topology source implementation

The draft now includes Workforce's `GovernanceTopology` provider and candidate
migration `0102_workforce_governance_topology`. It returns only the canonical
hospital count while verifying the exact requested employer kind/ID inside a
live, attributable tenant command. The platform binds a sole Workforce owner;
missing/duplicate owners and unavailable sources deny. It grants no permission.

The owner function retains a shared tenant topology advisory lock through the
consumer callback. A statement trigger acquires the exclusive counterpart before
any hospital-root insert/update/delete row locks. This covers existing hospital,
pharmacy, supplier and onboarding writers without cross-module table reads.
It prevents a count observation from racing those writers; it does not yet
implement IAM's owner-count policy or block second-hospital admission itself.
Those gates and the actual governance ledger/consumers remain required.

This additive migration creates two functions and one trigger; it changes no
rows, keys, owner identities or native accounts. Production application remains
unapproved. Rollback first disables all consumers of this guard, then removes
`workforce_governance_topology_write` from `workforce_hospitals`, drops its
zero-argument trigger function, and drops
`workforce_lock_governance_topology(boolean)`. Retained grant/review evidence must
not be removed. A consumer left running without the function must deny on the
source error. Scratch-database migration/rollback and full owner/topology race
verification are required before requesting live approval.

## Contracts, rollout and acceptance

Define canonical generated Go/Zod command, receipt, fixed-template, review and
notification contracts before exposing owner endpoints. All browser business
calls enter Global API. Lists are cursor-paginated with bounded counts; notices
are disclosed only to their current owner recipient. No email, proof, password
or private employment evidence belongs in events or public grant receipts.

An additive, separately approved migration must create RLS-protected governance,
command/decision history and owner-notification storage with immutable actor and
scope checks. Do not mutate the historical independent-approval ledger. Keep the
new consumer disabled until schema, graph model, owners and consumers are tested
and promoted together. Rollback stops new delegation and denies its consumers;
it does not delete identities, decisions, notifications or completed Staff links.

Required tests include real PostgreSQL RLS/outbox and FGA projection; concurrent
owner/topology changes; single/multiple owners; distinct-person review; all owner
notices and notice privacy; duplicate/changed/lost-response commands; expiry and
both revocation/command orderings; stale/missing graph and unavailable owners;
callback/tenant/database/scope escape; hospital and independent-employer positive
and sibling/foreign denials; exact existing-owner compatibility; and no new
clinical/stock/financial permission. Observe critical assertions fail under
mutation. Compile/unit/build locally; database/E2E execution stays in remote CI.

Live acceptance uses recipient-operated private account setup, real distinct
reviewers and the existing tenant. Verify reviewed links and scope revocation
before employment, posting and pharmacy operating tests. This document and a
successful ordinary invitation do not complete those gates.

The candidate now provides a shared synchronous IAM source guard in
`platform.Deps.GovernanceSources`, with only account-link review admitted by its
closed capability catalog. Auditor decisions and product source use share the
same locked source validator. The ordinary source callback does not enable the
private auditor read purpose. Source-use audit shares the product transaction;
source evidence cannot be reused as later or asynchronous authority. Workforce
consumer adoption and remote acceptance remain unfinished.

Account-link consumer policy: current owners use an explicit source carrying the
actual IAM owner-membership ID and membership epoch; recipients use the exact
current account-link governance grant. Both retain session/credential and canonical
employer gates. Maker/target/known-person independence remains a separate owner
rule. General manager permissions or private directory rows cannot substitute for
a review source. Existing proposal/change administration remains separately
authorized. A selected Staff/link purpose must permit identity checks and retained
source evidence without granting generic directory, employment or stock access.
Hospital and independent wire responses remain unchanged; private retained
provenance records source kind, source ID/version, actor membership incarnation,
owner count, employer, target Staff basis and reviewed link version.

The next candidate adds hospital-only person preparer/reviewer templates and an
explicit reviewed-Staff versus owner/grant authority union for selected identity
source/person review operations. WF0104 retains source proof in private review
history and fences evidence masters against writes under that purpose. Protected
file release revalidates the exact authority after storage I/O. Employment and
activation remain unchanged. Hospital/person authority discovery, target/inbox browse
and the review workspace now consume this union in the candidate described below.
Remote acceptance and the remaining employment/activation consumers are required
before this candidate resolves first-reviewer onboarding. No live approval for WF0104 has
been requested or inferred from earlier migration approvals. Remote owner/grant,
provenance and storage-cutoff evidence remains required.

Person authority discovery uses a separate `GET /workforce/person-review-authority-scopes`
route. It keeps legacy actor-link discovery and its cursor meaning unchanged.
The Workforce owner scans at most 20 canonical hospital roots plus one probe,
then reauthorizes each preparation/review capability through current reviewed
Staff authority or the exact person-governance source. Only authorized names and
capabilities leave the server; filtered empty pages retain opaque continuation.
Root creation time/ID orders the encrypted new cursor, sealed to route, tenant and caller.
The existing AES-GCM cursor sealer hides denied root anchors and rejects tampering;
missing sealer configuration denies the new route before scanning.
One five-second/idle budget and four workers bound authorization. Native session
and route permission refresh happen before parallel checks, never on a shared
Gin context inside workers. Capabilities are hints; commands repeat current
source checks. Target/inbox browse must use the same authority and a read-only
hospital purpose without inventing Staff/link references.

### Employment source guard implementation — continuation candidate

The full goal remains Staff initiation, independent findings and employment,
reviewed account linking, operating assignments and actual pharmacy access.
The employment consumer is still the next integration boundary; this addition
implements its IAM source/basis guard, not a working first-activation endpoint.

IAM now declares separate hospital-only templates for
`workforce.employment_preparer.v1`, `workforce.employment_reviewer.v1` and
`workforce.employment_activator.v1`. Their respective permissions/capabilities are
`workforce.employment.prepare`, `workforce.employment.review` and
`workforce.employment.activate`. An account-link, person-preparer/reviewer,
auditor, department, supplier or pharmacy source cannot substitute. These are
nonclinical source decisions, not dispensing, clinical privilege, posting or
operating authority. Existing owner-count, distinct-person approval, notice,
effective interval, membership and credential requirements still apply.

`GovernanceEmploymentActivationGuard` adds a callback-only IAM basis writer
selected by the authenticated account, canonical hospital and exact Staff ID.
Its selected scope is copied before authority work. It takes the exclusive
`iam.employment:<tenant>:<hospital>` gate before governance-owner and credential
locks, matching the existing scoped activation lock order; source membership,
grant and topology checks then remain held through product work. The writer is
bound to its issued command context and loses authority on callback exit.
Product failure rolls back basis, history and IAM audit with the same transaction.

A target must retain actual `staff_member` membership. The additional
`governance_delegate` routing role is permitted alongside it, so a first reviewer
can later become employed; that role alone never substitutes for ordinary
membership. Owner, clinical, operational, unknown and duplicate role combinations
remain inadmissible. Basis consumption is not an assignment or operating grant.
The candidate IAM0048 migration also permits source-managed governance routing
to be added after employment activation. The original managed-source insert guard
continues to reject direct routing-role insertion; ordinary employment still
blocks legacy owner and clinical role elevation. Adding routing membership
preserves the existing membership incarnation and increases its member count;
it does not manufacture a new epoch. Tests cover both onboarding orders.
The product still must check its exact current approved person/employment source,
selected Staff/version/generation, independence and expiry before using the writer.
No generic owner-to-Staff alias or synthetic actor/link ID is introduced.

The canonical catalog generates Go and browser contracts together; generation
rejects missing names, unknown metadata, duplicate scopes and wildcard or
malformed permissions before writing either artifact. Preparation and review
permissions provide coarse routing only; candidate Workforce submission/review
consumers separately validate the exact owner/grant source. Activation has not
yet adopted this union, and a catalog grant does not establish a completed
employment workflow.

Candidate WF0105 retains the exact nonclinical employment source on each review
revision. Existing scoped Staff authority remains a distinct tagged alternative.
The new purpose freezes existing draft terms/evidence and supports independent
review, retained reads and private evidence release; it does not authorize general
HR drafting or document-master writes. Purpose policies restrict all current
Workforce tables, including inherited owner/manager authority, to the selected
Staff/review and required source records. Only review/history/frozen children,
the owner-maintained cohort marker and attributable review audit may be written.
Immutable-source validation precedes trusted proof recording, which precedes
history capture. The Go consumer verifies the matching enabled recorder and
source/history guards, forced tenant RLS, purpose/delete policies and absence
of runtime TRUNCATE privileges before disclosing a target. A new tenant-scoped
Workforce table without those policies fails closed until its owner adds them. Storage release rechecks the same source
and frozen basis after I/O. No source fallback is permitted after a business
callback or dependency failure. This is an unapplied candidate with remote
acceptance pending; live WF0105 approval and rollback evidence remain required.

Local acceptance covers separate hospital-only capabilities, exact target and
context lifetime, ordinary membership versus nonclinical delegate membership,
clinical/owner rejection and canonical/browser parity. A real PostgreSQL
bootstrap test covers owner and exact delegated sources, retained unrelated
routing after source revocation, exclusive basis locking, writer escape and
atomic product-failure rollback. It must run in remote CI; compile/unit evidence
does not establish its database acceptance.

Before merge or deployment: verify the new preparation/review consumers and
connect first activation with purpose-bound data access and attributable write
provenance; add authority-aware employment discovery and UI; revalidate assignment
decisions and async settlement; complete operating review, second-hospital
owner-count admission, privileged legacy admission and real recipient-operated
Staff/pharmacy tests. No new live rollout or schema approval is inferred here.

IAM migration `0048_iam_governance_employment_bases` is an additional unapplied
candidate. It records immutable private owner/grant evidence against each
resulting basis generation and checks the current IAM source identity, version,
scope and membership epoch. It neither backfills evidence nor rewrites prior
basis/history rows. The typed writer requires the recording table and its enabled
matching trigger before admitting the product callback. A disabled/missing
recorder is unavailable, rather than a successful unattributed activation.
Rollback disables this new consumer and retains provenance/history. New IAM0048
live approval is required separately after database/rollback acceptance; no
production migration or rollout was performed here.

Legacy scoped Staff identity-source reads retain their optional workspace-header
behavior. Supplied mismatched or ambiguous expectations still fail. Governance
fallback requires both exact tenant/account headers, so this compatibility does
not admit owner/delegate operations with an unbound workspace. Staff-source audit
keeps an explicit `scoped_staff` kind alongside the six existing metadata fields;
no clinical or personal values enter that read audit.

## Candidate first-hire activation adoption

WF0106 separates activation from review purpose. The command uses the real scoped
activation/enrollment bundle or a current owner/exact activation grant. The latter
must enter IAM's exact-Staff typed activation callback and consume its writer
inside that lifetime; it cannot use generic source authorization for IAM writes.
The private generation proof binds source, actor, hospital, Staff, review,
generation, command and expected Staff version/epoch. A late Staff UPDATE fence
permits only inactive→active plus the owned version/epoch/timestamp changes. The
existing frozen-source generation guard and deferred receipt remain mandatory.
Purpose policies deny all other Workforce writes/deletes, even under inherited
owner/manager authority. Prerequisite checks require recording/transition/receipt
support, forced RLS, purpose/delete policies and no runtime TRUNCATE privilege.

The source API is readonly; it cannot consume a writer or update Staff. No fake
Staff/link or HR-intake context is manufactured. Callback/dependency failures do
not switch sources, and a shared five-second budget bounds the command. All
current-head database/replay/rollback acceptance is pending. No runtime
release flag changes are included, and native account linking, scoped operating
approval, settlement, UI and pharmacy acceptance remain unfinished.

### Employment discovery and inbox continuation boundary

The targeted WF0105 review consumer does not make a first reviewer able to find
its hospital, discover a pending review, select a person finding, or prepare the
underlying draft and released evidence. The existing employment scope and inbox
handlers still require a real linked Staff actor. The separate candidate
`employment-review-authority-scopes` route discovers current owner/exact grant
capabilities without changing the linked-Staff route; the candidate UI now selects
its discovery/inbox path explicitly, with remote acceptance pending. Do not treat direct-ID review
acceptance as completion of this user journey.

The continuation must preserve these separate sources end to end:

- Retain the existing scoped-Staff discovery contract and actual reviewed actor.
  Add current owner/exact employment preparation or review grant discovery
  without synthetic Staff/link/review identifiers. A capability list is a
  current-source read; a selected hospital is reauthorized before each operation.
- Bound hospital candidates, inbox pages and finding selection by the existing
  capped pagination and a shared five-second source lifetime budget. Perform
  current-source admission before disclosing names or summaries; bind encrypted
  cursors to the route, verified tenant/account and relevant filters. A candidate
  hospital ID, route header or previous page is not authority.
- A private browse purpose may reveal the selected hospital's minimal review
  summaries and the Staff labels needed for those summaries. It must not reuse
  an exact-review proof with invented target IDs. Its source, actor, capability,
  operation and employer remain explicit and auditable. Selected details,
  documents and finding content continue to use their exact target guards.
- Preserve forced tenant RLS and restrictive update/delete checks. Browsing must
  not acquire master, employment decision, payroll, clinical or stock writes,
  including when the same account also has inherited manager access. No proof
  snapshot or client capability flag authorizes a later mutation.
- The reviewer UI must obtain its choices through Global API, clear selected
  private data on account/tenant/source changes and expose dependency failures.
  Exercise owner and delegated no-Staff paths, ordinary scoped Staff, empty
  results, paging, stale/revoked sources, wrong hospital/tenant and recorder or
  policy failures before claiming the journey complete.

This is the next implementation boundary, not a claim that discovery, inbox,
authoring, UI, pharmacy access or deployment is accepted. WF0106 activation still
requires current-head real-database acceptance independently of this continuation.

### Candidate employer inbox purpose — WF0107

The distinct `employment-review-authority-inbox` candidate returns the existing
minimal employment summary contract under a current owner/exact preparation or
review source. It preserves the linked-Staff endpoint and cursor semantics. Its
page has at most fifty records plus one continuation probe, a five-second/idle
deadline and no external storage I/O. Current IAM authority remains held through
query, minimized source audit and transaction completion.

A separate private `employment_inbox` proof contains real source, employer and
actor evidence, with no Staff/link/review actor aliases. WF0107 adds only the
necessary hospital/review/Staff-label reads and exact audit insert; restrictive
purpose/delete policies deny unrelated Workforce disclosure and master writes
even when the actor also holds manager access. All read/audit prerequisites,
forced RLS and absence of runtime TRUNCATE privilege are checked before browsing.
Candidate WF0108 builds the tenant/hospital/Staff lookup index concurrently;
reads require a ready/valid index. The existing hospital/status index still
serves the inbox page. Query-plan/load acceptance remains pending remote review.
No grants, clinical/stock actions, employment changes or account links result.

The source review includes an additive sealed query-list adapter that reuses the
existing resource/filter cursor scope. Hospital, mode, recorded status, verified
account/tenant and route are bound before a scan; the two older filtered-list
constructors retain their current cursor meaning. Local cursor tests cover
foreign resources, changed filters/accounts and continuation. Source/proof and
strict SDK tests pass locally. Real database policy, no-Staff owner/delegate,
revocation and fault cases are pending remote CI; no live migration is applied.

This does not complete finding selection, selected terms/document authoring or
the reviewer workspace. They must adopt current source authority before this
path is exposed as a complete first-reviewer workflow.

### Candidate employment reviewer workspace adoption

The page initially selects hospital governance and allows an explicit switch to
reviewed Staff browsing. Each selection uses its own registered discovery/inbox
routes; query keys include the path as well as tenant/account/resource/filter and
cursor. Switching unmounts private detail and paging state. Busy decision,
unsaved form and private-download states disable the switch. No dependency or
permission error falls back to the other source. Exact selected-review commands
continue to reauthorize their existing tagged source union; the browser selection
is not authority and cannot bypass independent-person checks.

The initial regression failed on the old linked-Staff-only page, then passed with
the candidate wiring. Fourteen component tests cover the new governance path,
explicit Staff path, clearing selected detail, 403/503 discovery failure without
fallback and existing decision/uncertain-retry behavior. This local evidence is
not real PostgreSQL/FGA or browser acceptance. Finding selection and draft
terms/document authoring remain the next integration boundary.

Remote CI on `8de7569a` rejected the inbox migration's extra permissive policies
before database/browser tests could start. The corrected candidate extends the
existing single permissive read/write policy, with restrictive purpose/delete
fences unchanged. It does not weaken the global RLS validator. The runtime
preflight and fault tests now require the inbox source in the existing read/audit
expressions. Current-head migration and source acceptance must be rerun remotely.

### Selected governance person-finding preparation

The next candidate adds a separate `employment-person-finding-options` route on
an exact hospital/Staff resource. It reuses the canonical finding summary and
basis input without changing linked-Staff route/cursor behavior. Current IAM
hospital employment preparation authority is required on both candidate scan and
filtered disclosure; a reviewer, person preparer or supplied Staff ID is not an
authority substitute. Both original tenant/account assertions are mandatory.
Sealed cursors bind the actual Staff resource, hospital, expected Staff version
and employment epoch as well as caller/tenant/route. Pages have at most twenty
candidates plus one continuation probe. The retained source cannot change between
scan and disclosure, and each source command has a five-second/idle lease.

A new selected-Staff `employment_finding_selection` proof retains real owner/grant
provenance and expected basis, with no invented employment-review ID or actor
Staff/link. Its private purpose permits only the selected Staff/profile, current
person findings and their frozen identity evidence metadata/classifications and
creation provenance. The existing finding/evidence freshness checks run before
returning each option. Only an exact minimized read audit may be inserted; all
master/decision/terms/document writes and deletes remain denied even with manager
access. Existing selection indexes serve the query; required read/audit support,
forced RLS, purpose fences and index readiness are checked before reading.

This requires candidate WF0109, remains unapproved for live application and must
pass real PostgreSQL source, evidence, policy-failure and revocation tests. Binary
rollback disables this consumer and retains audits; removing schema support
requires capturing/restoring prior permissive expressions before dropping the
new functions/fences. No Staff, identity, findings or audit data is erased. This
finding selector does not itself authorize draft terms or document authoring;
those must acquire their own exact preparation consumers and retained provenance.

### Candidate hospital governance terms preparation

Separate `employment-preparation-source` and `employment-preparation-periods`
routes on an exact hospital/Staff resource use current owner or exact hospital
employment-preparer authority. Source, list, selected read, selected history and
save are closed registered operations. Original tenant/account assertions and
native membership/source verification are mandatory, with one five-second/idle
lease per command. There is no fallback to linked Staff or a coarse legacy role.
The public input/source/draft/history models are unchanged canonical Workforce
contracts. List cursors bind actual hospital/Staff, history cursors additionally
bind actual period, and both bind caller/tenant/route/lifecycle filter. The prior
Staff and independent-pharmacy routes and cursor behavior remain unchanged.

A typed save-authority closure receives the real normalized canonical command
only after decoding/validation. It preserves the existing digest/receipt, actor,
optimistic version, terminal withdrawal, Staff basis, active sponsor, supervision
lock and owner-kind checks. The actual v7 period path and command/revision ID are
retained; no review, period or actor IDs are fabricated. A replay must have the
same retained source identity and payload, under newly verified current authority;
lease/authentication timestamps can refresh. A legacy receipt without this
provenance cannot be rebound to a governance source. A new revision retains its
own source without rewriting earlier contributor history.

WF0110 adds a private selected-operation purpose and restrictive delete fence on
every Workforce table. Only the exact hospital kind, selected Staff and supplied
same-hospital active sponsor, selected draft/history and exact private audit are
permitted. The only master write is the exact selected draft command; canonical
triggers retain history. It grants no Staff, document, evidence, withdrawal,
review decision, login, posting, operating or clinical authority. The existing
single permissive policies are extended; the global RLS validator is unchanged.
The retained period guard's exact enrollment predicate is extended only for this
selected command. Migration fails if the expected current withdrawal/sponsor
checks are absent; it does not recreate an outdated guard or globally broaden
`workforce_enrollment_allowed`.

An AFTER history-insert trigger atomically retains private source provenance in
the immutable audit ledger, using the real revision ID as its audit resource.
Only the canonical history snapshot's trigger depth admits that provenance write.
It stores source identity, basis and input hash, not duplicated terms/reasons or
file contents. Normal operation audits also retain the exact private authority;
legacy audit shape stays version-only. Replay looks up the real revision under
current target scope and compares retained source identity before accepting the
canonical receipt. WF0111 creates the plain audit receipt lookup index
concurrently. Before any preparation read/save, preflight requires valid/ready
indexes, actual guard/snapshot/history/provenance triggers enabled with their
expected functions/events, required read/write/audit expressions, forced RLS,
purpose/delete fences and no runtime TRUNCATE.

Local proof/replay tests were observed failing before implementation, then passed
with `-race`. The expanded real PostgreSQL fixture now prepares the very terms
consumed by the employment review through the no-Staff governance route. It also
covers exact save/edit/replay/conflicting input/stale basis, selected read/history,
wrong source, original workspace mismatch, disabled provenance trigger, retained
read-purpose write denial, missing legacy provenance and revoked preparation
source. These first compiled before runtime acceptance. The later native binding
acceptance and SDK/UI continuation below supersede this checkpoint; downstream
staffing/access and complete live acceptance remain unfinished.

WF0110/WF0111 are not approved for live application. Required evidence includes
remote migration/source/atomicity/retry/negative tests and bounded query plans.
Binary rollback removes the new consumers while retaining policies, functions,
indexes and immutable source audits; old paths see an empty new-purpose GUC and
retain their previous checks. Do not delete provenance or reverse the retained
history. Any later DDL removal needs a captured prior guard/policy definition and
specific reviewed migration/rollback approval.

#### Terms locking-read correction and strict SDK

The first remote terms candidate reached the first real save and returned 404.
Canonical Staff/sponsor basis reads use `FOR SHARE`; the retained restrictive
Staff update guard predates this purpose and can hide those rows despite a
permissive SELECT extension. The correction extends only that guard's USING
predicate with the exact selected Staff or actual active same-hospital sponsor.
It preserves the prior explicit WITH CHECK, and the separate purpose fence still
has false Staff write checks. A dedicated helper is shared by that read extension
and the purpose visibility. Preflight requires the locking-read extension, and
the handler verifies its installed SQL proof before any canonical operation. The
existing genuine-read/master-write-denial probes remain required. The real
activation fixture additionally supplies its actually activated Staff as an
active contractor sponsor, with sibling-hospital and self-sponsor negatives.
This correction needs current-head PostgreSQL acceptance; no write check or
source assertion was relaxed to make the first failure pass.

The new strict SDK exposes source, bounded draft list, exact draft read, history
and save through the Global API. It reuses canonical source/draft/input schemas,
requires hospital-only scope with original account/tenant and real v7 command/
period IDs, rejects a command with no representable next revision before I/O,
and validates returned owner/Staff/receipt basis, actor/version/command and page
bounds/duplicates. History distinguishes period ID, revision ID and version.
Private response fields and mixed pharmacy/intake authority inputs fail closed.
Requests are no-store and cancellable before/after I/O; 403/503 failures have no
legacy or linked-Staff fallback. Thirty-three local SDK tests pass after an
initial missing-module red. Type checking caught a test table flattening its row
array; corrected cases were rerun as actual duplicate/foreign arrays. Full
workspace and current-head remote checks were still required at that SDK-only
checkpoint. The implemented authoring/binding continuation below records the
subsequent evidence; no individual SDK suite proves the full workflow.

### Employment preparer target selection

Workforce owns Staff and the minimal `PersonReviewTarget` projection. A current
native owner or exact hospital employment-preparer grant may search that
hospital's targets without linked Staff, person-preparer or directory authority.
The separate read-only POST `employment-preparation-targets/search` accepts the
canonical employee-code search body (empty code browses). No employee code enters
a URL. Max 20 rows plus one probe, one retained source, a five-second lease and
original tenant/account headers apply. Sealed cursors bind normalized body,
account, route, tenant and actual hospital before any scan. Missing sealer/source
or database proof returns 503; missing permission 403; delegates receive the same 403 for unknown and out-of-grant hospitals, while an authorized owner receives 404 for an absent hospital.

Planning envelope remains 100 Staff per tenant, 50/150 tenants at 12/36 months.
At 10 peak searches/s, <=1 KiB input and <=20 minimal rows, this reuses the owner
Postgres and existing tenant/hospital keyset and casefold employee-code indexes;
no new service, index or cache. Target p99 is 500 ms within the shared five-second
failure ceiling; runtime query/load evidence remains an acceptance gate. Source
failure discloses no partial rows. Private proof omits Staff/period/command IDs
for this operation only; selected operations still require actual Staff IDs.
No HR master, draft, document or operational write is authorized. Every served
row records a minimized version-only read audit in the same transaction.

Candidate WF0112 extends the exact draft read purpose, retaining all prior write,
delete, source and tenant fences. Rollback disables the new API consumer while
preserving the append-only migration and audit; do not delete historical proof.
Legacy person-target discovery is rejected as an alternative because it confers
a different authority. Local pure cursor/proof tests precede implementation;
remote real owner/grant/revocation/RLS tests precede rollout. This is part of the
full workflow, not permission to merge an unfinished UI or deployment.

The authoring UI reuses the existing canonical terms form with two explicit
save/sponsor consumers. Governance discovery `can_submit` gates its entry,
original account/hospital keys isolate all queries, and selected Staff version is
checked against a current source before editing. No general directory or legacy
intake source is retried. Draft command/period IDs survive uncertain writes for
exact replay. The recipient and operational workflows remain separate gates.
Selected governance document authoring is implemented in the candidate below;
exact terms binding is implemented in the subsequent native binding candidate.
Full workflow acceptance remains unfinished; this is not merge-ready.

### Selected employment document preparation

Workforce remains the file/Staff owner; the existing attachment store, restricted
scanner worker and canonical document engine own quarantine/verdict/release. Add
a separate hospital `employment-preparation-documents` family for current native
owners/exact employment preparers. The canonical document input/output is reused;
only enrollment records for the selected Staff and category employment_evidence
are admitted. Expected Staff version/employment epoch are strict query
preconditions, never authority. Original tenant/account headers remain mandatory.
No general record-choice, credential, qualification, identity or classification
workspace is admitted. List is cursor-bounded and sealed to actual hospital,
Staff, account, tenant and basis. Existing 10 MiB PNG/JPEG and scanner controls
remain unchanged. Planning volume/latency is the target-selection envelope above;
file transfer has the existing bounded body/storage deadlines, with each owner
phase bounded by a fresh maximum five-second/idle source lease. No database
transaction holds locks across body/storage I/O.

A closed document purpose binds actual Staff/file, basis, operation and canonical
mutation stage. The source identity is retained across all phases of one HTTP
request and revalidated after storage. Canonical mutation hooks install only the
already-validated intent, upload stamp, real quarantine receipt, revoke or retry
transition; read proof cannot mutate files or masters. A new candidate migration
extends the existing single tenant policy while adding restrictive purpose/delete
fences across Workforce tables, exact contributor writes and minimized audit.
No enrollment authority is broadened. Source/trigger/policy/index support fails
closed. Worker authority remains its existing service identity and no Staff actor
is fabricated. Released bytes require both the original source identity and
unchanged current metadata/generation after storage I/O.

Metadata replay retains canonical immutable intent semantics: same metadata/hash
returns the original file, a different intent conflicts, and replay does not
relabel original creation provenance. Each authorized command/read is attributable;
private provenance stores source identifiers, stages and hashes, never filenames,
body bytes or copied HR values. Revocation never deletes file or contributor
history. Dependency failure discloses no partial file/list and never fabricates a
scan verdict. Binary rollback disables the new consumer while retaining file,
audit and contributor history. Live migration approval follows concrete remote
upgrade/rollback and isolation evidence. The simpler legacy document/intake route
was rejected because it requires a different authority and broader record kinds.

Acceptance includes real current owner/exact preparer with no Staff actor;
register/replay/conflict, upload/scanned release, retry/revoke, expected workspace
and basis failures, other hospital/Staff/file/category denial, inherited-manager
master/verdict/write denial, disabled support and atomic rollback; source
revocation and source replacement across storage I/O; preserved worker processing
and canonical Staff/pharmacy behavior. Binding to exact terms and independent
submission/review remains a separate required part of the full authoring journey.

Selected-file SDK/UI continuation: the candidate now exposes upload, original-file
resume, manual status refresh, scan retry, verified download and confirmed
revocation from the selected Staff preparation screen. It never uses legacy
intake/Staff routes. Receipt checking rejects foreign/stale/private fields and
invalid scan chronology; binary disclosure verifies bounded size, MIME and digest.
Uncertain operations retain the exact ID/input/file or next scan attempt. Account
transition and unmount cancel active I/O and release object URLs. Private queries
include the complete Staff/version/epoch basis and discard inactive results.

Local validation: `pnpm turbo lint format:check type-check test build` passed all
30 tasks (5m54s), including 1,983 shell tests and 1,298 API tests. Focused document
client suite: 20 passed; document panel: five passed, including recovery,
revocation confirmation and registration-time access loss. The entry-point test
was observed failing before integration, and a scan-before-upload receipt test
failed before validation was corrected. An initial full check rejected an impure
render clock; the panel now reuses the existing render-safe clock. No tests were
skipped or assertions weakened. Current backend-head web/E2E checks passed, while
supplier acceptance failed a posting approval/cutoff race with 503; its diagnostic
rerun and full Go coverage remain pending. This is no evidence of full live
acceptance. No merge, migration or deployment occurred in this continuation.

### Exact native employment evidence binding (candidate; remote acceptance pending)

Workforce remains the sole owner of canonical terms revisions, document release,
evidence bindings and immutable history. The consumer is the hospital governance
employment preparer and independent employment reviewer. This slice must connect
the existing selected-file authoring UI to the existing canonical binding engine;
a parallel evidence table or a legacy intake grant is rejected. The source is a
current native owner or the exact hospital employment-preparer assignment. No
Staff actor, enrollment-manager permission, operating duty or clinical privilege
is fabricated.

Reuse the preceding planning envelope (100 Staff/tenant, 50/150 tenants at 12/36
months, maximum 20 evidence bindings per terms revision, maximum 10 MiB/file).
Metadata commands remain bounded at 4 KiB, lists at 20 plus one probe and each
source phase at five seconds or the earlier idle/source expiry. The metadata p99
target remains 500 ms; these are design targets requiring measured remote
query/load evidence. Storage/scanning uses the existing bounded owner services;
no new service, queue, cache or datastore is justified. Reads require current
authority; commands commit binding, immutable history and private provenance in
one owner transaction. There is no stale-success fallback.

The proposed separate consumer stays under
`employment-preparation-periods/:period/revisions/:version/evidence` and reuses
canonical attach/withdraw inputs and binding outputs. It must provide bounded
list, exact binding read, immutable history, attach, withdrawal and protected
content. Expected tenant/account and Staff version/employment epoch preconditions
stay mandatory. Sealed cursors bind account, tenant, hospital, Staff, actual terms
ID/revision and basis. The public response must not gain private authority fields.

A dedicated private purpose names the exact operation, selected period/version,
binding, canonical command/fingerprint and selected released file where relevant.
An attach requires the exact current draft and unchanged Staff/employment basis;
it freezes the actual terms revision ID and scanner-confirmed source/release
generations, hash and contributor evidence through the canonical engine. The
existing twenty-binding limit and unique active-file rule remain database-owned.
Withdrawal preserves all historical metadata and permits only its existing
attributable terminal transition. Read/history of a prior revision is distinct
from attaching to it: historical source versions are checked against that exact
revision rather than rewritten as the current Staff basis. Historical reads do
not permit a stale revision to be submitted or activated.

Replay checks the retained original canonical command and original governance
source provenance. It cannot adopt a legacy receipt, relabel the contributor or
turn a different source into the original actor. Current source authorization is
still required for replay. Duplicate command with a different intent conflicts.
A new private history-provenance trigger must commit atomically with the existing
canonical snapshot, without changing the public model or history shape.

Restrictive purpose and delete fences must cover every Workforce table, retain
the single permissive tenant policy and admit only selected locking reads and
exact binding/history/audit changes. Existing canonical Staff/terms/release guards
stay intact; only a closed source-specific alternative to their legacy enrollment
authority may be added. Support preflight checks require forced RLS, current
trigger functions/enabled state and page/receipt indexes. No runtime TRUNCATE or
master mutation is admitted, including through inherited manager access.

Protected content must retain the same original source across initial binding
lookup and both storage authorization phases. It rechecks attached status and the
unchanged frozen file generation/hash after storage I/O before releasing bytes.
No database lock is held across storage. A missing/changed binding or foreign
object returns 404, a missing native grant 403, a changed command/current draft
409 and unavailable authority/support 503. Source denial cannot fall back to a
Staff or intake route. The established hospital discovery disclosure rules remain
unchanged.

Before implementation, reproduce missing capability through the real native
owner/preparer fixture and write pure purpose/fingerprint tests. Remote acceptance
must exercise attach/list/history/content/withdraw/replay; changed source and
legacy receipt refusal; changed Staff, terms and released generation; foreign
hospital/Staff/period/binding/file; dropped/disabled support; contributor/snapshot/
audit rollback; and post-storage source/release revocation. Both existing hospital
Staff and independent pharmacy binding consumers must remain green. Then wire the
strict SDK and UI to select an available file, retain uncertain commands, show its
exact terms revision and prepare independent review. Binary rollback disables this
consumer while retaining canonical evidence/history and private provenance. Any
additive live migration needs its concrete acceptance/rollback evidence and the
specific approval required by the repository rules. This paragraph authorizes no
live schema change and does not represent implemented behavior.

The candidate implementation now registers that separate native binding family,
reuses canonical attach/withdraw/read/list/history operations and protected content,
and adds WF0114 purpose/provenance controls. Existing Staff/pharmacy scopes leave
the canonical command-capture hook unset; no legacy grant is substituted. Current
source and expected Staff basis are checked before each owner phase; the hospital
supervision lock precedes Staff row locking. Replay requires original native
command/source provenance; legacy or changed-source receipts cannot be adopted.
The binary gate retains original authority through binding lookup and both storage
phases and reuses canonical attached/generation/hash rechecks.

Pure proof/source/command tests, backend build, vet, affected Go lint, architecture
collection/migration checks and compile-only bootstrap/Global API checks have
passed locally. The real native fixture now prepares/scans/binds through an exact
employment preparer, tests replay/history/withdrawal/source replacement and missing
support, and keeps independent review downstream. The corrected source3da62382
subsequently passed these PostgreSQL/storage cases as recorded below. No local
application/database/container was started. SDK/UI binding is implemented below;
whole-workflow remote/browser and full Staff/pharmacy completion remain open. WF0114 is an additive source candidate,
not an applied or specifically approved live migration.

### Native binding authoring and review preparation continuation

The candidate now includes a strict native binding SDK and private query hooks,
selected terms reread, available-file selection, attach/withdraw recovery,
protected download, evidence history and terms history. It distinguishes current
expected Staff basis from the bound historical terms basis. A changed terms
revision prevents authoring; a later Staff basis leaves existing evidence readable
without enabling a new attachment or submission. Original IDs/input/file metadata
survive uncertain writes; terminal withdrawal preserves frozen generations. The
shared review form selects native governance finding discovery explicitly, using
a separate cache key and no legacy selector fallback. Existing Staff callers keep
their original discovery consumer. No new public wire schema is copied.

Backend source `3da62382` passed the complete remote **Staff governance input
boundaries** step in CI `43525604797` at 2026-11-13T23:21:07Z, including the real
native binding/replay/history/withdrawal, disabled-support and source-replacement
cases. Its predecessor failed a test helper's unknown read-purpose tag after
positive binding/list/history/content already passed. The corrected probe reads a
genuine native owner/admin proof before testing inherited manager-write denial;
it does not relax the write-denial assertion or alter production authorization.
This step is not evidence that full coverage/supplier/browser checks passed.

Local focused evidence: 23 binding SDK and 20 selected-file SDK assertions pass;
the expanded UI exercises attachment recovery, withdrawal/history recovery,
historical read restrictions, concurrent terms withdrawal, native selector choice
and the selected-terms entry. Missing component/history/entry and historical-read
assertions were observed failing before implementation. The final full workspace
run passed all 30 tasks (21 cached, nine run, 6m11.342s), including 1,990 shell
and 1,321 API tests. The earlier full run encountered tests while their history/
historical-read implementation was still being added; the final run used unchanged
application source and passed. A missing download retry prop was caught by type
checking and corrected. No assertions or required checks were skipped. Full live Staff initiation, reviewed
linking, assignment/settlement, operating authority and pharmacy acceptance still
remain required. No migration, merge, native identity or production mutation has
been performed in this continuation.

### Independent operating-access governance source

The independent-duty consumer needs its own fixed nonclinical authority, rather
than account-link, hospital assignment or legacy employer-manager authority.
Add `iam.operating_access_reviewer.v1` with exactly
`iam.governance.operating.manage` and `iam.governance.operating.review`, scoped to
one `pharmacy` or `supplier` employer. Hospital, tenant, department, outlet and
premise grants are rejected for this template. Existing templates keep their
permissions and scope kinds. The native owner path and exact current grant path
use the existing IAM governance ledger, membership incarnation, credential
watermark, topology/owner-count locks, validity and callback lifetime.

This first source increment changes the canonical catalog and closed source
mapping only. It does not change operating-duty route admission, the legacy
administrative role helper, Workforce employer-manager access, Staff masters,
product stock permissions or owner security. IAM owns the grant; Workforce owns
the employer; the supplier/pharmacy product retains its current location and
physical stock. No copied identity or new infrastructure is introduced. The
existing capacity assumptions and five-second source budget above apply, with
unchanged deployment cost. Source lookup is current and strongly consistent;
missing source denies, hidden scope remains hidden, and unavailable membership,
credential or topology dependencies fail closed. Consumer adoption must retain
exact immutable preparation/decision provenance, original-source replay,
independent maker/target exclusions and current Staff/link/location gates before
this source can administer duties. Source availability alone is not operating
access or completion of the full goal.

Canonical generator parity and local positive/negative scope validation precede
real PostgreSQL/FGA/broker source tests. Those tests must cover each employer,
native owner and exact delegate, wrong employer/tenant/template/capability,
source revocation and dependency failure. Existing routes retain their current
behavior until the separately tested native consumer is wired. No schema change
is needed for this catalog/source increment: the existing governance ledger
stores a validated versioned template key and has no copied catalog enum.
Rollback disables issuance/consumption of the new template while retaining all
existing grant, decision, notification and audit history. Full Staff initiation,
reviewed links, employment/postings, both pharmacy ownership models, live QA and
Kargo deployment remain required by the original goal.

### Native operating-duty consumer

IAM adds `/iam/operating-governance-employers/:kind/:employer/duties/:id`
(GET/PUT), its `/proposal-receipt` GET and `/commands/:command` GET/PUT, plus
`/iam/operating-governance-employers/:kind/:employer/staff/:staff/duties` GET.
Wire commands, canonical duty versions, location/Staff/link checks, independent
approval, event publication, projection and target operating authority stay owned
by the existing canonical engine. Global API remains the client entry point.
Native manage/read/withdraw/revoke require the separate operating manage
capability; approve/reject require its review capability. The exact current
owner or grant is held by IAM's source callback throughout each transaction.
The path employer must match every body, selected row, page and receipt. A
purpose-bound Workforce callback validates that source and its active guarded
transaction before reading only the current independent employer root; it does
not require or create a manager/Staff row and does not replace target identity
or independent review checks.

Additive IAM0050 records private immutable native origins and decision sources,
with forced tenant/scope RLS and source-current SQL checks. Canonical insert and
command-receipt triggers write the provenance in the same transaction. Native
commands cannot adopt a legacy duty; legacy human commands cannot change a
native duty. System projection remains able to acknowledge the canonical
approved revision using its existing worker checks. Original preparation and
command receipt replays require the same current owner membership or exact
source-grant ID/version and membership epoch, never an alternate grant or a
recreated owner membership. Missing or changed provenance fails closed. Native
source reads do not publish credential/session evidence in existing wire models.
The source cutoff stops future administrative commands/replays; already issued
target duties retain their own current identity, location, expiry and explicit
revocation checks.

Owner locks precede credential, membership, source-grant and topology locks;
then canonical target-membership/duty/location/Staff locks use existing order.
Private source reads are bounded by selected duty or paged staff query. No new
infrastructure or cost is added; source and dependency failure remain bounded by
the existing five-second command budget. A failed mutation rolls back history,
source proof, receipt, audit and outbox together; caller request IDs retain the
canonical retry/conflict rules. Rollback disables the native routes and preserves
all source/history rows and completed physical/financial records. Acceptance
requires local source/boundary tests, real database trigger/rollback isolation,
legacy/native nonadoption, stale source and target, independent reviewer,
both independent employer kinds and exact-head contract/build/lint/E2E checks.
The final native UI and both pharmacy ownership-model live journeys remain part
of the original goal.

### Qualified independent employer discovery continuation

The candidate adds a separate Workforce-owned sealed employer-choice page and
strict SDK. Canonical `staff-access.json` generates its Go/Zod projection;
hospital discovery and legacy routes retain their contracts. Native nonclinical
owners/exact operating reviewers receive only canonical employer kind/ID/name and
current manage/review flags. Current IAM source, credential/membership/topology,
verified expected tenant/account and an audited guarded Workforce root read are
required before disclosure. No Staff, private source proof or broad product role
is released. Assets are employer identities and delegated authority; other-tenant
users and revoked/compromised delegates must not cross this disclosure boundary.

Reuse the existing sizing/SLO assumptions, twenty-candidate scan, four concurrent
checks and five-second overall source budget bounded by the session deadline.
Authorization filtering can yield an empty page with continuation. This adds no
infrastructure or migration; dependency or audit failure refuses the entire page.
It does not claim measured throughput/latency. Rollback removes new discovery
admission while retaining duties/provenance. Reusing the legacy employer directory
was rejected because it requires broader authority than the native delegate holds.

The unauthenticated discovery and missing SDK module assertions were observed red
before implementation. Local Go build, focused race, vet/lint and bootstrap
compile checks pass; 26 focused discovery/native-duty SDK tests pass. Remote
integration covers exact employer-only disclosure and source revocation alongside
the existing real independent Staff fixture. The late completeness-fault fixture
now creates a separate API-owned location, avoiding the legitimate one-live-duty
unique index so the intended deferred-attribution guard can be exercised. These
new remote cases have not yet passed. Qualified Staff/location selectors, native
preparation/review UI, full Staff/pharmacy journeys, merge and deployment remain
open. IAM0050 remains an unapplied migration candidate.

### Governed operating Staff choices — implementation target

Workforce owns the canonical Staff display name/version and independently reviewed
account link; IAM owns current nonclinical operating-governance admission. Add
`GET /v1/workforce/operating-governance-employers/:kind/:employer/staff-choices`
for exact pharmacy/supplier employers. Native owner or exact current
`iam.operating_access_reviewer.v1` manage authority is required, with verified
expected tenant/account headers. This is ORG-D3/D4/D8 consistent: selection creates
no membership, employment, manager grant, professional or operating authority.
Assets are Staff names, linked account subjects and immutable identity versions;
other-tenant users, revoked delegates and privileged insiders must not cross the
source/owner disclosure boundary. Out-of-scope employer is hidden with 404,
missing coarse capability is 403 and unavailable dependencies/support are 503.

A generated minimal projection contains Staff ID/display name/version and current
linked subject/link ID/version. Only active independent Staff and active reviewed
links matching that Staff revision are selectable. No contacts, HR category,
documents, evidence, source proof or native organization data is disclosed.
Keyset order is descending canonical Staff `(created_at,id)`, limit20, with
sealed tenant/account/employer/kind cursor binding. Source authority is current
and held through the bounded read/audit transaction. Current target membership,
identity, source location and versions are still rechecked by IAM on preparation;
a choice is never a reusable proof. One SQL statement obtains the bounded current
join and shared row locks, then attributable per-result read audit is atomic.

Candidate WF0116 adds an explicit read-only source purpose and retained policy
extensions for Staff/link reads and audit insertion. Restrictive purpose/delete
policies cover all Workforce tenant tables, preserving their existing predicates
when the purpose is absent and refusing inherited master writes or unrelated
private reads while it is active. The audit payload binds exact Staff/link
versions and current private source. Installed functions/policies and forced RLS
are verified before activation; missing/changed support fails closed. Existing
review/preparation source purposes are unchanged. No cross-module SQL or new
identity/Staff master is introduced.

Reuse existing platform planning assumptions and five-second/session source
budget, twenty-row pages and p99 read target; there is no new infrastructure or
measured performance claim. Existing tenant/employer/Staff and active-link indexes
must support the exact join; query-plan evidence is a remote acceptance gate.
The legacy Staff directory is rejected because it requires broader access. A
binary rollback stops this endpoint and retains all identity/audit history; no
column/table replacement or destructive down migration is introduced. WF0116 is
an additive, unapplied candidate requiring named live migration approval after
acceptance. Prove source scope, current link/basis, revocation, stale workspace,
sealed paging, missing support, inherited write denial and audit rollback in
remote PostgreSQL/FGA tests; complete strict SDK, qualified product-location
selection and native UI afterward. Neither this design nor a green compile
completes the full original Staff/pharmacy goal.

### Governed Staff-choice implementation evidence

The candidate now implements the scoped Workforce selector, canonical generated
Go/Zod model, strict native SDK and WF0116 read-only purpose. Shared Staff/link
locks and one bounded batched audit insert keep disclosure and read evidence
atomic without per-row database round trips. Selection preserves canonical v4
Staff IDs as well as v7, and source-purpose state is restored before returning
to the outer guarded command. Exact current owners and delegates share the
projection; no legacy directory or product authority is substituted.

Local red/green evidence: absent source-proof/handler and strict query helpers
first failed compilation; missing SDK import first failed the public SDK test.
Their focused tests now pass, with 37 combined SDK assertions. Explicit field
classification fixed the preceding head's observed architecture failure; no
classifier or assertion was loosened. Offline migration-ID/collection/route/
permission-matrix and both field-classification checks pass. Build, affected
Go race/vet/lint and bootstrap compile-only checks pass; all 30 workspace
lint/format/type/test/build tasks pass (22 cached). The 15 inventory-script
assertions pass, and generators/contractdocs/inventory checks are current.
The attempted local database-backed RLS check could not start because Docker is
absent; no database/container ran. Its actual migration/RLS acceptance stays in
CI. SQL statements, SQL function bodies and PL/pgSQL blocks parsed offline;
that is syntax evidence only. Generated app builds were cleaned, caches retained.

Previous head a016ba0c passed remote Staff governance boundaries, customer
onboarding, supplier regression, web and E2E. Its Go job stopped at the missing
employer-model field classification now corrected here. The new Staff-choice
remote cases compile and cover native owner/delegate, absent/sibling employer
source, stale expected tenant/account, strict filters, sealed paging across
account/employer/kind, canonical v4 Staff, stale Staff/link basis exclusion,
source cutoff, support tampering, raw identity mutation denial and audit rollback
for pharmacy and supplier. They have not yet passed on PostgreSQL/FGA. Qualified
product-location choices, native UI adoption, full Staff/recipient/posting and
both ownership-model live journeys remain open. WF0116 is unapplied; there is no
new main merge or live deployment in this increment.

Remote `d7493c2a` passed **Staff governance input boundaries** in CI43586537964
at 2026-11-14T10:51:07Z, including the new real PostgreSQL/FGA pharmacy/supplier
Staff-choice cases, v4 paging, exact owner/delegate, current source cutoff,
changed support, stale basis, raw write denial and audit rollback. This accepts
that API boundary, not the full original Staff/pharmacy workflow or deployment.

Supplier43586537957 then exposed a historical-upgrade fixture ordering error:
its document-provenance phase applied new WF0116 while intentionally deferring
WF0066/WF0094, so `employment_generation_id` did not exist. The fixture now
explicitly defers WF0116 there and includes it in its later complete posting
upgrade phase after those prerequisites. Both phase lists are updated; no
production migration is made optional and no behavioral assertion is changed.
The observed 42703 is the red evidence. Global API compile-only tests, backend
build, affected vet and lint pass after this correction. Full historical-upgrade
and supplier acceptance remain pending on the next exact head.

### Qualified operating location choices — implementation target

Expose one native IAM preparation endpoint,
`GET /v1/iam/operating-governance-employers/:kind/:employer/location-choices`,
through Global API. IAM owns current operating-manage admission and composes the
existing owner-provided operating-location boundary; PharmaConnect/SupplyConnect
retain all product SQL, structural facts and read audit. Workforce rechecks the
canonical typed employer under the same guarded current owner/exact grant. No
broad product catalogue, legacy manager or operational duty is substituted.

Extend the existing location-owner interface with a bounded selection operation
receiving the current principal, exact employer and retained callback-only source.
Both actual owners and the platform dispatcher must implement it; missing owners
fail closed. The generated public model contains owner tenant, employer kind/ID,
location ID, code/name, country and structural employer/location versions. The
private owner result additionally carries its canonical creation anchor for sealed
paging; no private source proof, contacts, addresses, stock, licences or financial
facts enter the HTTP result. Only current independent/chain Indian pharmacy
outlets and owned Indian supplier organisations' active warehouse/depot premises
match the existing structural location guard. This is IN-PH-02/06/10 consistent:
structural selection is not professional, licence, trading, stock or physical-act
eligibility. No new state-specific or legal rule is introduced.

Reuse the five-second/session/source deadline, 20-row page cap, descending owner
location `(created_at,id)` ordering and existing platform capacity/p99 targets.
Sealed cursors bind verified tenant/account/employer/kind and fixed `country=IN`
filter. Unknown, duplicate, empty or malformed query fields are refused. Product
owners validate the exact source/actor in a guarded command, hold shared current
root/location locks and atomically batch attributable read audit before release.
Names are never reusable authorization; duty preparation retains its existing
Staff/link, current membership, source/version, location and independent review.

Candidate pharmacy0047/supply0018 add explicit read-only selection purposes.
Retained root/premise read predicates are extended only for the exact structural
source. Restrictive purpose/delete policies fence all product tenant tables,
closing inherited stock/master writes and unrelated private reads during choice
lookup; context is restored before returning to IAM. Function/policy fingerprints,
forced RLS, runtime privileges and existing page indexes fail closed if unavailable.
No table/master is copied and no physical posting, release, licence, payable or
identity state changes. Each returned location receives the existing immutable
platform read audit (tenant, actor, action, resource type/ID and time), batched
atomically; audit failure rolls back disclosure. Preserve its column-name-only
privacy contract: names, source proof and revision values are not added to that
audit schema. The current source proof remains callback-only, and canonical duty
commands still retain their exact structural versions and immutable private
source provenance. A new selection-receipt ledger was rejected because a choice
confers no authority and does not mutate a business record. This decision uses
the established structural-read audit boundary without weakening command proof.

The simpler legacy catalogue selector was rejected because it requires unrelated
permissions and exposes broader commercial data. Separate product HTTP endpoints
would duplicate IAM admission already required by duty preparation; the existing
owner guard keeps product data ownership explicit inside the modular monolith.
No service or infrastructure is added. Binary rollback disables this new native
consumer and retains audit/history; no destructive down migration is proposed.
These migrations remain unapplied live candidates requiring concrete named
approval after real PostgreSQL/FGA scope/paging/fault, compatibility, upgrade and
rollback acceptance. Query-plan/load evidence and both ownership-model journeys
remain required. Complete strict SDK and native UI adoption before claiming the
full Staff/pharmacy workflow accepted; no new regulatory policy is activated.

### Operating location-choice candidate validation

The native IAM endpoint, both product-owned guard implementations, platform
protocol validation and strict SDK are implemented in this candidate. Shared
source-shape validation rejects wrong tenant/employer/template/capability,
expired sessions/sources, invalid memberships and malformed human subjects.
The dispatcher refuses foreign/reverse cursors, foreign/duplicate/malformed
owner results and missing providers. Missing identity was observed returning a
dependency error in the first local route test; explicit authentication now
precedes source refresh and the 401 assertion passes. No legacy fallback exists.

Observed red-to-green tests cover missing source proof/owner interface/route/SDK,
Unicode/control subjects, foreign cursors and reverse paging. The 51 combined
SDK assertions pass. Focused IAM/authz/platform/product race tests, offline
route/pagination/migration-ID/field-classification architecture checks, backend
build, affected vet/lint and bootstrap/Global API compile-only checks pass.
All 30 workspace lint/format/type/test/build tasks pass (25 cached). Product
migration SQL, SQL function bodies and PL/pgSQL blocks parse offline; that is
syntax evidence only. No local application/database/container was run. Canonical
operating generator, contractdocs and inventory are current: 1135 registered
surfaces, 1001 still unclassified. Generated app builds are cleaned with caches
and dependencies retained.

Real API-created fixtures now cover both owners, current owner/delegate,
absent/sibling source, scope-bound sealed paging, strict queries, immutable
pharmacy and mutable supplier revisions, inactive supplier premises, atomic read
audit failure, missing forced RLS and inherited product write denial. They compile
but have not run remotely on this location-choice candidate. Pharmacy0047 and
supply0018 remain unapplied live candidates. Native UI adoption, historical/whole
Staff and both pharmacy ownership-model journeys remain incomplete.

Prior `9a7d40fb` CI exposed an IAM golden test still expecting 49 migrations.
The observed local reproduction now passes with exactly 50 plus an explicit
IAM0050 ID/nonempty-SQL assertion; all prior IDs and hashes remain checked.
Supplier43587374642 passed the corrected historical ordering but later failed
supervised transfer fault injection: expected persistence 500, received source
503. Its failure remains unresolved and the assertion is unchanged. Failure-only
transfer diagnostics now record constant stage, elapsed milliseconds and deadline
classification without private payloads, so the next real-service run can locate
the failing boundary. This is diagnostic evidence, not a claimed runtime fix.

### Native independent-duty UI — implemented, acceptance pending

Add a dedicated Workforce operating-access page, linked from Staff access for
current operating-governance users. Its identity loader retains the original
scope while a command is uncertain, including a missing/changed permission reply;
a cookie/account switch is detected through expected tenant/subject headers and
does not rewrite the frozen command. Normal navigation uses the existing leave
guard. Current-source loss hides cached names/details and new write controls,
while GET-only original receipt recovery remains bound to the original scope.

Use the qualified employer and active reviewed-Staff selectors and native
location hook exclusively. Reuse the canonical duty form and decision controls:
legacy callers keep their directory/catalogue consumers, while an explicit
native mode receives the canonical qualified Staff/account projection and uses
the owner location projection's exact revisions. A missing/mismatched qualified
account denies preparation rather than invoking a legacy directory. Source
refresh/paging cannot replace an uncertain proposal's ID, frozen input or source.
Maker/target/independent-review rules and activation remain server decisions.

The page must freeze employer/Staff navigation during pending commands, retain
recovery UI on source-query failures, and prevent newly missing/ineligible choices
from starting commands. Pending decision/proposal controls remain mounted on
background query failure. Source flags are presentation hints only; every API
rechecks current governance and canonical identity/location state. No new clinical,
product catalogue or legacy employer-manager authority is granted. Add native
prepare/recover/review and scope-cutoff component tests, preserve all legacy tests,
then run complete workspace checks/build before exact-head CI and live acceptance.
This UI target does not close historical/recipient/transfer or production gates.

Validation for the native UI candidate: all 30 workspace lint/format/typecheck/
test/build tasks pass (22 cached), including 227 shell files and 2008 tests.
The focused recovery/scope/native/legacy suite passes 105 assertions across six
files. Playwright collects five Staff-access cases, including two new native
discovery/denial cases; these browser cases have not yet executed on this head.
Build outputs were removed, retaining dependencies and caches.

At backend head cb8849b8, supplier43592757028 passed the product/owner stages
but failed supervised child approval at workforce_posting_chain_test.go:118:
expected 200, received 503, with a posting-review deadline diagnostic. This is
a different occurrence from the preceding transfer persistence fault. Neither
that earlier fault nor full Staff workflow acceptance is presumed resolved.
The dedicated UI currently selects active eligible Staff only; historical/
ineligible duty management remains an explicit open gate.

### Approval deadline diagnosis continuation

Native UI web checks pass at 89605942 in CI43595306491. Its other runtime jobs
remain pending. Candidate approval diagnostics now attribute failures to constant
phases (scope authorization, proposal basis, receipt read, decision authorization,
supervision read/source checks, management sources, decision persistence and
guarded completion) with elapsed milliseconds. The error mapper retains its
existing status/code and excludes raw error content and private proofs. A
deterministic race-tested diagnostic test covers deadline/source/storage/denial
responses and verifies private details remain absent from logs and HTTP bodies.
This is diagnosis only: it neither increases the fixed authority budget nor
repairs or bypasses the unresolved child-approval/transfer failures.
