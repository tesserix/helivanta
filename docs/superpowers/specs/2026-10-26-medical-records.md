# Medical record ownership and release

Status: design under development for #72 and the backend dependencies of #73;
minimal patient-owner reference projection implemented locally.
No medicalrecords module, storage workflow or patient report API is implemented by
this document. Practitioner verification foundation is merged in #1353 but remains inactive;
overall practitioner clinical authority is unfinished.

## Verified starting point

Inspected main `fa31004`, `internal/bootstrap/modules.go`, patientmaster's
`hospital.go`, `patients.go`, `module.go` and the upload implementation in
Workforce. There is no registered medicalrecords module. Workforce uploads own
staff evidence, not patient documents. Its PNG/JPEG inspector and quarantine
protocol do not establish PDF, plain-text or DICOM-reference support. The current
MediCore visit accepts an optional UUID without proving a hospital-scoped patient
relationship; it is not a validated encounter reference for this feature.

Patientmaster owns patient registrations and hospital visibility. Its current
hospitalTransaction holds the current patient hospital grant through commit and
records sensitive reads. The existing GET patient response includes demographics;
it is not a minimal cross-owner record-admission contract. No exported patient
registration reference contract was found in its contract package. MediConnect
claim/guardian/consent decisions do not themselves authorize disclosure of an
individual document. Existing module grant tables must converge under ORG-AC-08;
medicalrecords must not add a separate records-officer grant ledger.

## Owners and scope

Medicalrecords owns document identities, immutable versions, document-type
catalogue, storage-generation/checksum receipts, processing and release history,
retention decisions and disposition evidence. Patientmaster remains the identity,
claim/guardian and consent-ledger owner. The encounter owner must validate an
optional encounter against the same registration/hospital; until that contract
exists, an arbitrary visit UUID must not be accepted as a verified encounter.
Country-profile policy owns approved retention applicability; no universal Indian
retention duration is supplied here. LabConnect owns diagnostic sign-off and
patient-release decisions; this service cannot release an interim result merely
because bytes are available. IAM owns assignments, permissions and scope epochs.

Every resource is tenant/hospital/patient scoped. Staff commands require a current
shared IAM permission and an authorized registration relationship, independently
of supplied IDs. A patient/proxy read additionally requires a current approved
claim/authority, applicable purpose/consent and the document owner's current
release decision. These are conjunctive checks, not interchangeable grants.
Administrative templates must not acquire patient contents by configuration access.
Missing permission is 403, invisible objects are 404, unavailable authorization is
503. IN-ID-01/04/06/07/08/09 and ORG-AC-01/02/03/04/06/07/08 apply.

## Contracts and correctness decisions

1. Resolve the patient through a published minimal owner contract, not a second
   patient master or cross-module table query. It must bind actor, tenant,
   hospital, patient, operation/purpose and source version. Define its revocation
   boundary and test it against real grant changes before record writes rely on
   it. An old read response cannot authorize later finalization or download.
2. Separate upload intent, quarantined content, clean scan, classification,
   finalization, patient release, supersession and disposition. Persist truthful
   pending/failed states; no pending object enters a released listing. Reauthorize
   after external I/O and before sensitive state commit or byte release.
3. Use immutable command IDs and canonical intent. The issue's suggested hash
   uniqueness/409 conflicts with its acceptance requirement for 200 replay.
   Exact command replay returns the original receipt; changed intent conflicts.
   Equal bytes across different encounters/classifications are not automatic
   authority to merge two records. Duplicate-content review preserves context.
4. Server-verify hash, size, media structure and exact immutable storage generation.
   Apply configured per-type ceilings before transfer and enforce them again at
   storage confirmation. Malware/unsupported-content uncertainty blocks release.
   PDF handling needs an actual isolated validation/scan path; do not relabel the
   existing image-only inspector as PDF support. DICOM wrapper references are
   owner-validated references, not arbitrary fetch URLs or PACS pixel storage.
5. Finalized content/classification are immutable. Corrections append a new
   version with reason, exact predecessor and optimistic version guard. Preserve
   old bytes, signatures, clinical provenance and restricted historical access.
6. A raw object-storage signed URL is a transferable bearer capability; its
   signature alone cannot meet #72's different-session replay rejection. Serve
   through a short-lived session-bound authenticated release endpoint via Global
   API, checking current authorization and owner release after storage I/O.
   Never send public bucket links or trust a URL as a patient-access grant.
7. Retention requires a versioned reviewed policy with jurisdiction/service,
   document class, effective date, legal hold and accountable approval evidence.
   Do not infer three years or any other legal default. Missing applicability
   blocks finalization/disposition with an explicit unavailable-policy state;
   it must not fabricate an expiry. Erasure casework and physical disposition
   remain separate decisions. Durable timers and replay preserve disposition
   evidence; no automatic hard deletion until the reviewed policy authorizes it.
8. OCR is advisory, per-tenant optional, and must use the authorized data region.
   A tenant declining OCR remains functional through metadata search. After clean
   scan, OCR failure does not falsely reject an otherwise valid filed document;
   show failure/unavailable status and bounded durable retries. Search is private,
   scoped and audited; no OCR text, titles or document contents in events/logs.
9. File/replace/release/withdraw/dispose facts use typed contract events inserted
   into the local outbox with the owner state. Consumers deduplicate event IDs and
   fetch current authorized owner projections. Events do not grant disclosure.
10. Patient download, onward sharing and offline copies are separate decisions.
    Previously exported bytes cannot be recalled by revoking a server URL. An
    approved offline/export policy and visible validity limits are required before
    enabling offline content or general OS sharing; never claim immediate offline
    revocation. Patient report metadata must include actual file size, version,
    release state and freshness rather than an inferred successful attachment.

## Capacity and failure envelope

Planning assumptions, not measured production demand: 20 uploads/s peak at 4 MiB
median is 80 MiB/s object ingress. Keep bulk bytes outside ordinary JSON workers;
quarantine storage confirmation still verifies generations and tenant intent.
Model 100,000 documents/hospital/year across 100 hospitals: 10 million/year and
30 million version rows at 36 months before growth/corrections, approximately
40 TiB/year at the median size. Storage/retention cost requires actual tenant
volume and cloud pricing before approval. Catalogue configuration is a separate
small workload; do not shard merely to address object byte volume.

Story targets are 98% successful first-attempt filing, searchable OCR within
60 seconds p95, signed-access issue under 200 ms p95 and eligible disposition
within 24 hours. Proposed metadata availability is 99.9% monthly and p99 under
300 ms excluding object transfer/OCR. Measure end-to-end Global API latency and
storage bandwidth before asserting these targets. Set explicit worker concurrency,
request/object-size/time budgets and per-tenant quotas from that measurement.

IAM/patient/consent/release/retention/audit outage denies the dependent sensitive
operation. Object-store outage retains retryable intent without a filed row.
Scanner uncertainty leaves quarantine; OCR outage leaves metadata functionality
with explicit OCR state. Broker outage retains the transactional outbox. Temporal
worker outages retain durable work and timers; no synthetic success or cron-only
retention fallback. Valkey is an optional bounded-TTL cache, never the authority
for release/withdrawal, retention or consent.

## Delivery and acceptance evidence

Develop the full owner workflow in correct slices, retaining the full story scope:

- Versioned type/retention configuration and published owner reference contracts.
- Patient-scoped immutable intents, real media/storage/scan receipt validation and
  interruption/replay recovery, including dependency outages.
- Finalization/versioning/search/OCR, attributable audit and real source lineage.
- Owner-authorized staff/patient/proxy release and withdrawal through Global API.
- Retention/legal hold/disposition with durable replay and qualified policy review.
- Schema/FHIR mapping against the selected published IG and actual consumers;
  structural JSON validity alone is not ABDM or clinical conformance.
- Runtime/IAM/workflow registration, public repo CI, reviewed GitOps provisioning,
  matching migrations/workers, deployment and signed-in synthetic UI acceptance.

Each requires positive, unauthorized tenant/hospital/patient, stale revision,
revoked grant/claim/consent/release, duplicate/concurrent command, media mismatch,
malware, wrong generation, storage/audit/outbox/OCR failure and recovery tests.
Assert exact bytes and persisted state, not a route or passing inventory check.
Also prove immutable correction/history, legal hold versus expiry, incompatible
session replay rejection and no interim/unreleased record in patient lists.

Additive forward-only migrations retain private evidence/history. Use the owner
rollback plan, including compatible retained readers/workers once new versions or
release decisions exist. No production schema/rollout is authorized by this draft.
Do not close #72/#73 or claim MediConnect complete from configuration or uploads.

### Registration reference projection

Add GET `/v1/patients/:id/registration-reference?hospital_id=:hospital` to the
patient owner, routed through Global API. It requires existing `patient.read` and
patientmaster's current hospital visibility, with the grant held through the
scoped read/audit transaction. It returns only `patient_id`, `hospital_id` and
`record_access_authorized:false`. It is a current reference read, not a signed or
durable authorization receipt, and has `Cache-Control:no-store`; consumers must
not cache it as permission for a later write, finalization or content download.
It neither validates an encounter nor grants records access. Supplied IDs and
client tenant/actor headers cannot select authority. Require exactly one hospital
selector and canonical nonzero patient UUID (legacy v4 identifiers remain valid).

Read permission denial is 403, missing/foreign registration or hospital is 404,
expected-tenant mismatch is 409, and inability to read/authorize/audit the current
reference is 503 without identifiers or database details. No new grant table,
role assignment, migration or event is needed. Use the existing patient access
read audit, preserving actor/hospital/registration attribution. The canonical
response is a data-only patientmaster contract; no demographic wire model is copied.
A five-second request budget bounds this indexed single-registration lookup and
its audit. This closes reference projection only; record authorization still
requires the medicalrecords/IAM and disclosure contracts above.

Reference projection validation first reproduced the missing route (404), then
passed through the actual Global API proxy and owner harness. Focused tests passed
(6.028s): exact minimal payload/audit, foreign patient/hospital, ambiguous selector,
revoked hospital grant, expected-tenant mismatch, absent read permission and real
audit/hospital-authority database faults with recovery. Acceptance-tagged patient
lint and contract generation/check passed. Full patientmaster race/coverage passed (117.139s, 80.0% statements), Global API
race/coverage passed (5.016s, 86.7%) and matching passed (3.116s, 100%). The first
architecture run identified missing singleton/privacy declarations; classification
and its exact security pin now describe the authenticated HTTP-only patient ID.
A subsequent run used an incorrect Docker environment; rerunning with the
isolated test socket passed the full architecture race suite (32.012s). Vet,
API/migrate builds, contractdocs tests, lint, secret and diff checks passed.
There is no production deployment or medical record release.
