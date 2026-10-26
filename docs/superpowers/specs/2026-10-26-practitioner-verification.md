# Practitioner effective verification

Status: issuer-policy configuration implemented locally; effective verification
remains unfinished. Not registered or deployed. Continues #474 and
ADR-0043 without activating prescribing, clinical privileges or HPR fetches.

## Owner and trust boundary

Practitioner owns registration identity/verification. Workforce remains the owner
of HR enrollment; facility roles/clinical privileges and patient/proxy consent are
separate. Current registration/source-review/evidence facts are implemented in the
registry foundation. Positive source observations currently remain unverified.

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

## Still required after configuration

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
authorized owner read. No event is a permission grant. All 56 module harness routes
reject stale workspace. Runtime bootstrap, effective decision/normalized identifier
ownership, qualifications/HPR/facility privilege consumers, policy activation and
production migration remain separate incomplete gates.
