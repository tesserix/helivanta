# Reviewed hospital pharmacy department bindings

Status: implementation design; no production activation or regulatory policy.
Related: #418, #429, ADR0024, ADR0044, ADR0051, ADR0052 and ADR0060. This is a
dependency of all five reusable Staff foundation requirements, not their
replacement. Standalone employing establishments remain subject to ADR0054.

## Source findings and ownership

Pharmacy owns immutable pharmacy/outlet registration and explicit outlet access.
Organization owns current hospital department revisions. Workforce owns current
Staff actors, employment and postings. IAM owns independently reviewed scoped
grants. Licence pharmacist name/registration fields are historical licence text;
affiliation verification is a separate relationship. Neither is the binding below.

The current Pharmacy Staff source is read-only canonical directory browsing.
There is no reviewed outlet-to-department binding, professional acceptance or
Staff-specific product duty. Legacy dispense rows also lack trusted hospital and
outlet lineage. No change here rewrites those rows or derives lineage from a
selected hospital. Product binding records must contain references, not copied
Staff, department or professional masters.

Use the inherited unmeasured budget: 50 peak requests/s, 80:20 reads/writes,
2–20 KiB responses, 500,000/1.5 million Staff at12/36 months, p99<500ms and
99.9% availability. Lists are bounded to50 records/page, with keyset cursors and
an index beginning with tenant/outlet. Reuse Postgres, current IAM/OpenFGA,
Organization/Workforce guarded owner ports and the existing outbox. No service,
cache, queue, infrastructure resource or new software dependency is required.

## Reviewed relation and authority

The relation asserts which current canonical department runs one hospital-owned
outlet. It does not assert that all Staff in that department work at the outlet,
that the outlet is licensed, that a registration is accepted or that a person may
dispense. Independent/chain pharmacy organizations are not assigned a fabricated
hospital. Their employer and branch journeys remain in the parent objective.

Each immutable proposal pins tenant, pharmacy, outlet, hospital, department and
department revision, with an opaque UUIDv7 command, normalized protected reason,
proposer, current actor observation and server time. An independent reviewer
must hold current explicit hospital-scoped binding-review authority and current
editor access to this exact outlet. The proposer needs corresponding binding-
prepare authority and current editor access. Both require a current reviewed
digital Staff actor in the named hospital. Neither role is clinical authority.

The new IAM catalog has exact product-owned prepare and review templates, each
also containing current Staff-actor use and canonical department-choice read.
It extends the existing canonical template system for this real second product
consumer; there is no module-private grant vocabulary or broad default role.
The platform's coarse TenantAdmin permission remains insufficient without the
independently approved assignment. The assigned hospital is the authorization
boundary; no department-head inheritance is inferred. The new exact Pharmacy
actor consumer is reviewed under ADR0052 and its architecture allowlist.

At most one binding is active per outlet. Activation is immediate, matching the
current structural owner's effective-time contract. Future scheduling is not
represented by backdated or pretend-current rows. Reassignment is an explicit
independently reviewed replacement that names the current binding/version;
approval ends the previous current relation and activates the replacement in
one transaction. Retirement ends a relation and retains both proposal and
decision evidence. Current department retirement/revision change makes the old
relation unusable until a fresh proposal is approved; no silent retargeting.

## Commands, disclosure and failures

Proposed additive owner routes live below the existing exact pharmacy/outlet
path and enter through Global API:

- `PUT /staff-department-bindings/{binding}` records a proposal; exact retry
  returns the retained proposal, changed intent under the same id is409.
- `POST /staff-department-bindings/{binding}/commands` accepts an immutable
  command id, expected version and approve/reject/withdraw/end decision. Review
  commands carry protected reasons and explicit relationship attestation.
- `GET /staff-department-bindings` lists bounded metadata/history, never Staff
  or licence payloads. Current authorizations and outlet scope are mandatory.

Canonical inputs will generate Go/Zod models. Responses distinguish pending,
approved, rejected, withdrawn and ended records and show current-source
availability separately from retained approval. Recovery reads an exact command
receipt; a network timeout is not proof the command failed. No delete endpoint.

The routes include `/hospitals/{hospital}` below the exact pharmacy/outlet path.
The verified tenant and source hospital are pinned from the refreshed principal
and server-checked command, never from client headers as authority. Missing
permission is403, hidden tenant/outlet/hospital is404, stale/conflicting source or
command is409, invalid payload400, unavailable IAM/OpenFGA/owner503. Sensitive
responses are no-store; unavailable checks withhold rows and callbacks.

Acquire IAM guarded authority, current Staff-actor source and Organization
structural references before Pharmacy binding/outlet locks. The actor and exact
outlet editor grant remain held through commit. Serialize activation on the
immutable outlet row so two approvals cannot both become current. A department
change or grant revocation either waits behind the admitted command or wins
first and denies it. Actor/source deadlines constrain the final commit even if
callback cancellation is ignored. Audit and event publication failure roll back
the complete command. Logs/events carry identifiers, versions and transitions;
private reasons and credential content remain with their owners.

Approval is independent of proposer and linked Staff subject, including retained
login associations where applicable. A proposal cannot approve itself through a
second login associated with the same Staff record. Rejection/withdrawal preserve
history without claiming source eligibility. Exact receipt replay never repeats
another binding decision audit/event. Each admitted receipt disclosure retains
current actor/source-use and relationship-read evidence. Audit failure withholds it.

## Vertical acceptance and subsequent consumers

The real Pharmacy Staff workspace must offer canonical hospital/department
choices under current assignment; no typed UUID or copied department list is the
normal flow. Separate normal accounts prepare, review and reload a binding.
Changed session/scope clears private panels; failed reads show unavailable,
never a retained binding as current. Related-role edits create a replacement
proposal rather than modifying approved history.

TDD covers actual Postgres/IAM/OpenFGA and the Global API: positive independent
approval, proposer/subject refusal, foreign/sibling scope, revoked editor/actor/
assignment, stale department/current binding, duplicate/changed-content command,
both competing approval orderings, grant/source writer races, owner outage,
audit/outbox failure, final deadline and preserved history. UI tests cover exact
catalog validation, required fields, pending/uncertain commands, denied/revoked
scope and independent review. Run affected full suites, lint/vet/build,
generation/parity checks and normal-role browser acceptance before release.

Next consumers reference this reviewed relation plus their own exact current
Staff posting. Professional acceptance additionally consumes Workforce-owned
credential/evidence reviews, a qualified jurisdiction/service policy and current
registration state. It must not copy licence text or private certificate data
into a new master. PIC/cover, assistants, operating actions and legacy-lineage
reconciliation remain explicit unfinished requirements. IN-PH02/04/06/10 require
current primary sources and qualified review before consequential activation.

Persist additive migrations and source contracts; named production migration and
rollout approval remains separate. Rollback removes mounting/consumer use while
retaining additive schema and all historical evidence. No policy or grant is
activated by this design document.

## Purpose-limited choice projections (implementation design, not mounted routes)

The normal-role workspace needs canonical names rather than typed identifiers.
Keep the parent Staff planning budgets (50 peak requests/s, 80:20 reads:writes,
500,000/1.5 million Staff at12/36 months, p99 under500ms, 99.9% availability).
The following projections add no service, cache or replica. A request scans one
bounded page, never all Staff or all outlets. Their latency is an unmeasured
budget, not a performance result.

Workforce owns proposed GET `/v1/workforce/staff-actor-scopes`. Reuse its existing
own-login-link candidate query and filtered-list mechanism, with a maximum20
candidates and at most four concurrent authorizations. Return only hospital ID
and current canonical hospital name after independently approved
`workforce.staff_actor.use` and the existing current Staff-actor guard admit that
candidate. Pin the candidate login-link identity/version to the admitted actor;
a changed link is withheld. Current employment generation, credential and commit
deadlines remain enforced by the existing guard. This is a reusable own-scope
projection, not private Staff directory access or posting/operating eligibility.
Use canonical `contracts/workforce-staff-actors.json` for this new metadata shape;
do not reimplement the existing Go authorization port in a wire model.

Pharmacy owns proposed GET
`/v1/pharmacy/hospitals/{hospital}/staff-binding-outlet-choices?purpose=read|prepare|review`.
Default purpose is read. Maximum10 choices contain only canonical pharmacy/outlet
IDs and their owner-record names. Hospital assignment and current Staff actor
admission precede enumeration. Read requires exact `pharmacy.staff_binding.read`;
prepare/review require their corresponding exact permission as well as read.
Each disclosed outlet additionally requires its current explicit outlet grant
(reader/editor for read, editor for preparation/review) and hospital-owned parent.
No affiliation, onboarding or regulatory status becomes operating authority.
Do not expose other hospitals' binding identifiers or names through these choices.
Extend the existing Pharmacy binding canonical contract for this projection.

Preserve IAM → Staff actor → Pharmacy lock order. For a page of outlets, acquire
the existing outlet gates/access locks in ascending canonical outlet-ID order,
then recheck grant/ownership before disclosure; presentation remains in stable
created-at/ID keyset order. Revocation either precedes admission or waits behind
the authorized read. Do not disclose the candidate names if authority, source,
outlet access or audit recording is unavailable. Retain attributable protected
read evidence inside the same guarded transaction. Names alone grant no action;
all subsequent choices and commands reauthorize their exact target.

Both projections require matching single expected-tenant and expected-subject
headers. Opaque pagination binds verified tenant, subject, route and purpose/
hospital where applicable; an empty filtered page may have a continuation.
UI follows continuation instead of treating an empty page as complete. Invalid
or repeated query parameters fail400, stale account intent409, unavailable
dependencies/audit503; denied candidates are withheld. Clearing private rows and
pending panels on account/scope changes remains mandatory.

The UI composes own Staff actor hospitals, authorized Pharmacy outlets and
Organization's existing canonical department choices. It never requests broader
catalogue/private HR roles to populate a selector. Concurrent metadata changes,
wrong/sibling scope, source/grant revocation, fault rollback and bounded paging
must be proved through real owners/Global API before normal-role browser
acceptance. These endpoints remain implementation work; this section does not
assert that they exist, pass tests or are activated.
