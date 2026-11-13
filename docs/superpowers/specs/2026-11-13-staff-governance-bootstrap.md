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
source. These compile; runtime acceptance is pending. UI/SDK preparation adoption,
document authoring/binding and downstream staffing/access remain unfinished.

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
workspace checks and remote current-head acceptance must still pass. This does
not complete preparation UI or associated document authoring/binding.

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
Governance document authoring is still unfinished and this is not merge-ready.

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
