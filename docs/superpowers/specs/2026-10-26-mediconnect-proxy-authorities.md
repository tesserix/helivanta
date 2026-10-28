# MediConnect reviewed proxy authorities

## Scope and ownership

Issue #552 requires different parental, appointed-guardian and adult-delegation
workflows. Patientmaster owns registration, hospital registrar access and authority
reviews. MediConnect owns account/claim identity; the Global API remains the public
entry. A family relationship, identity-link approval or this registry's acceptance
is not clinical/procedural consent or record-release permission. Existing self
v1 declarations and their signed receipts must not be reinterpreted.

This change implements the reviewed guardian authority registry, a prerequisite
for proxy consent and profiles. Adult delegation remains a separate explicit
patient-initiated offer, not a guardian-review shortcut. #552 stays open until its
remaining APIs, authorized projections and integrations are complete.

## Commands and data

Add migration `0009_patientmaster`; never edit applied `0008_patientmaster`.
A new `patient_proxy_authorities` ledger stores one canonical hospital/patient
binding, approved identity-review decision and registration version. Kind is
`parental` for an existing `child` claim, or `appointed_guardian` for a `ward`
claim. It pins the proxy subject from that review, never from a supplied subject
or contact. An independent current hospital registrar records policy, identity/age
and authority evidence references, explicit confirmations and a bounded validity.
Appointed guardians additionally require an appointment reference. These are
accountable hospital attestations about retained evidence, not an automated
external verification result or legal certification.

Only India is supported. Resolve declared country metadata through the canonical
country catalog. Parental validity cannot extend beyond the existing registration
age boundary, evaluated in the catalog's calendar zone; an estimated DOB cannot
establish that boundary. Calendar-year addition clamps leap-day births to the
last day of that month, conservatively ending this portal authority. It does not
certify a statutory majority date; qualified review must choose any earlier
necessary expiry. Appointment validity is explicitly reviewed rather than inferred
from age, disability or relationship. Current primary-source/legal review and real
notice approval are still required before enabling subsequent real proxy-consent
or disclosure workflows; no legal policy is activated by this registry.

Public routes, all with verified membership and the existing optional expected-
tenant assertion:

- `PUT /v1/patients/proxy-authorities/:authority`: registrar review permission,
  exact hospital registrar grant; 201 create, 200 exact replay, 409 changed intent.
- `GET /v1/patients/proxy-authorities/:authority` and
  `POST .../:authority/revoke`: current hospital registrar review scope.
- `GET /v1/patients/my/proxy-authorities` (bounded keyset page),
  `GET .../:authority`, `POST .../:authority/accept` and
  `POST .../:authority/revoke`: exact verified proxy subject and hospital.

States are proposed v1, accepted v2, and revoked v2/v3. Revocation is terminal;
replay never extends validity or reopens a revoked row. Acceptance checks the
local reviewed decision, current registration version and database-clock expiry.
It is a local registry transition; later access must also revalidate the live
MediConnect claim and its own signed, purpose-scoped declaration. Read responses
always carry `record_access_authorized:false`, not a capability token. History
and expired records remain visible to their authorized participants with no-store.

## Isolation, recovery and cost

Force tenant/hospital/subject RLS and immutable SQL lineage/state guards. Keep
registrar and participant transactions separate. Ordinary patient reads must not
inherit proxy access. For acceptance validation only, an exact selected-authority
context permits the owner handler to read the bound registration version; the
predicate checks verified actor, tenant, hospital, subject and reviewed claim.
It does not change the patient's write policy or return demographics. Audit that
source metadata read and all registry reads/writes. Atomically enqueue a new
ID/version-only event; no evidence, subject, demographics or relationship payload.

Use exact tenant/hospital/id indexes and owner keyset indexes. Each command is a
bounded 1–2 KB request and a single owner transaction; no network, cache, worker
or new service is needed for this registry. Existing portfolio load assumptions
remain 1,000/3,000 peak RPS at 12/36 months; this adds no capacity/SLO claim.
Withdrawal does not depend on external identity, signer or appointment providers.
Unknown/stale authority is not turned into access. Concurrent review/registration
changes and duplicate commands must preserve attributable single transitions.

Rollback code leaves retained ledger/history in place; do not drop it. Follow-up
proxy consent needs its own purpose/notice, immutable signed declaration, live
claim checks, majority/appointment revalidation and record-owner authorization.
Notifications, OTP development and SMS activation are not added here.

## Verification

Start with a real PostgreSQL/NATS failing HTTP lifecycle test. Cover independent
review, class/claim binding, unknown/adult classes, estimated DOB and validity,
exact owner/hospital/tenant authorization, no patient-read elevation, stale
registration/claim decisions, duplicate/conflicting/concurrent commands, terminal
revocation, SQL immutability and audit/outbox rollback. Exercise real Global API
forwarding and generated registry parity. Run affected race/coverage, build, vet,
lint and regression gates, then perform isolated synthetic deployed verification.
This registry does not close the full proxy-consent or family-profile stories.

## Reference review

ICTHospital commit `97da247a3b037da290298c3ddae900928750f236` exposes optional
`guardian_relation` and `guardian_mrn` patient metadata in `app/Models/Patient.php`
and `app/Http/Controllers/PatientController.php`. This informs terminology, without
evidence of independent authority review, proxy consent or secure record release.
No implementation was copied. Primary-source PDF refresh attempts failed during
this review; existing notes do not substitute for current qualified review before
real proxy-consent or disclosure activation.

Applicable India product acceptance criteria: IN-ID-02 (estimated DOB stays
uncertain), IN-ID-04 (relationship and scoped authority), IN-ID-06 (separate
purpose-specific consent) and IN-ID-07 (audit, revocation and no implicit record
access). This registry implements relationship-review evidence and lifecycle;
IN-ID-06 remains incomplete until the separate signed proxy-consent workflow.

## Regression finding

The existing clinical-consent lifecycle test initially failed: the draft migration
replaced the patients read policy without preserving its recipient resource
predicate. Restore `patient_clinical_recipient_resource_allowed` alongside the
existing registrar/self branches and the new narrow acceptance branch. The same
existing HTTP assertion is the regression oracle; do not weaken the clinical
recipient gate or treat denied legitimate consent as expected.
