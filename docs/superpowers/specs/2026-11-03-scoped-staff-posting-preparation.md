# Scoped Staff posting preparation

Status: implemented local candidate in PR #1532; normal browser acceptance and
production activation remain open. This specification does not activate policy.
Continues the [effective-posting specification](2026-11-02-effective-staff-postings.md)
and [complete reusable Staff objective](2026-11-02-reusable-staff-foundation.md).
Tracks #417, #418 and #429 without claiming their acceptance.

## Problem and verified baseline

The deployed `305294ab` preparation API retains canonical Staff/department
references and immutable non-operating drafts. Its permission and legacy hospital
grant do not supply a hospital-scoped IAM preparer or reader journey. Granting
`organization.structure.manage` to make a picker usable would expose unrelated
structural commands. Adding an IAM template alone would leave source eligibility,
discovery, RLS and normal UI acceptance unresolved.

Workforce owns Staff, login links, reviewed employment and drafts. Organization
owns current departments. IAM owns independently approved assignments and their
OpenFGA projection. Global API routes UI business traffic to those owners.
Canonical model inputs remain `contracts/staff-access.json`,
`contracts/workforce-postings.json` and the Organization contract source; consumers
must reuse generated wire models. A role, employment generation, posting draft
and effective operating duty are distinct records.

Verified source constraints:

- `pkg/authz/scoped_authority.go` supports hospital scope; it does not implement
  department or outlet assignments. Compound checks are bounded at two.
- `iam/assignment_command.go` holds current membership, assignment/scope epochs,
  credential revocation and employment-basis authority through a guarded commit.
  Its expiry timer cancels the transaction at the earliest checked cutoff.
- `workforce/hr_intake.go` demonstrates original-account binding, permission
  refresh, current scoped grants, no downgrade after scoped admission, and
  reviewed actor pinning. It is not a posting authorization projection.
- `lockReviewedLeaveActor` permits historical links without a generation through
  `validateGenerationActor`. New scoped posting access must require a current
  reviewed **digital** generation and the exact linked subject.
- `readProtectedGenerationState` restores owner-private purposes after reading
  the exact source. `employmentGenerationState` returns the earliest person and
  employment end. Its result must participate in the command deadline.
- Organization's current-reference guard proves structural state inside the same
  guarded database command; it does not prove Staff eligibility. Its existing
  list includes retired nodes and requires tenant governance read permission.
- Posting writes take the exclusive `workforce_lock_supervision` gate before
  Staff rows. That also serializes creation and retained-command retry. Retain
  this order until deterministic tests justify changing it.

## Capacity, privacy and scope

Unmeasured planning assumptions inherited from the parent: 50 peak requests/s,
80:20 reads/writes, 500,000/1.5 million Staff at months 12/36, p99 below 500 ms and
99.9% availability. This implies 40 reads and 10 writes/s at peak; growth is 3×
over the following 24 months. Scope-specific contention must be measured rather
than treating the hospital advisory gate as free.

Use bounded keyset pages of at most 50 choices, with a proposed 64 KiB serialized
response budget including envelope/cursor. Validate that budget against maximum
Unicode names and actual generated fields before freezing the contract. Existing
2–20 KiB typical payload estimates are not a worst-case guarantee. No attachment
or person/employment findings belong in a choice response. No new service, queue,
cache, image or dependency is needed. Incremental infrastructure cost is existing
PostgreSQL/OpenFGA capacity; benchmark and audit-retention cost remain unmeasured.

Protected assets are Staff identity, employment findings, private documents,
reasons and structural visibility. Threats include sibling/foreign scopes, a
retained session, withdrawn assignments, forged source IDs, role overlap,
expired employment, same-command races and a source change during commit.
Verified principal → IAM → source owners → target command are separate trust
boundaries. Browser IDs, headers and graph tuples never replace owner checks.

## Actors and permissions

| Actor                 | Authority and scope                                          | Preconditions                                                                | Approval                                                             |
| --------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Posting preparer      | Administrative, one hospital, new immutable catalog template | Current digital reviewed Staff actor; active membership and exact assignment | Existing independent IAM assignment review/activation; no self-grant |
| Posting reader        | Administrative read, one hospital, separate template         | Same current actor requirements                                              | Same independent IAM path                                            |
| Organization governor | Existing tenant governance role                              | Existing current authorization                                               | Existing governance path; unchanged                                  |

Add a genuinely separate owner-declared posting-read permission. Preserve the
existing preparation permission's v1 read behavior for already authorized callers;
define additive reader admission explicitly. A reader cannot save drafts. A
preparer can read/save drafts and obtain current choices; neither receives
structural history/management, protected evidence, identity administration,
employment activation, posting approval or Pharma operations by implication.

Organization declares a narrow department-choice read permission, composed into
the new immutable Staff access catalogs. It returns only active, current,
same-hospital structural references using the canonical Organization output
model. It exposes no private reason/actor and cannot mutate the tree. Do not
silently expand existing HR-intake/catalog snapshots. Tenant role defaults and
legacy grants stay explicit compatibility paths, not fallbacks after scoped
revocation. This does not settle open ORG-D2/D5/D6 decisions.

## Owner authorization composition

Implement only after resolving the closed provider boundary in
[ADR-0052](../../adr/0052-staff-source-command-authorization.md) against actual
architecture tests. No general module RPC, cross-owner table access, owner
repository export or arbitrary remote callback is permitted.

IAM must bind its checked tenant, subject, hospital, exact server-declared action
and effective cutoff to the active guarded command. A client cannot mint that
binding. Restrict minting to the IAM owner with architecture enforcement; do not
turn it into a general public context setter. Any proof is ephemeral, same-database
and callback-bound. Copies, a closed callback, changed principal/scope/action,
an unrelated transaction and another database fail closed.

A Workforce-owned current-actor provider consumes that binding and validates the
exact active login link, active canonical Staff, non-null reviewed digital
generation, matching subject, source/identity epochs and effective reviewed
person/employment evidence. Reuse owner eligibility logic rather than replicating
it in Organization. Minimal proof consists of canonical Staff/link/generation IDs,
their current versions/epoch and earliest effective cutoff. Private findings,
documents and registration content remain in Workforce. Only two initial
consumers exist: Workforce posting preparation and Organization choices.

The proposed owner permission is `workforce.staff_actor.use`. Each scoped
consumer must independently check its own business permission **and** this
Workforce source-use permission in the existing two-check IAM command. Only the
Workforce provider interprets source-use evidence. This avoids a cross-owner
whitelist of arbitrary consumer permissions and does not grant identity review,
profile disclosure, employment activation or operating duties. The source-use
permission has no standalone business endpoint. Add it through new immutable
catalog snapshots with the reader/preparer templates, not by expanding old ones.

IAM issues evidence through `WithScopedCommandEvidence` only inside its already
verified guarded action. `ScopedCommandGrant` returns a copied exact grant while
that callback and its original root remain live. Copies of the request slice,
`context.WithoutCancel`, changed scope/action and escaped callbacks cannot extend
the evidence. Architecture checks restrict issuing calls to
`iam/assignment_command.go` and reject dot-import bypasses. Consumers must also
require their own same-database guarded command; this evidence alone grants no
database access or current Staff eligibility.

The Staff provider should follow the existing structural-reference callback
pattern. Return only minimal immutable reference data for the callback's audit
and target command; those IDs are data, never reusable authority. Every command
re-enters the provider using its checked actor subject. Do not add an exported
repository, general module RPC, caller-selected source query, private findings
payload or another proof-factory API merely to transport those IDs.

Before implementation, prove the source-writer order against identity case
commands as well as HR/person/employment writers. Existing generation validation
locks current employment/person review heads for update after the identity epoch.
Reusing it while another command later advances that actor's identity epoch
requires a deterministic lock-order test, not an assumption that actor reads are
lock-free. A candidate is a canonical shared Workforce source gate for actor
readers and the existing exclusive gate for identity/source writers; posting
writes must acquire exclusive ownership first to preserve retained-command
serialization. Do not upgrade shared to exclusive or mutate historical migration
SQL. These writer/gate changes and their forward migration remain unimplemented
until the full order is verified.

Lock order is IAM authority → Workforce supervision/source/actor → Organization
structural gate/rows → posting head/revisions/audit/outbox. Keep the existing
exclusive Workforce gate for the first implementation; no shared-to-exclusive
upgrade. Sort multiple stable source IDs where required. Compare this order with
every affected identity, person/employment review, source-end and profile writer
before coding; a hidden reverse order invalidates the design. No OpenFGA or file
storage I/O occurs under source row locks.

The earliest session, IAM assignment/basis and current Staff-source deadline must
bound the guarded transaction through commit. The source cutoff may be earlier
than the IAM observation. Explicitly propagate it to the root guard or prove an
equivalent atomic bound; checking `effective` once and discarding `until` is
insufficient. Recheck current eligibility before final commit. No protected rows
or HTTP success are emitted before the outer transaction commits.

The platform operation `DB.ConstrainGuardedCommand(ctx, tenant, until)` may only
shorten an active same-database/same-tenant command. It grants no access and
accepts no permission, SQL or source selector. A past cutoff cancels the root
transaction even if an inner owner ignores the returned error; a future cutoff
remains armed through the outer commit. A later source cannot extend an earlier
deadline. Wrong scope/database, absent or closed command and a zero cutoff fail
closed. IAM retains its current denial semantics when a command source expires.
No public wire contract or schema changes are needed for this lifetime control.

PostgreSQL deferred constraints must drain inside the cancellable command before
its final deadline check and before issuing COMMIT. Canceling a driver context
while COMMIT itself is blocked is an uncertain database outcome, not proof of
rollback. An observed real-PostgreSQL test reproduced a committed write after
the source-expiry error when deferred trigger work was left inside COMMIT.
`SET CONSTRAINTS ALL IMMEDIATE` before the final context check prevents that
deferred work from carrying an already-expired command into a successful commit.
The final admission also compares the effective cutoff with PostgreSQL
`clock_timestamp()` after the deferred work drains; it does not rely solely on
when the Go timer goroutine is scheduled. Command participation is closed before
draining, so no owner may append work through an escaped callback at that point.
Ordinary network loss after COMMIT remains an uncertain response; retained
commands and fresh authorization are still required to recover its outcome.

## HTTP, persistence and failures

Publish exact additive routes/query projections, permission semantics and errors
in canonical owner contracts before implementation. Organization serves its own
choice projection through Global API; Workforce must not proxy its business
tables. Workforce serves scope-bound hospital discovery, minimal Staff sources,
draft lists/history and retained exact-command recovery. Preserve existing v1
receipts, UUIDv7 commands, expected versions, explicit UTC microseconds,
`status: draft` and `grants_access: false`.

Purpose-bound RLS must independently limit scoped posting access to the admitted
hospital and target Staff. A general authorized-purpose flag alone is not enough.
Source permission does not authorize private HR fields. Reads carry owner audit;
save audit/head/revision/outbox remain atomic. Same key/actor/intent reauthorizes
and returns the retained receipt. Different actor, source, scope or intent
conflicts. A lost response keeps its exact command for recovery.

| Condition                                              | Required result                                                |
| ------------------------------------------------------ | -------------------------------------------------------------- |
| Foreign/non-visible object                             | 404 without confirming another scope                           |
| Missing permission or no current scoped actor          | 403; never retry using legacy authority after scoped admission |
| Revoked credential                                     | Existing unauthenticated response; no fallback                 |
| IAM/OpenFGA, source proof or database unavailable      | 503, no stale protected rows or success                        |
| Invalid required fields/projection                     | 400                                                            |
| Stale draft/source/revision or changed retained intent | Existing documented 409 contract                               |
| Audit/outbox failure or elapsed cutoff before commit   | Entire command rolls back; recover using retained command only |

OpenFGA and database are critical; unavailable authority denies. Retained posting
history is a historical snapshot, but disclosure still requires current access.
The Organization choice projection must not expose retired/currently changed
nodes as valid choices; final save verifies the selected exact revision again.

## Normal-role UI acceptance

Discovery must support overlapping legacy, HR-intake and new posting roles
without duplicate hospitals or a shared cache that accidentally broadens another
feature. Preserve per-feature projection context in query keys and SDK methods.
Bind original tenant/subject and abort on account/tenant changes. A reader has
history and no editor; a preparer has required-field create/edit and current
choices. Revocation hides protected results and prevents retry with stale data.
Empty, pending, unavailable, stale and no-access states must be explicit.

Use normal Staff Admin, independently approved scoped preparer, scoped reader
and unrelated reviewer logins against the local fixture first. Prove required
fields, source choice, create/edit/history/reload, reader-write denial, exact retry,
unsaved navigation, role overlap, revocation and foreign hospital isolation.
Private JSON credentials/evidence remain outside Git. Verify one master, exact
immutable revisions/audits and zero new operating/login authority by read-only SQL.

## Migration, review and parent acceptance

Allocate the next free owner migration IDs only at implementation, preserving
reserved Workforce 0068 and immutable previous catalogs. Persist every forward
SQL migration and canonical generator input. Local fixture application is
authorized; production migration/rollout requires specific approval of the final
revision and complete migration list. Recovery removes exposure through GitOps
and retains additive schema, exact commands and audit history; never drop
employment/posting evidence to roll back UI access.

Admission gates include real PostgreSQL/OpenFGA negative/race tests, source-cutoff
ordering, current generation/subject and callback misuse, RLS, audit/outbox
rollback, same-key concurrent creation, expired deadlines and a lock-removal
mutation observed failing. Run affected full suites, build/vet/lint, generators,
architecture/inventory checks and normal-role browser acceptance before merging.

This slice does not create an effective posting or approve operating duties.
Independent posting approval, managerial supervision/cycles, transfers,
contractor expiry, suspension/end/rehire, standalone employers, Pharma
professional acceptance/PIC/cover/assistant duties and every legacy dispensing
consumer remain mandatory parent work. #429 also needs clinical department
reachability and referral/roster acceptance; hospital-scoped preparation cannot
close it. Current source research is not regulatory policy activation.

The simpler alternative of adding catalog permissions alone is rejected because
it leaves current actor proof, structural choice visibility, RLS and UI behavior
unimplemented. A separate Staff service is rejected because current-source
atomicity depends on the existing shared guarded database and no independent
scaling/failure requirement justifies extraction.

## 2026-11-03 implementation checkpoint

The reader permission, scoped draft consumers, bounded hospital/Staff discovery,
current department choices and `/workforce/postings` UI are implemented locally.
Candidate migration 0074 preserves one permissive tenant policy, restricts DELETE,
and retains legacy Staff write checks. Directory reads cannot broaden exact-target
draft context. Audit records the checked scoped assignment and digital actor.
See the [QA ledger](../../qa/staff-management-progress.md) for evidence and open gates.
Normal browser sign-in works. A fresh digital actor has completed UI-created
Staff/evidence, independent person/employment review, activation and separate link/
assignment decisions. Reader history/reload with absent write controls and
preparer required fields, current department choice, create/edit/history/reload
and unsaved-navigation protection now pass without a legacy hospital grant.
[Positive UI proof](../../qa/2026-11-03-scoped-posting-positive-ui.md) preserves the
older revoked source as negative evidence. Follow-up UI acceptance verifies a
committed-but-lost response recovered through the original command receipt without
duplicate revisions/audits, preparer withdrawal with fresh-session reader-only
history across reload, and unrelated reviewer denial. Same-session preparer expiry
also removes a dirty editor while preserving Reader history; separate-tab sign-out
clears the original editor to login. Direct withdrawal with isolated sessions and
the remaining account/foreign-scope matrix still prevent full browser acceptance. No
effective posting, operating duty or issue #429 completion is claimed.

### Credential and authorization failure checkpoint (2026-11-03)

The local acceptance fixture now covers administrator credential revocation
while reviewed posting assignments remain current, old-session denial, a fresh
signed-session recovery and continued old-token denial. A real OpenFGA outage
fails protected HTTP requests and owner scope checks closed; recovery preserves
the retained receipt and leaves the failed new command absent. A synthetic-user
watermark bypass makes the new test fail with 200 instead of 401; original source
is restored before final validation. See the Staff progress ledger for exact
commands, evidence and the unresolved normal-role browser acceptance gate.
