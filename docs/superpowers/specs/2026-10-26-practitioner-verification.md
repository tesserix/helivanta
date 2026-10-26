# Practitioner effective verification

Status: issuer policies and registration attestation APIs implemented locally;
overall practitioner verification remains unfinished. Not registered or deployed. Continues #474 and
ADR-0043 without activating prescribing, clinical privileges or HPR fetches.

## Owner and trust boundary

Practitioner owns registration identity/verification. Workforce remains the owner
of HR enrollment; facility roles/clinical privileges and patient/proxy consent are
separate. Current registration/source-review/evidence facts are implemented in the
registry foundation. Positive source observations alone remain unverified. Separate administrative
attestations can establish time-bounded registration data trust.

Issuer interpretation must be explicit tenant configuration, not a model-invented
legal rule. Registration policy configuration requires its own manage/read
permissions, an exact currently accepted steward appointment and an opaque review
reference to the accountable source assessment. Administrative access alone does
not supply that appointment. No policy is installed by default. Configuration is
not proof of qualified legal review or a professional privilege grant.

## Registration issuer policy

An immutable tenant-local policy identity binds one council source_system to a
closed canonicalization rule (exact or ASCII uppercase) and an IANA expiry calendar
time zone. Those identity semantics cannot change silently. Versioned complete
policy editions contain a required source freshness duration, explicit date rule
(date_required or allow_undated_with_attestation), validity window, review reference
and accepted appointment/version. Source freshness is 1–31,536,000 seconds as a
product bound, not a statutory period; there is no supplied default. Date rules
and freshness require qualified tenant review before real use.

PUT /practitioners/registration-policies/:policy records immutable intake; PUT its
/revisions/:revision path records a complete edition with expected_version. Exact
replay returns the retained receipt, changed intent/stale expected versions
conflict. Current GET and bounded list return current editions using immutable
policy creation ordering. Retained GET/list expose edition history. Source_system
is unique within the tenant, not across customers. Policy identity creation is
configuration and cannot reserve an individual's registration identifier.

Head, edition and audit commit atomically. Database guards repeat accepted-current
appointment, time/bounds/identity checks and prohibit history rewrite or head
advance without an attributable edition. Locks follow policy head then appointment.
Read permission and committed audit are required for sensitive reads/replay and
empty lists. The existing 8 KiB decoder, five-second transaction, two-second lock
and 1–200 keyset bounds apply. No external I/O, new service or cache is needed.
Policy storage stays bounded per request; source/history growth must be measured
against the registry release envelope before activation.

Migration 0013 is forward-only and undeployed. It retains policy history on rollback.
A policy edition can outlive its signer's appointment: acceptance is checked when
that edition is created, and prior signed authority is not erased by later staff
changes. New editions require current authority. Expired policy never supplies a
current verification default; availability/error must be explicit.

## Registration attestation requirements and remaining gates

A verification command must bind exact current registration revision, a matching
source review and its released generation-bound evidence, current policy edition,
independent current reviewer and source-time/calendar facts. Canonical identity
ownership is established only by that authorized verified decision; unverified
claims cannot reserve identifiers. Duplicate normalized identifiers across people
conflict and require explicit reconciliation, never silent merge. Non-ASCII values
must not be silently case-folded by an ASCII policy. Undated registration requires
an explicit attestation when the reviewed date rule permits it; unknown expiry must
not be treated as perpetual by default.

A current projection must re-evaluate registration/practitioner state, source age,
policy validity, evidence revocation, revision changes and explicit decision
revocation. Historical decisions remain immutable. Its freshness/validity ceiling
must constrain downstream role projections and cache TTL, and dependency outage
must fail closed. FHIR/canonical identifier mapping, HPR linkage, conflict handling,
qualified review, expiry events and current/historical clinical authority remain
release gates. Configuration alone does not satisfy them or close #474.

## Local configuration validation

The first HTTP test reproduced the missing route (404), then passed against the
real module harness. Tests cover explicit missing/invalid rules, accepted versus
nominated/expired/revoked authority, separate permissions, foreign scope/forced
RLS, immutable issuer identity and edition history, 12 concurrent exact replays,
version/intent/source conflicts, bounded history pagination and retained replay
after later editions or appointment revocation. Audit/outbox failures roll back
configuration; read audit failure blocks data, including empty collections. A new
test initially used an unregistered fixture token (401); registering its limited
policy permissions preserved the intended unaccepted-appointment 403 assertion.

Policy refresh events carry only policy ID/version; actual rules require an
authorized owner read. No event is a permission grant. All 65 module harness routes
reject stale workspace. Registration decisions and canonical identifier ownership are now implemented
in the local harness. Runtime bootstrap, qualifications/HPR/facility privilege
consumers, policy activation and
production migration remain separate incomplete gates.

## Organization-access boundary

This work records administrative credential-data attestations. It creates no role,
assignment, delegation, clinical privilege or workflow approval. IAM permissions
must come from the shared authorization owner; current data-review appointments
are existing provenance/qualification records and must not become an alternate
IAM grant source. The existing appointment model needs ORG-AC-08 convergence
before activation. ORG-D-* assignment/writer/workflow decisions remain open and
are not settled here. Shared workflow publication and organization-specific role
and approval templates remain activation gates (ORG-AC-01..04/07..10,
ORG-WF-01..06). No public/private institution hierarchy is hard-coded.

New attestation/revocation actions use current platform IAM checks and exact
credential scope; they do not add caller eligibility via another private grant
ledger. Claim submitters cannot attest their own credential facts. Consumers must
never interpret a positive registry attestation or refresh event as a clinical
permission. Clinical authority is explicitly not evaluated; current/historical
clinical roles require the separate scoped IAM/workflow integration.

Expiry interpretation also needs an explicit start-of-day or end-of-day boundary
in the configured issuer calendar. Draft policy 0013 is unpublished/undeployed;
its required expiry_boundary is added before publication, with no assumed legal
cutoff. Undated facts require a separate explicit attestation when the configured
policy permits them. The resulting trust ceiling is not a licence certificate.

### Registration attestation validation (2026-10-26)

The local handler/database harness now exercises immutable attestation and
withdrawal receipts, canonical identifier conflict retention without person
merges, concurrent exact replay, stale versions, independent reviewers,
unauthorized tenant scope, evidence revocation, policy/revision/source changes,
and audit/outbox rollback. Missing undated attestation and elapsed source age
are rejected; the current projection becomes stale at the exact lease boundary.
Administrative attestation always returns `clinical_authority: not_evaluated`.

Validation: practitioner race suite passed (111.233s), architecture suite passed
(37.095s), additional expiry-boundary tests passed (5.612s), practitioner vet and
build passed, acceptance-tagged practitioner golangci-lint reported zero issues,
contractdocs generation/check passed, and the practitioner secret scan found no
leaks. These are local tests, not proof of deployed service integration.

This remains a draft, inactive runtime implementation. Canonical identifier-conflict reconciliation,
ORG-AC-08 shared IAM/workflow convergence, identity/qualification/HPR and facility
privilege integration, global/runtime wiring and deployment acceptance remain
open. Migration 0014 is forward-only: receipt and identifier ownership history
must be retained; rollback requires forward correction rather than deletion.

Read-isolation follow-up: HTTP receipt/projection/history reads return not-found
for a foreign tenant. Direct database reads with missing module access or a
foreign tenant return zero verification/identifier rows. Injected read-audit
failure blocks every read surface and exact replay; recovery returns the original
receipt unchanged. Focused real-PostgreSQL tests passed.

Configured-calendar validation: five real-handler/PostgreSQL scenarios passed
(7.921s), covering Asia/Kolkata midnight under start-of-day and end-of-day rules,
the exact end-of-day boundary, the preceding second, and future issue dates.
Successful receipts assert the exact minimum of the issuer expiry and source-age
ceiling, rather than merely asserting a future timestamp. These use synthetic
policy configuration and do not establish a legal issuer expiry interpretation.

## Source-observation reconciliation (local implementation)

An adverse observation must remain immutable. A later positive observation alone
must not erase it. The local implementation adds an explicit tenant-local administrative reconciliation
receipt naming the current registration revision, one active replacement review,
1–10 unique adverse review IDs, and an accountable reason reference. The active
review must concern the same registration/current revision and have a strictly
later source-check time than every named adverse observation. Reconciliation
records data-review judgment; it never certifies legal standing, grants a clinical
privilege, merges practitioners or transfers canonical identifiers.

The caller needs current shared IAM `practitioner.registration.verify`, verified
tenant scope and independence from identity, claim, revision and evidence
submission. Do not add a module-private caller appointment/grant path. Source
review appointment provenance remains existing evidence, subject to ORG-AC-08
before activation. Missing permission is 403, foreign/missing objects are 404,
and unavailable authorization is 503. Sensitive reads/replay require audit.

Command: PUT `/practitioners/:practitioner/registrations/:registration/source-reconciliations/:reconciliation`.
Body: `revision_id`, `active_review_id`, `adverse_review_ids`, `reason_reference`.
GET at the same path and bounded GET collection expose retained receipts under
record-read permission. Canonical intent includes sorted unique adverse IDs and
original actor; exact replay returns the original result, changed intent conflicts.
The command locks the registration root, verifies the current revision and all
same-tenant/object references, then appends receipt, audit and refresh outbox in
one short transaction. Pre-read audit commits before rejected/failed commands.
No network call or cache participates in the decision. Outbox failure rolls back
receipt; database/audit/IAM outage fails closed.

Attestation/current projection ignore an adverse observation only when an explicit
receipt binds that exact observation to the exact active review used by the
attestation. Unnamed/new adverse findings still block. Changed revisions, evidence
withdrawal, source freshness and policy expiry keep their existing fail-closed
checks. Reconciliation does not reactivate a withdrawn verification: a separate
new versioned attestation is required. Identifier collisions remain a distinct
unresolved path; no implicit ownership transfer is introduced.

Capacity uses the existing registry release assumptions and five-second request/
two-second lock budgets. Each command reads at most ten named adverse records and
one replacement review; add indexed registration/review lookups and an indexed
receipt linkage. History is keyset bounded at 1–200. No new deployment or service
cost is introduced. A forward-only additive migration retains receipts on rollback;
only compatible readers/guards may be activated together. Qualified review,
shared IAM/workflow convergence, runtime wiring and production acceptance remain
activation gates. This implementation is a draft, unregistered and undeployed. Identity ownership
conflict recovery and qualified-review activation remain unfinished.

Reconciliation tests first reproduced the absent command (404). The local
implementation passes explicit-adverse-history/replay and newer-adverse-finding
checks. A missing-review regression reproduced 500 instead of 404; SQL no-data
errors now map to not-found. The focused suite passed (6.763s), including audit,
read-audit and outbox outages, recovery, retained ownership/history and forced RLS.
Full practitioner race/coverage passed (140.778s, 84.7% statements); architecture
tests passed (49.069s). Acceptance-tagged practitioner lint/vet, API/migrate
builds, contractdocs generation/check/tests and secret/diff checks passed.
These results are local only; source reconciliation is not active in production.
