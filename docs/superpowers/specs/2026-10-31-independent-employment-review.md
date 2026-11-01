# Independent employment terms review

Status: immutable submission/read and independent-decision backend/SDK candidate
implemented locally, with separate scoped reviewer assignment setup. Review
workspace/protected bytes, operational activation and full acceptance remain open.
No deployment or normal-browser/provider acceptance is claimed. Continues
A01–A12, #1035/#417 and [ADR-0049](../../adr/0049-reviewed-employment-access.md).
The [employment lifecycle design](2026-10-29-reviewed-employment-periods.md),
[current-person review design](2026-10-31-pre-employment-person-review.md) and
[consumer inventory](../../qa/staff-operational-access-inventory.md) remain
prerequisites. This record-review stage must not be presented as operational
employment activation or complete Access/RBAC.

## Verified sources and missing behavior

Workforce currently owns mutable employment drafts and immutable draft revisions,
released-file employment bindings, independent person findings and reviewed
Staff/login links. The intake role prepares inactive joiners without assigning
operational access. The current candidate freezes exact person-finding, terms and evidence references
in an immutable submission. The independent decision candidate now approves or
ends reviewed terms; no operational activation consumes them yet. Identity review permission does not authorize employment approval.

Migration 0059's contributor/creation-capture candidate supplies the missing
employment-evidence provenance and refuses incomplete historical identity-file
provenance. It does not freeze employment submissions or create review authority.
The existing local QA API/database have not received that upgrade. Existing
approved person findings are inputs, never proof of approved employment or login.

Workforce owns the records, selected evidence, contributors, review receipts and
eligibility basis. IAM owns reviewed scoped assignments, membership and session
incarnations. Zitadel owns global credentials. The UI calls Global API; no
cross-module SQL, alternate master or direct client/product route is introduced.

## Authority and disclosure

The preparer holds both current hospital-scoped `workforce.enrollment.manage` and
`workforce.documents.manage` through the existing versioned HR-intake template.
An independent reviewer needs a separately declared owner permission
`workforce.employment.review` and an additive versioned employment-review template.
Do not expand an existing identity reviewer or historical template definition.
Both actors need active independently reviewed Staff/login bindings and live
credential, membership and original assignment authority. Staff status/job title,
hospital manager rows or tenant-admin permission alone are insufficient for the
new review commands. IAM's existing independent assignment decisions approve the
reviewer assignment; review does not grant the actor's own authority.

The verified tenant/hospital contains each target Staff, period, selected binding
and person finding. Targets may be inactive and may have no digital account.
A finding's declared account remains a declaration until separately reviewed
binding/provisioning verification. No account is manufactured for a non-digital
worker. Reject an actor's own Staff, declared target subject and historical
Staff/login associations; an absent current target login does not prove
independence. Preserve the complete relevant preparer/contributor exclusions.

Assets are private appointment/contract terms, identity associations, protected
bytes and trustworthy review history. An insider, another tenant's member or a
stale/replaced authority must not alter or disclose them. Every owner operation
checks current actor, verified scope and legal transition. Return 404 for
invisible objects, 403 for missing scoped authority, 409 for workspace/source/
version/intent changes, 401 for a revoked credential and 503 for unavailable
current identity/IAM/owner authority. Expected tenant and subject assertions are
mandatory and exact, and never constitute authority.

## Immutable submission

A UUIDv7 submission and command identify one immutable proposal. The bounded
command names the exact current period revision/version, current target Staff
version/source epoch/hash, exact approved person-finding ID/revision/version and
one to twenty explicit evidence-binding ID/revision/version selections. Require
an applicable appointment policy reference, reason, terms-examined attestation
and known-associations disclosure. Do not invent dates, employment eligibility,
notice periods, mandatory government identifiers or statutory HR policies.

Validate the person finding's current approved state, validity, identity hash,
source epoch, selected evidence and contributor completeness inside the owner
transaction. Preserve the finding's original source version separately from the
current employment-preparation source; a matching name or email is not a join.
Terms must be the current open draft revision at submission. Validate contractor/
agency party and accountable sponsor requirements from the existing terms model.
Select only current attached bindings for that exact revision whose released
bytes and scan metadata still match. Freeze current classification revision as
well as binding, release generation/checksum and scanner lineage.

Store immutable references to the owner's existing canonical terms history and
freeze selected evidence/contributors, rather than copying mutable head rows or
introducing another contract master. Derive relevant preparers from canonical
terms/evidence histories, Staff preparation and captured document contributors.
No missing legacy origin or unbounded/truncated contributor set is accepted as
complete. Later writes cannot extend the submitted child set or rewrite the
original source. Historical rows remain readable under current authorization.

## Independent decisions and current state

The submitted state is pending. An independently authorized reviewer may approve
or reject with an expected version, distinct command, mandatory reason and actual
human examination/independence attestations. The preparer may withdraw a pending
submission; an authorized reviewer may revoke an approved record. Preserve every
revision and decision. A rejected, withdrawn or revoked review cannot return to
pending or become approved by replay. Exact retry reauthorizes and returns the
original command receipt; changed intent under that command ID is 409.

Pending approval rechecks the exact current preparation head, source, finding,
evidence, validity and exclusions after locks. A changed draft invalidates that
pending approval and requires a new submission. An already approved terms record
is immutable; editing a new preparation draft does not itself terminate an
existing operational employment period. Operational activation/renewal consumes
an explicitly selected reviewed record in its own authoritative transition.
Neither this review nor a finding ID alone changes directory status, employment
incarnation, login link, IAM grant, clinical privilege, card or payroll.

Read responses distinguish recorded review status from current applicability.
Unknown/expired/revoked/source-changed/unreleased evidence never looks eligible.
Receipt history cannot repair an uncertain command or confer current authority.
The UI labels approved terms separately from employment/access activation.

## Protected bytes and transaction boundaries

Preserve lock order: current IAM scope/membership, hospital supervision, reviewed
actor Staff/link, target Staff, period/review and selected bindings/documents in
stable identifier order. Use current owner time after waiting. Head, immutable
history/receipt, selected evidence/contributors, minimized audit and identifier-
only outbox commit in one owner transaction. Failed writes roll back all effects.
Broker outage after commit leaves durable publication pending, not a grant.

Use the existing five-second scoped command budget bounded by session expiry.
Remote identity refresh/storage/scanning remains outside owner row locks. A
review-specific evidence download pins original permission/assignment(s), actor
link/source epoch, review revision, target source, selected binding and released
bytes across I/O, then reauthorizes and verifies generation/length/digest before
release and successful-download audit. No general file permission is conferred.
A replacement actor link or assignment cannot complete an older in-flight read.

Use additive owner migrations, scoped keys/foreign references, immutable child
sets/receipts and forced RLS. Reviewer read purpose is restricted to its selected
Staff/period/finding/evidence; preserve existing WITH CHECK/delete boundaries.
Do not widen all enrollment/private-file/history policies to make reviewer reads
work. Existing rows with missing provenance remain unresolved. Application
rollback retains history and cannot restore ended operational authority.

## Delivery, budgets and complete-goal gates

Reuse the existing unmeasured planning assumptions: 50 peak owner requests/sec,
500,000 Staff at twelve months, 1.5 million at thirty-six months, 99.9% availability
and p99 below 500 ms. Commands remain at most 8 KiB; cursor pages default to 50,
maximum 200. Bound evidence at twenty bindings and contributors at 200. Stored
child snapshots add to the draft-only storage estimate; measure representative
and maximum cases before capacity claims. Reuse the existing Postgres/monolith;
no new always-on service, cache, worker or provisioning cost is justified.

Implement canonical Go/Zod inputs and actual signed Global API missing-route
regression first, then owner schema/commands, scoped reviewer assignment and the
normal preparer/reviewer workspace together. Prove lost responses, exact retries,
changed sources, self/historical review, contractor requirements, missing origin,
foreign scopes, stale/concurrent decisions, expiry/lock waits, actor/assignment
replacement, dependency outages and failed audit/outbox writes. Run generators,
contractdocs, affected race/coverage/lint/build and real browser/provider gates.

After record review, implement the reviewed-source-to-operational-generation
transition required by ADR-0049. Future renewal approval must not invalidate a
currently valid period early. Effective suspension, accepted exit and expiry must
deny at authoritative boundaries without relying on cleanup timers. Rehire/resume
requires fresh reviewed incarnation, links and access decisions. Complete all
registered and separately wired background consumer guards and legacy/governance
review, preserve other hospitals and accountable history, and verify every
A01–A12 journey before full Access/RBAC acceptance or activation. Migration and
production rollout approval remains specific to the validated revision/action.

## Submission implementation checkpoint — 2026-11-01

Workforce 0060 and the canonical employment-review generator now implement
strictly scoped PUT/GET pending submissions. A signed Global API regression
observed the missing endpoint's 404 before implementation. Compound scoped-only
intake authority preserves other intake callers' compatibility; an independently
reviewed scoped Staff Admin works even with legacy permissions present. Owner SQL
pins scope/references, immutable child transaction identity, complete origin,
exact contributors and current sponsor/finding validity. Audit/outbox failure
probes prove all durable effects roll back together and exact retry recovers once.
Before-0059 fixtures now apply the entire actual forward suffix in order.

A sponsor-inactivation regression initially returned `pending` rather than
`source_changed`; source revalidation now covers both submission/current reads
and database source/commit checks. This is a metadata-source test using the
existing synthetic legacy fixture, not proof of reviewed suspension or offboarding.
The SDK verifies original scope, source, canonical terms/binding projections,
command intent and cancellation. It exposes historical/current distinctions.
All subsequent independent-decision, UI/provider and operational gates above
remain required; pending submissions grant no employment or access authority.

## Independent-decision source checkpoint — 2026-11-01

Workforce 0061 and IAM 0021 now implement independent decisions/endings and the
separate exact-permission reviewer template/catalog. The SDK verifies legal
status/version, current capabilities and original command/actor/intent; Staff
Access separately verifies the employment catalog before offering assignments.
Signed API, direct owner SQL, scoped source isolation, historical author and
source/sponsor/evidence regressions pass. Approval leaves the joiner inactive and
later preparation drafts preserve approved historical terms. Audit/outbox failure
probes roll back decision head/history/evidence/publication together.

Full corrected owner race suites, Go vet/build/lint and all 22 web gates pass;
see the delivery tracker for exact commands, elapsed times and the scanner-role
policy/calendar-test regressions caught by broad verification. No existing fixture
upgrade, production migration/rollout or release occurred. Review workspace,
protected bytes/inbox/history/selection and normal browser/provider acceptance,
operational generation and every A01–A12 gate remain required.

## Selected-review history slice — 2026-11-01

Add the protected `/:review/history` collection using the existing explicit
preparation/reviewer modes and current no-fallback owner authority. Workforce
remains the sole owner; the typed Global API client is the consumer. Preserve
tenant/hospital/Staff/review SQL predicates, current IAM assignment and reviewed
active actor-link locking, exact original workspace assertions, forced RLS and
the existing sensitive-read audit. The assets and insider/foreign-tenant/stale-
authority threats above apply without broadening private-document authority.

Reuse the unmeasured 50-peak-owner-requests/sec and 99.9%/500ms p99 planning
budgets above. This endpoint is read-only business behavior but writes one audit
per successful page, so it uses the primary owner transaction rather than a stale
read replica or cache. Current legal transitions bound one review to three
receipts; the existing `(tenant_id,hospital_id,id,version)` history primary key
bounds row lookup before sorting. Hydrate frozen terms and at most twenty selected
evidence bindings once per page rather than once per historical receipt. The
existing 500,000/1.5-million Staff assumptions do not justify another datastore,
worker or always-on service. No measured demand, latency or capacity claim is made.

Current IAM, identity and owner authority are critical: failure denies with the
existing unavailable response; audit persistence failure returns no private page.
No storage/provider call occurs inside this read. Default/max list limits remain
50/200, with the normal cursor envelope and strict single mode. Pagination fields
are accepted only on this registered history route; command/detail query shapes
remain strict. Ended records and unavailable evidence retain their historical
receipts without returning current capabilities or evidence bytes.

The SDK validates canonical owned receipts, continuation consistency, order and
uniqueness, frozen-source consistency within a page and cancellation. A consumer
must preserve the original workspace across pages, label the result historical
and obtain current detail before acting. This is a bounded history prerequisite,
not the employment-review inbox, preparation selector, protected byte release or
normal reviewer UI. The existing data-preserving schema needs no migration or
runtime activation; application rollback removes only the route/client surface
and retains all employment decisions/history. Enabling operational employment
enforcement remains subject to ADR-0049's separate unfinished activation gates.
