# ADR-0053: Reviewed managerial posting supervision

Date: 2026-11-04. Status: proposed policy and delivery design. Only the optional
Organization ancestry source check is being implemented in this slice. This ADR
does not activate positive approval, a permission, a statutory designation or a
new production migration.

## Context and capacity

The complete [reusable Staff objective](../superpowers/specs/2026-11-02-reusable-staff-foundation.md)
and [effective posting specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
remain required, including standalone employers and modern/legacy Pharma
consumers. #1544 and #1547 deployed non-operating proposals and terminal decisions;
#1550 adds normal submission UI. Approval still returns
`posting_supervision_required`. The profile's `supervisor_id` is a general HR
reporting relationship, not a reviewed, effective department-management posting.
Merged #1546 legitimately consumes that relationship to narrow scoped leave
review through pinned reporting-line steps; hospital leave-desk authority remains
separate. Preserve that HR consumer without treating it as managerial posting
authority.

Retain the parent's **unmeasured assumptions**: 50 peak requests/s, 80:20 reads to
writes (40 reads and 10 writes/s), 2–20 KiB bounded responses, p99 below 500 ms,
99.9% availability and 500,000/1.5 million Staff at 12/36 months. At an assumed
five historical placements and two command revisions per placement, that is
2.5/7.5 million placements and 5/15 million revisions. At roughly 2 KiB per
revision, payload alone is about 10/30 GB before indexes. These figures justify
bounded indexed lookups, not a new service, cache or partitioning scheme. The
existing Organization owner caps one hospital at 5,000 nodes and eight levels.
Planning figures and an EXPLAIN fixture are not throughput or SLO acceptance.

## Proposed approval model

Workforce retains one canonical Staff identity. A proposed posting explicitly
declares one internal managerial placement kind: `hospital_management`,
`department_supervisor` or `department_member`. It also declares its supervisor
posting reference, or an explicit root request. This is a responsibility model,
not a patient-access role, clinical privilege or pharmacist designation.

- Only a hospital-management request may be a root. Its independent approval
  needs a new explicitly reviewed hospital-scoped management-review capability
  in a closed IAM catalog, in addition to ordinary posting review and current
  Staff-actor use. Generic administrator status, HR category, a profile Supervisor
  field and an ordinary reviewer assignment cannot bootstrap that capability.
  No existing role or historical catalog is silently expanded.
- A non-root names an exact approved managerial posting and reviewed source.
  A member posting cannot supervise another posting. Hospital management is
  explicit hospital responsibility; a department supervisor covers its exact
  department and current descendants, never siblings, parents or another hospital.
- Root requests remain non-operating preparation until independently approved.
  The target, preparer, retained login associates and other contributors cannot
  supply the independent decision. An approval records a named person, current
  source, scope, interval and accountable decision; it does not claim a statutory
  appointment or grant an IAM role.
- Managerial reporting remains distinct from clinical and pharmacist supervision.
  Matrix assignments remain separate. Historical HR profile references are not
  silently reinterpreted or erased; their consumers need an explicit migration.

The canonical proposal input can evolve additively with an optional typed
supervision intent. Existing omitted intent keeps its current non-operating
meaning and cannot be positively approved. Exact metadata belongs in retained
Workforce records; a new protected source/context contract must disclose it to
the reviewer without adding unknown fields to existing strict receipt models.
Pin canonical JSON/Go/Zod inputs, recovery and actual consumer compatibility before
implementing those commands. This ADR is not the implemented wire contract.

## Current-source and transaction requirements

Reuse the current guarded command. IAM checks every required assignment through
commit, then Workforce's hospital supervision gate orders actor, target and
supervisor source locks, then Organization holds its structural source gate.
Additional Staff keys are locked in stable order. Recheck after slow external
I/O; constrain commit to the earliest identity, assignment, employment or
supervision cutoff. No OpenFGA/storage call is introduced under product row locks.

Every supervisor chain is bounded and must reach an explicitly approved root.
Self-supervision, a repeated Staff/posting, an incomplete chain or an exceeded
bound denies; none is treated as a root. Nominal supervisor intervals cover the
child interval. Reviewed-source expiry remains a separate effective cutoff and
does not silently rewrite an ongoing posting's nominal end. Scheduled supervisors
can support a scheduled child only when the approved intervals and sources cover
its start. The exact bounds and all source writers still need implementation and
race proof before approval is enabled.

Effective state derives from current source/decision/interval/chain state.
Suspension, ending, expiry and revocation stop dependent use without erasing
history. Rehire/restoration requires a fresh generation and approval; it cannot
revive the old chain or IAM/duty assignments. Historical approval is not revoked
merely because its reviewer later leaves; explicit withdrawal of a decision or
loss of its target/supervisor source is a separate attributable transition.

IAM or required owner unavailable: 503, no write. Missing current permission:
403. Hidden scope: 404. Changed/ineligible source or conflicting command: 409.
Audit/outbox failure rolls back the whole command. Same-key recovery reauthorizes
and returns the original immutable receipt; it never rebases onto new supervision.

## Implemented first dependency: Organization ancestry source check

Extend the existing value-only `authz.DepartmentReference` with a paired optional
ancestor ID/revision. Zero/zero retains the existing direct-reference contract;
a partial or invalid pair fails closed. No new authorization method or business
module call is introduced. The caller must obtain these references from its
own verified posting source; the references themselves confer no authority.

Organization verifies the same tenant/hospital, both exact active revisions and
the current child-to-ancestor path. It alone queries its tree. Current references
are row-locked in stable ID order, and the existing shared hospital structural
gate remains held through the outer command commit. The path is bounded by the
owner's eight-level rule and uses keyed parent probes. Hidden/missing, retired,
moved, stale or incomplete structure does not become broader supervision.

This implements ORG-ST-01/02/03/05 and supports the later ORG-AU-05/07/08 checks;
it does not implement their complete posting/grant consumers. Existing callers
in Workforce's draft/submission handlers leave the constraint absent and retain
their old behavior. Every call site, owner query, architecture rule and existing
guard test must still pass. No Staff or department table is copied into a product.

## Delivery, rollback and alternatives

The ancestry dependency adds no route, event, migration, permission, service,
image repository or standing operating cost. Reverting its code restores the old
internal reference shape; persisted business records are unchanged. Before any
consumer uses the new constraint, its exact scoped source and cutoff must be
tested. Future retained intent/approval tables use append-only forward migrations,
immutable history and separately named rollout approval; schema rollback preserves
history and needs a reviewed compensating change.

Rejected: treating null profile supervision as unrestricted root authority;
letting a general reviewer appoint the initial hospital manager; inferring
management from job category; allowing every approved member to supervise;
copying Organization tables into Workforce; or resolving ancestry in Pharma.
The existing authorization-only owner guard is the smallest boundary that keeps
current source ordering without creating another Staff master or a new RPC seam.

Still required: complete canonical intent/approval contracts and implementation,
closed IAM catalog/projection, real independent root/descendant approval,
supervisor/source/structure race and mutation proof, lifecycle/transfer commands,
normal-role browser acceptance, all departmental/Pharma consumers, employer-scope
evolution and named production evidence. Do not close #417/#418/#429 or the parent
goal from this dependency's tests.
