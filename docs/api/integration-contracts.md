# Events and cross-product integration contracts

Unreleased organization work adds a same-tenant hospital-reference request,
retained Workforce existence observation and identifier-only department revision
notification. Their payloads derive from
`contracts/organization-structure.json`. No Staff details, private reason or
authority travel in these events. See the [organization contract](organization.md)
for correlation, duplicate handling, failure behavior and incomplete UI/runtime
acceptance. The new owner does not query Workforce tables directly.

The staff-access worktree adds a same-tenant IAM/workforce exchange:
`helivanta.in.iam.assignment_scope_requested.v1` →
`helivanta.in.workforce.assignment_scope_verified.v1`. Both declarations and
payloads derive from `contracts/staff-access.json`. The request contains proposal,
request and hospital IDs plus the proposing actor subject, needed to verify the
owner's explicit manager grant. The response contains matching IDs and eligibility.
No target subject, private reason or staff profile is sent. Neither event grants
access. IAM accepts only its current correlated pending request, rechecks
membership/expiry, and records a review-queue or rejection state. Approval needs
fresh owner verification and independent reviewer authority; this preliminary
response cannot substitute for current authorization. See the platform domain
contract for retry, history, activation and unfinished product-access gates.

Department-scoped proposals (ADR-0055) add a second owner on the same request:
`helivanta.in.iam.assignment_department_scope_requested.v1` →
`helivanta.in.organization.assignment_department_verified.v1`, published beside
the Workforce request in the same outbox transaction. The request carries only
proposal, request, hospital and department IDs; the reply echoes them with
`eligible`. Organization answers from current structure: a department is eligible
only in a verified hospital with an active node-to-root path, otherwise it answers
`false`; an unavailable read retries rather than answering. IAM keeps the first
answer per owner and request in append-only `iam_assignment_department_verifications`,
ignores stale or mismatched replies, settles on any refusal, and advances only when
Workforce and Organization both confirm the current request.

IAM also publishes `helivanta.in.iam.assignment_projection_requested.v1`, with
only `{proposal_id}` from the canonical staff-access contract. The owning IAM
consumer reloads the current subject/hospital scope and permission snapshots;
the event supplies no authority, subject, permission list or private reason.
The durable consumer's `AfterCommit` step writes OpenFGA outside database locks,
records guarded per-snapshot activation, then compare-and-set acknowledges the
scope epoch. Duplicate deliveries re-run
that idempotent step, including after a crash following commit. Stale attempts
cannot acknowledge a newer epoch. Missing graph dependencies or write failures
retry through the existing bounded retry/dead-letter mechanism. Activation does
not replace IAM's current-state authority check or product-owned lifecycle and
object checks; no new product route is authorized by this event alone.

[Contract index](README.md) · current guarantees and proposed journeys

## Client integration boundary

UI/apps connect through the [Global API](global-connector.md); product APIs retain
their contracts and state. Its routing service does not replace event delivery,
Temporal workflows or downstream completion evidence described here.

## Current event authority

The canonical subject constants and payload types live in each module's data-only `/contract` package. Other modules may import that package, but not the producer's repository/service implementation. The registry exposes `Publishes`, `DirectedSubjects`, `DirectedWriteTables`, durable `Consumers` and per-replica `Broadcasts`. The [generated inventory](reference/README.md) is their complete union, including conditional workflow consumers with registration gates enabled. It does not assert that any environment enables those workers.

[Generated payload schemas](reference/event-payloads.json) map every published subject to its Go payload type, source line and an inline JSON Schema 2020-12. They are generated from current JSON tags, embedded structs, primitive/time fields and pointer nullability. They describe structural serialization, not all semantic validator rules, and are not wired into broker validation. A new unsupported field type fails generation rather than becoming an unconstrained guessed schema.

Subjects currently have `helivanta.in.<module>.<event>.vN` form. `in` is part of the existing subject contract; runtime bus namespacing may add an environment namespace. Do not derive patient country, residency or routing authority from that token. See `pkg/events.Bus.Subject` and deployment configuration.

The existing envelope is:

```json
{
  "event_id": "00000000-0000-4000-8000-000000000001",
  "type": "VisitCreated",
  "version": 1,
  "occurred_at": "2026-10-22T00:00:00Z",
  "tenant_id": "00000000-0000-4000-8000-000000000002",
  "data": {
    "visit_id": "00000000-0000-4000-8000-000000000003",
    "patient_name": "Synthetic Example",
    "department": "OPD",
    "patient_id": ""
  }
}
```

This is synthetic. `destination_tenant_id` is optional; tenant-less credential-revocation broadcasts legitimately use an empty `tenant_id`. Event `type` is **not uniform**: older modules use labels such as `VisitCreated`, while newer consumers check the full subject string. `patient_id` in the current visit payload is serialized even when empty. Current envelopes do not universally contain source product, origin hospital, aggregate version, correlation or causation IDs. Their addition/requiredness is a target review item, not a current guarantee.

The [envelope schema](../../contracts/api/event-envelope.schema.json) describes this current wire shape. Select the payload schema by the declared broker subject; do not assume `type` is interchangeable with that subject.

## Delivery, retries and provenance

Business state and outbox insertion share a PostgreSQL transaction. Dispatcher publication is at least once. Durable consumers claim `(consumer,event_id)` and apply local changes in one transaction; rollback rolls back the claim. An `AfterCommit` hook runs outside that transaction on every delivery, including already-claimed events. This permits a failed durable workflow start to retry after the business/inbox commit. A broker ACK must not be mistaken for downstream clinical completion.

Current bus constants include a five-delivery ceiling and a 30-second ACK wait; handling includes dead-letter behavior, and configured stream retention is bounded. These are implementation details to verify when changing workload/worker configuration, not a universal retry SLA. Do not describe jittered exponential retries or permanent/transient error classification as implemented everywhere. See `pkg/events/bus.go`, DLQ tests and retention configuration.

Broadcasts invalidate every replica's cache; they do not replace durable PostgreSQL state. Missed revocation invalidation must degrade to authoritative checks/cache expiry, not forgotten revocation. Durable work-queue consumers and fanout broadcasts have different delivery models.

A destination tenant is permitted only for a registered directed subject. The event retains origin tenant; consumers validate destination, purpose and local lineage. Cross-tenant writes need explicit provenance columns and current policy. The reference ping module demonstrates directed delivery; emergency receiving implements a constrained receipt path. Neither is a general authorization bypass.

## Integration journey matrix

| Journey                                          | Current implementation                                                                                                                                     | Target handoff and recovery                                                                                                        |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Patient registration → encounter                 | Patient registration event is ID-only; visit accepts optional patient ID without hospital lookup                                                           | Validate current hospital registration/care authority; unify reference requirements without breaking old rows                      |
| Visit → lab/pharmacy                             | Both consumers create pending tasks for every visit; name snapshot still present                                                                           | Explicit clinician order/prescription contracts; no automatic clinical act from a visit event                                      |
| Lab result → chart/portal                        | Result event published; no registered chart/portal consumer                                                                                                | Authorized released-version projection, amendment/withdrawal and critical acknowledgement                                          |
| Prescription → dispense → charge                 | Legacy task completion event; no full signed-order/stock/charge saga                                                                                       | Safe reservation, exact fulfilment, source charge-line ID; physical dispense leaves charge pending on billing failure              |
| Admission/transfer → housekeeping/billing/safety | Not implemented in MediCore                                                                                                                                | [Bed specification](domains/beds-and-admissions.md); strongly consistent owner occupancy and versioned asynchronous projections    |
| Supply → pharmacy                                | Supply-party and outlet inventory foundations exist                                                                                                        | Separate dispatch/receipt postings, transit and discrepancy reconciliation; no shared stock writes                                 |
| Workforce hospital name → pharmacy               | `hospital_named.v1` `{hospital_id, name, name_version}` announced once per version by a 30 s background pass; pharmacy keeps the newest version per tenant | Name shown only on a verified affiliation (`hospital_name`, else null); pharmacies and suppliers are never announced               |
| Workforce/supply upload → scan                   | Configured event-to-Temporal bridges and private file lifecycle                                                                                            | Exact-generation verdict, retry attempt identity and reauthorization; scanner outage keeps quarantine                              |
| Credential review → reminder                     | Configured workforce bridge                                                                                                                                | Matching durable workflow and current validity/policy; reminder is not renewed authority                                           |
| Emergency dispatch → receiving                   | Explicit link/request/command and directed receipt workflows                                                                                               | Current acceptance, timeout/escalation, physical handover and reconciliation; no inferred government integration                   |
| CRM → discovery/onboarding                       | Ownership defined in backlog; no module implementation                                                                                                     | [CRM contract draft](domains/crm-and-mediconnect.md); idempotent inbox, owner/version rules, pending sync and independent DPA gate |
| Hospital referral → another hospital             | Proposed sharing architecture                                                                                                                              | Minimal consent/purpose-bound disclosure, receiving acceptance and explicit cancellation limits                                    |

## Review standard for each new event

Record producer and responsible team, subject/version, exact envelope `type`, JSON Schema, synthetic examples, data classification, source authority and grants, consumer/durable name, expected ordering, deduplication scope, retry/DLQ/replay procedure, retention and deletion handling, latency/freshness target and reconciliation owner. Document events with no consumers as such; a publication is not an implemented end-to-end integration.

Use additive compatible changes within a subject version. Required new fields, removals, type/meaning changes and tightened consumer validation need a new version or an explicit coordinated migration. An optional producer field can still break a consumer with `DisallowUnknownFields`; test actual consumers. Do not broadcast demographics to make a projection easier. If a new metadata-only visit version is approved, migrate both current intake consumers before retiring v1.

For multi-step work with waits, preserve the existing Temporal direction. Define workflow IDs, activity idempotency, deadlines, human signals, compensation before irreversible actions and reconciliation after them. No new orchestrator or deployed service is introduced by these docs. Follow [#667](https://github.com/tesserix/helivanta/issues/667), [#485](https://github.com/tesserix/helivanta/issues/485), [#5](https://github.com/tesserix/helivanta/issues/5), [#6](https://github.com/tesserix/helivanta/issues/6) and [#710](https://github.com/tesserix/helivanta/issues/710).

The [department handoff candidate](workforce-department-links.md) uses Workforce request/Organization observation messages with retained full identifier claims, explicit boolean results, duplicate recovery and atomic audit/outbox. It does not publish Staff data or grant operating authority.
