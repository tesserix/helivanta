# Scoped Staff reviewer bootstrap

Status: implementation design; no deployed permission or approved migration.
Implements the accepted ORG-D7 policy in ADR-0044. It does not settle an open
ORG-D decision. Protected ordinary account initiation was merged in PR #1668; this governance design remains unimplemented.

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

| Boundary                                              | Current source requirement                                                                                                 | Required integration before full acceptance                                                                                                                                                |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Ordinary assignment proposal read/review              | `assignmentAccessFor` and asynchronous `decisionScopeStatus` require `assignmentAdministrator`                             | Exact hospital scoped-access reviewer, rechecked both at decision and later owner verification settlement; preserve target/proposer separation and product prerequisites                   |
| Person findings and identity-source review            | `personReviewTransaction` / `identityScopeTransaction` require `lockReviewedLeaveActor`                                    | Exact nonclinical person-review governance actor; current native human/membership, contributor exclusions, protected document/evidence checks and audit; no invented Staff link            |
| Employment preparation/review                         | `intakeAuthorityWithGuard` requires the reviewer's existing reviewed link even for first reviewers                         | Separate employer-bound nonclinical preparation and review authority with explicit governance provenance in audit/receipts; retain evidence and independent-person rules                   |
| First employment activation                           | `employmentActivationWithGuard` requires scoped intake actor plus `employment.activate` and `enrollment.manage`            | Employer-bound nonclinical activation authority retaining current approved person/employment and native account checks; no operating authority follows from the activation decision itself |
| Hospital department posting and pharmacy relationship | `scopedPostingTransaction` requires `StaffActorUse` and `WithCurrentStaffActor`                                            | Complete the reviewers' own reviewed employment first, then ordinary reviewed posting/relationship assignments; preserve Staff, department, professional and product guards                |
| Independent pharmacy/supplier operating duty          | `operatingAdministrationRole` and Workforce `employerAccess(...,"manager",...)` require legacy administrative roles/access | Exact employer-scoped operating-access reviewer through both IAM and Workforce source checks; retain target identity/location eligibility and subject/proposer separation                  |
| Legacy privileged enrollment                          | `memberHandlers.grant` admits known roles                                                                                  | Coordinated admission change preventing new privileged rows from bypassing reviewed delegation, preserving first-owner onboarding and safe retained-row repair                             |

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
