# Reviewed managerial posting supervision and positive approval

Status: proposed implementation contract; not activated. Implements the next part
of #417/#418/#429 and the complete reusable Staff objective, following ADR-0053.
The ancestry prerequisite is independently reviewed in #1551. This document does
not mark the parent, operational activation or departmental/Pharma acceptance
complete. No production migration or policy is authorized by this document.

## Ownership, scope and capacity

Workforce owns Staff, employment, posting intent, decisions and managerial chains.
Organization owns departments and their current ancestry. IAM owns permissions,
reviewed assignments, epochs and OpenFGA projection. Products reference the same
canonical Staff and posting; they add duties and professional acceptance, never
another employee master. This delivery remains hospital scoped; standalone
employer evolution is still required and must not fabricate hospitals.

Retain unmeasured planning assumptions: 50 peak requests/s, 80:20 reads/writes,
2–20 KiB normal responses, p99 below 500 ms, 99.9% availability and 500,000 Staff at 12 months and 1.5 million
at 36 months. Five placements and two commands each imply 5 million/15 million retained
revisions, about 10/30 GB at 2 KiB before indexes. Bound managerial chains at 32
postings, Organization paths at its existing eight levels, source responses at
64KiB, command bodies at8KiB and choice pages at50. These are design budgets,
not measured throughput or compliance acceptance. No service, cache or worker is
added; existing database/outbox and Global API retain their boundaries.

Private employment/management data and unintended scope expansion are the assets
at risk. Other tenants, sibling hospitals/departments and stale insiders are the
actors. Verified session, current IAM assignments, exact Workforce source and
Organization callback boundaries must all authorize; client intent is not proof.

## Actors and authority

- The existing posting submitter prepares a managerial intent in one hospital,
  with current reviewed digital Staff actor eligibility. It cannot approve it.
- The existing independent posting reviewer retains ordinary review authority.
  Its template and historical permission snapshots are not expanded.
- An initial hospital-management appointment additionally requires a separately
  approved hospital-scoped `workforce.posting_management_reviewer.v1` assignment
  granting `workforce.posting.management.review` and Staff actor use. This is a
  management capability, held alongside the separate ordinary review assignment;
  it supplies no clinical, HR editing or operating permission. The existing IAM
  independent-owner proposal/approval flow governs that assignment. A tenant
  administrator flag or null HR supervisor cannot substitute for it.
- Department managers receive no new assignment from a posting. Their clinical,
  pharmacy, payroll or other actions still need separate current product grants
  and applicable qualification, privilege and designation.

Apply ORG-AU-01/02/05/07, ORG-ST-01/02/03/05 and ORG-AC-01/02/04/05/06/07/08.
No open ORG-D decision is silently settled. Managerial supervision is distinct
from the existing profile reporting link consumed by Leave, clinical supervision
and pharmacist-in-charge/assistant duties. Preserve those owners and consumers.

## Canonical input and compatible output

Extend `contracts/workforce-postings.json`, then regenerate its Go/Zod outputs.
A proposal may include optional `supervision` with:

- `kind`: `hospital_management`, `department_supervisor` or `department_member`;
- `supervisor_staff_id`, `supervisor_proposal_id` and `supervisor_revision_id`:
  a complete optional canonical Staff/posting reference. Staff retains its
  existing UUID format; proposal and revision are UUIDv7. All are absent for
  hospital management and required for the other kinds. This matches the owner
  API's Staff-scoped key and reuses its existing RLS lookup boundary.

Positive approval must establish that the referenced proposal identifies a
canonical approved managerial posting, not a login or category. Omitted supervision keeps legacy non-operating preparation and
continues to refuse positive approval with `posting_supervision_required`.
Explicit null, incomplete references, nil Staff/non-v7 posting identifiers, unknown keys/kinds and root
requests with a supervisor are invalid. Nested validation agrees in Go and Zod.
The exact normalized intent participates in the retained command fingerprint.

The current public proposal and decision receipts already admit `approved` and
remain byte-compatible: no new unknown fields are added to existing strict SDK
responses. `grants_access` stays false even after approval. Retained supervision
metadata is disclosed through a new opt-in protected supervision context, bound
to exact proposal/revision/version, current purpose and original account.
Ordinary list/history and existing protected context shapes stay unchanged.
Use the existing protected `/context` GET with the additional explicit query
`supervision_context=v1`, preserving its required purpose/version/revision and
original-account preconditions. Omitted opt-in returns the exact legacy model;
opt-in returns a new canonical envelope containing the legacy context and a
nullable retained intent. Unknown/repeated opt-in values return400. The SDK uses
a distinct typed getter; an old getter must not silently discard a new flag.
The full new model and approval/source capabilities must be settled before the
feature PR is exposed; this internal draft is not a published response contract.
No extra Staff personal evidence or ancestor private rationale is copied into a
public receipt/event. Context exposes only bounded, necessary chain references,
current state, reviewed department identities and source cutoffs.

A new optional decision attestation `hospital_management_reviewed` is false when
omitted and may be true only on approval. A true attestation selects the extra
management assignment before any Workforce row locks. The stored intent is
checked inside the command: a hospital-management approval without current
callback-bound management evidence denies; a non-root cannot be reclassified by
this flag. This is explicit review intent, never caller-supplied authority.

## Persistence and positive transition

Add an append-only Workforce migration; preserve all earlier migration bytes.
Retain typed supervision intent against the initial proposal revision, and bind
subsequent decisions to that immutable source. Add a current posting source epoch
and independently retained lifecycle/approval evidence as the owner needs it.
Database constraints, forced tenant/hospital RLS and guarded purposes enforce
shape, immutability, exact revision links and legal positive transitions. Normal
application writes without the required owner context/source evidence cannot
manufacture an approved root. IAM authority is enforced by the closed server
callback, not by trusting client-supplied SQL settings; the database credentials
remain a trusted server boundary. New IAM templates/catalog/migration are generated from staff-access JSON;
older catalogs and snapshots remain unchanged.

Freeze intent in the initial proposal transaction. Add a nullable creation
transaction column to retained revisions, then set its default for future rows;
leave historical rows null without a volatile-default table rewrite/backfill.
An initial-row `supervision_requested` marker defaults false, preserving legacy
commands and public receipts. Typed intent references the initial revision,
canonical Staff/proposal and optional complete supervisor key, with tenant and
hospital constraints. A trigger requires the marked version1 and its creation
transaction. A deferred check refuses commit if a marked submission lacks its
intent; the typed row cannot be added later, changed, removed or reparented.
Later decision revisions preserve the initial link through owner lookup.

Opt-in disclosure uses the same current purpose/actor boundary and atomic read
audit as existing context. A marked proposal with missing/corrupt metadata is an
integrity failure, never a fabricated legacy null. Preserve default-context
compatibility and test immutable retry, post-withdrawal history, wrong scope,
dependency failure and audit/outbox rollback through the actual Global API.

Approval requires the current exact pending proposal; its current target Staff
version, reviewed employment generation and source epoch; matching active
Organization references; and current actor use and independent decision proof.
Preparer, draft contributors, target and retained login associates cannot supply
the independent decision. Existing rejection/withdrawal and immutable recovery
semantics remain intact.

A hospital-management posting is an explicitly approved root, not inferred from
missing data. A non-root names an exact approved supervisor whose kind can
supervise; a member cannot supervise. Hospital management covers only its own
hospital. Department supervisors cover their exact active department and current
descendants, never siblings, parents or other hospitals. Use Organization's
current-ancestry guard instead of copying or querying its tree in Workforce.

A chain must reach an approved root within 32 postings. Self Staff/posting links,
repeated Staff/postings, missing ancestors or exceeded bounds deny. Every nominal
supervisor interval contains the child's interval. Source expiry is a separate
cutoff, not a rewrite of nominal ongoing terms. Scheduled children require
sources and supervisor intervals covering their start; they do not become
current early. Effective eligibility is the intersection of all current source,
chain and interval constraints.

## Transaction and failure behavior

Acquire all required current IAM grants before Workforce locks. Preserve the
existing guarded command and hospital supervision gate, current actor checks and
stable owner lock order. Resolve and pin target/supervisor source while that gate
is held, then acquire the Organization structural callback. No external graph,
object storage or RPC is introduced under owner row locks. Recheck deadlines
before outer commit; a success body is sent only after that commit succeeds.

When IAM or a required owner is unavailable, return503 and write nothing.
Missing permission/current eligible actor is403, hidden scope404, malformed
input400 and stale/ineligible/conflicting source409. Audit/outbox failure rolls
back the head, immutable revision and source evidence together. Same command,
actor and intent reauthorize and recover the original receipt; different intent
conflicts. Recovery never resamples supervisor intent or grants access.

## Lifecycle and full-objective acceptance

Positive approval alone does not complete lifecycle enforcement. The same owner
source must later fence every dependent operating use. Suspension, resignation,
termination, retirement, contractor expiry, supervisor ending and structural
moves deny dependent use without deleting history. Restoration/rehire requires a
fresh reviewed generation and new decisions; old duties/assignments cannot revive.
Transfers atomically end the old posting and schedule its replacement after
independent review. Product-owned duty/credential checks remain additional.

Required tests cover normal independent root and descendant approval; old catalog
and omitted-intent compatibility; wrong/expired management grants; current source
and attestation mismatch; all chain/direction/bound failures; exact/concurrent
recovery; target, supervisor, identity and structure races in both orderings;
audit/outbox faults; and future/ended source states. Mutation proofs must catch
removed independence, root capability, source/interval and ancestry predicates.
Normal Staff submitter/reviewer UI must select and display canonical intent and
sources, approve, recover uncertainty, reload persistence and deny forbidden use.
Local test fixtures do not replace real-account browser acceptance.

Subsequent lifecycle, standalone employer, all departmental/Pharma operating
consumers and production evidence remain explicit parent obligations. Reporting
root approval as full operating activation would be incorrect. Future routes,
permissions and migrations require contractdocs, canonical generation and review
ledger updates, exact-head CI, GitOps and separately named rollout approval.

## Retention checkpoint implementation detail

The candidate 0079 migration binds the exact initial revision using the existing
revision primary key plus an owner-context trigger checking hospital, Staff,
proposal, version 1, subject and creation transaction. It adds no redundant index
on the populated revision table. Proposed supervisor references remain typed,
immutable untrusted intent; they are not foreign-key certified appointments.
Positive approval must resolve and pin the exact same-hospital Staff/proposal/
revision under the existing selected-Staff owner scope, then retain separate
verified decision evidence. An unavailable, unrelated, stale or unapproved source
must fail closed. This distinction avoids broadening RLS to resolve an untrusted
reference and must remain visible in reviewer UI acceptance.
