# Effective Staff postings and departmental consumers

Status: proposed implementation specification. No new endpoint, schema, grant,
production policy or architectural exception is approved or activated by this
document. This continues all five requirements in the
[reusable Staff objective](2026-11-02-reusable-staff-foundation.md), not only the
shared directory. Tracks #417, #418 and department access #429.

## Source facts and the problem to solve

Organization owns canonical hospital departments and immutable structural
revisions. Workforce owns Staff, reviewed employment generations, login links,
credentials and the existing HR vocabulary-to-structure snapshot handoff. IAM
owns independently approved assignments, current membership and OpenFGA
projection. Pharma owns outlets, professional acceptance and operating duties.
The handoff is historical evidence; it is not an effective posting.

The current `authz.ScopeRequest` contains HospitalID and no outlet/department
scope. `ScopedAssignment.objectKey` accepts hospital and Staff scopes only.
`ScopedCommandGuard`/`CompoundScopedCommandGuard` and
`tenantdb.WithTenantCommand` already keep IAM authorization and a product write
in one bounded transaction. The existing employment-activation bridge lets
Workforce consume an IAM-owned employment basis through its guarded writer;
it does not establish a general module-to-module RPC facility.

Workforce already has `lockReviewedLeaveActor`,
`readProtectedGenerationState` and `employmentGenerationState`. These enforce
the current reviewed login/generation, source/identity epochs, independent
person/employment findings and effective deadline inside their owning module.
Reuse this owner logic rather than copying eligibility rules into IAM or Pharma.
IAM assignment evidence alone intentionally does not replace those product/source
checks; a current IAM receipt is not a claim that every Staff source is eligible.

Pharma's outlet command helper locks explicit outlet access through commit.
Its legacy `/pharmacy/dispenses/:id/dispense` handler instead operates on
tenant-scoped rows with visit IDs and patient-name snapshots, without a hospital
or outlet key. The legacy visit consumer creates those rows. Adding a guard to
`outletAccess` cannot cover that command family. The existing API review already
records this narrower-scope migration gap. Neither a client-selected hospital
nor an outlet inferred from common ownership repairs its lineage.

## Capacity, privacy and consistency

Retain the foundation's unmeasured planning budgets: 50 peak requests/s,
80:20 reads/writes, 2–20 KiB bounded responses, p99 below 500 ms, 99.9%
availability, and 500,000/1.5 million Staff at 12/36 months. These are design
assumptions, not measured performance. Lists use server-bounded keyset pages,
at most 200 rows. No new service, queue, cache or image is needed for the shared
database implementation.

Private employment, identity evidence and professional findings are the assets.
Relevant attackers include sibling/foreign scopes, self-reviewers, retained
sessions, stale authority projections and concurrent lifecycle changes.
Identifiers from a browser or event are references, never authority.

Posting history and observations may be read as historical snapshots. New
approvals and operational commands must use current source state. Source
cutoff and the affected command must have a defined order through commit;
periodic reconciliation, a short cache TTL or an event timestamp cannot provide
that guarantee. External storage and OpenFGA I/O stay outside owner row locks;
their result is revalidated against current identity/epochs before admission.

## Canonical posting record

Preparation starts with a versioned draft; its exact
[candidate contract](../../api/workforce-postings.md) is separate from an
effective posting. A draft may precede employment activation and records current
Staff concurrency pins plus the proposed structural reference and interval. It
confers no authority. Submission/approval must later bind the reviewed generation
and remaining required conditions below; no draft is an approved posting.

Workforce will retain one posting identity plus immutable command/revision
history. It references, without copying:

- Verified tenant, employer/hospital and canonical Staff ID.
- Exact reviewed employment generation and source epoch. A directory status
  of Active alone does not satisfy this precondition.
- Canonical department/sub-unit ID and the structural revision used for review.
- Effective interval as explicit instants `[starts_at, ends_at)`. A contractor's
  posting cannot extend beyond the reviewed engagement. Calendar-date inputs
  require the configured owner timezone; unset timezone gives an explicit block.
- Managerial supervisor posting, if required by the posting type, independently
  from clinical or pharmacist supervision. A supervisor must be effective in
  the permitted scope; self-links, cycles and ended supervisors are refused.
- Optional product-unit reference, verified by that product owner. For Pharma
  this binds pharmacy/outlet IDs and the explicit hospital relationship.
  Workforce owns the posting; Pharma adds duties against its ID, not another
  employee or employment master.
- Preparer, independent decision, protected reason and source references;
  public receipts omit private reasons, registration content and actor details.

New resource, command and revision IDs are UUIDv7. Existing Staff/hospital IDs
retain their formats. Expected version and exact actor/intent are part of every
retained command. Same-key/same-intent retry reauthorizes and returns its original
receipt; changed actor, scope or intent conflicts. A lost response is recovered
by exact command lookup, never by silently generating a new command.

Submission is `pending`; independent review produces `approved` or `rejected`.
The preparer may withdraw a pending proposal. Approved future postings are
scheduled, not effective early. Effective state is derived from interval and
current source/decision state, not a worker-maintained Active checkbox. End,
suspension and revocation preserve the approved history and advance the source
epoch. Resumption/rehire needs a fresh generation and new approval; it cannot
restore old assignments or duties. Corrections append evidence.

A transfer is one retained Workforce command that ends the old posting and
approves/schedules its replacement atomically after the independent decision.
Two pending conflicting transfers for the same posting conflict. Source-ending
and dependent authority checks must commit together; projecting new tuples is a
separate recoverable step. Replacement access still needs independent IAM review.
Staff, employment, posting, duty and IAM validity intervals intersect; none
extends another owner's interval.

## Current owner checks through commit

The proposed composition needs an explicit reviewed extension of the existing
authorization bridge. It is limited to the deployed shared database, owner-
implemented eligibility checks and the active guarded command context. It must
not expose repositories, GORM transactions, arbitrary table names, business
queries or caller-chosen upstreams to other modules. Business data exchange and
reconciliation continue to use owner contracts/events.

The intended order is current authentication/membership and IAM assignment locks,
then Workforce employment/login/posting locks, then Organization structural scope,
then Pharma outlet/professional/duty and business locks. Each owner determines
its exact row order; multi-Staff/supervision operations sort stable IDs. All source
mutations that can acquire multiple owners must obey that same order. A source
mutation touching only its own rows must not acquire an earlier owner's lock
afterwards. Audit/outbox failure rolls back the guarded command.

An owner supplies current proof only inside its callback. Proof binds the
verified tenant, subject, exact Staff/generation/posting and requested scope,
current epoch/version and effective deadline. It cannot escape its callback,
be cached, be supplied by a browser, authorize a different database, or survive
the transaction. Identity and required grants are rechecked after slow I/O.
The deadline is the earliest authentication, assignment, employment, posting,
credential, duty or supervisor cutoff. Recheck before final commit; missing
authority/dependency denies rather than falling back to legacy grants.
Return HTTP success or protected bytes only after the outer guarded transaction
has committed. Wrapping an existing response-writing Gin handler in a later
commit is unsafe: a source/audit failure could follow an already-sent success.
New command callbacks return their result to the outer owner boundary; reads and
file release retain their own current-authority and disclosure-audit rules.

The proposed composition is recorded in
[ADR-0052](../../adr/0052-staff-source-command-authorization.md). Before exposure,
platform/security review must verify its closed authorization-provider boundary,
wiring, RLS purposes and deterministic lock order. The present standards prohibit
general cross-module calls; an interface alone does not make them permissible.
An extracted service cannot join this transaction: extraction requires a separate
reviewed fencing protocol and equivalent cutoff acceptance. Do not disguise a
remote check followed by an unguarded product commit as the same guarantee.

## Pharma professional acceptance and duties

Pharma acceptance references the current Workforce credential and reviewed
validity/evidence version. Registration number, issuer and uploaded bytes remain
with Workforce. A new product-owned decision binds outlet/premise, jurisdiction,
applicable policy/source version, reviewer and the exact owner observation.
Preparer and linked Staff subject cannot decide; retained login associations
remain relevant. Missing current evidence or timezone blocks eligibility.
Historical licence snapshot text stays readable and requires explicit reviewed
reconciliation; it is never silently converted to verified Staff identity.

PIC, cover and assistant supervision are effective, independently reviewed Pharma
duties referencing canonical postings. They do not grant IAM permission. An
assistant may prepare only through its declared commands; it cannot finalize a
pharmacist act or impersonate a supervisor. Finalization requires the exact
eligible pharmacist, current outlet assignment and named duty. Scope, supervision
and validity conflicts are owner-enforced and retain handover history.

No state council, licence, retention or drug-schedule rule is inferred from this
spec. Activation requires applicable current primary sources and accountable
pharmacy/legal review under IN-PH-02/04/06/10. Regulatory research is evidence for
review, not an activated production policy.

## Migration and API/UI delivery

The canonical input will be a reviewed `contracts/workforce-postings.json`, with
generated owner Go and shared Zod models. Proposed HTTP commands, errors,
permissions, event payloads and examples must be pinned there and in the owner
API before implementation. All UI/app traffic enters Global API. New scoped
prepare/review/read templates have closed canonical catalog history; a tenant
administrator role or HR category is not a substitute for scoped authority.

Normal Staff UI must support required fields, source/current-state explanation,
canonical department choice, supervision, pending independent review, history,
effective transfer/end and conflicting edits. Pharma reuses the canonical Staff
selector/summary and adds only its own acceptance/duty forms. Foreign, disabled,
stale and unavailable states must not show stale private rows as current data.

Inventory and classify every affected operational surface and legacy grant before
cutover. Preserve a separate governance/recovery path. Unresolved legacy dispense
lineage is explicitly unavailable for new operating authority; coordinate its
consumer migration and API compatibility rather than silently disabling or
changing v1. Do not activate only the modern outlet routes and claim complete
Pharma enforcement. Existing transactions and evidence survive corrections/exits.

Standalone employer support remains required by the parent objective. Current
Staff and its dependent tables are hospital-scoped; a pharmacy organization is
not a hospital. A separate reviewed expand/backfill/consumer migration must
establish canonical employer scope without a duplicate employee master or fake
hospital. This hospital posting work does not settle that contract or claim it
complete. Existing hospital consumers keep their explicit boundary throughout.

## Required proof before completion or activation

1. Real Postgres owner tests: versions, immutable receipts, duplicate/changed
   retries, required/null/false distinctions, scope isolation, audit/outbox
   rollback, transfer conflicts, cycles, contractor boundaries and non-resurrection.
2. Real OpenFGA/IAM tests: exact department/outlet assignment, no sibling/foreign
   scope, owner/administrator without professional authority, revoked role,
   outdated graph snapshot, pending/failed projection and authorization outage.
3. Deterministic barriers: suspension/end/expiry wins before command admission;
   an admitted command orders before source cutoff; delayed identity/FGA/storage
   response cannot restore authority. Cover supervisor and structural retirement
   races as well as Staff. Mutation-test each critical epoch/locking predicate.
4. Actual modern and legacy Pharma route coverage: read/disclose, prepare,
   dispensing finalization, stock safety, H1 release, refund and financial
   commands. A helper test is insufficient evidence for every consumer.
5. Normal admin/preparer, independent reviewer, pharmacist, assistant, owner,
   inventory and auditor browser sessions through Global API. Verify required
   fields, edit/reload, refusal, protected evidence and retained history, then
   read-only persistence proof. Synthetic fixtures do not bypass real guards.
6. Generator and contractdocs parity, affected full race suites, lint/build/
   typechecks and consumer tests; reviewed exact migrations, GitOps rollout and
   production synthetic acceptance. Do not close #417/#418 or the parent goal
   until their full acceptance and remaining employer/audit work are proven.
