# Patient and care workflow guide

[Domain index](README.md) · reusable design constraints, not new API contracts

## Patient identity and authority

Patientmaster owns registrations and local MRNs; MediConnect account/link intake
does not grant access to clinical records. Use the
[current patient contract](../api/domains/patients-and-clinical.md) and
[MediConnect authority contract](../api/mediconnect.md), including their unresolved
release gates. Preserve identifier issuer/system, validity and verification state.
Patient, encounter, practitioner, organization and location IDs identify different
resources; stable identifiers do not authorize access.

Search for possible existing registrations before creation. Similar names, DOB,
shared phones or national identifiers are review signals, not automatic merge or
cross-hospital disclosure rules. Correct demographics with provenance; an approved
merge needs traceability, downstream reconciliation and recovery. Keep unknown
patients, estimated ages, preferred names, language/accessibility needs and assisted
registration in the applicable workflow. Never infer guardian or disclosure
authority from an emergency contact, payer or family relationship.

## Scheduling and care transitions

An appointment reserves planned care; an encounter records care delivered. Neither
proves admission, bed occupancy, nursing completion or discharge. Consult the
[clinical](../api/domains/patients-and-clinical.md) and
[bed/admission](../api/domains/beds-and-admissions.md) contracts; use their approved
states, not an example lifecycle copied from a general guide.

For inpatient, outpatient, emergency, day procedure, community or virtual care,
identify the actual service owner and unsupported gaps. Scheduling must account
for current clinician privileges/availability, appointment type/duration, room and
equipment, waitlists/cancellations and relevant referral/funding requirements.
Interpreter, mobility, escort and transport needs are distinct constraints.
Concurrent allocation must not double-book a scarce resource; rescheduling and
notifications preserve lineage and do not imply the receiving party acknowledged.

Keep clinical discharge, physical departure, financial close, receiving handover
and bed cleaning/readiness separate. Staff need visible responsibility at each
handoff and a safe fallback for unavailable dependencies.

## Clinical information and medication

Conditions, allergies, observations, notes, immunizations, referrals, care plans,
medication intent, administration, results and documents remain separate owner
records or authorized projections. A FHIR resource name is a conceptual mapping,
not permission to introduce a parallel master/table or claim FHIR support.

Retain patient/encounter references where applicable, author/source, effective and
recorded times, status, verification and version lineage. Patient-reported history,
draft notes, signed facts, corrected results and historical records must be visibly
distinct. Unknown allergy status is not a reviewed negative finding. Preserve
corrections/entered-in-error evidence instead of overwriting signed history.

Medication reconciliation across admission/transfer/discharge needs an accountable
reviewer. Drugmaster owns coded identity/formulation versions; MediCore owns
prescribing, PharmaConnect dispensing and stock, and administration is a separate
clinical fact. Preserve exact formulation, strength, dose unit/denominator, route,
frequency, duration, reason for change and source version where the owning contract
defines them. A catalogue import is not a prescription, allergy check or verified
interaction engine. Data-use rights, clinical content validation and local
regulatory approval are separate gates. Never fill missing doses, interactions,
schedule restrictions or high-risk medicine policies from model memory.

For diagnostics, preserve order, specimen/study identity, result version, release,
review and follow-up acknowledgement. Delivery or opening a report does not prove
clinical review. Follow the [laboratory contract](../api/domains/laboratory.md);
late/amended results need an accountable reconciliation path.

## Terminology and integration meaning

For SNOMED CT, LOINC, diagnosis/procedure classifications and medicine terminology,
preserve system URI, code, display, edition/version and context. Check the deployed
profile, licence and mapping approval. Similar labels or differing units are not
equivalent concepts. Versioned mappings and unmapped/ambiguous states must remain
visible; do not invent a clinical mapping to pass a validator.

HL7 v2 messages, FHIR exchanges and DICOM imaging serve different boundaries. Read
the [integration contracts](../api/integration-contracts.md) and owning canonical
schemas. Track external IDs, correlation, acknowledgement level and source version;
distinguish transport receipt, application acceptance and work completion. Use
durable retries/replay and reconcile duplicates, out-of-order updates and partial
failure without reverting corrected clinical facts. Sending an event or passing a
schema validator does not prove end-to-end interoperability.

## Access, usability and operational evidence

Enforce verified tenant and narrower resource scope, current purpose/consent/proxy
authority and sensitive-read/write audit at the server. Break-glass requires its
approved policy and reason; it is not an agent's ad hoc permission bypass. Protect
exports and attachments with owner authorization. Keep PHI, precise locations and
credentials out of routine logs, URLs, analytics and test fixtures. Retention,
disclosure, correction and legal holds follow reviewed applicability, not a generic
delete button.

Test relevant journeys with users under time pressure: patient context remains
visible, current versus historical data is clear, warnings are actionable, keyboard
and assistive technology work, and errors permit safe correction. Avoid duplicate
entry and unapproved clinical alerts that create alert fatigue.

Operational reports define measures, denominators, units, currency, timezone,
source/version and freshness. Missing data is unavailable, not zero. Reconcile
charges/claims/payments with their owner; a dashboard is not another ledger.
Measure integration queue age and unacknowledged handovers under approved escalation
rules. Restoration, reconciliation and downtime procedures need exercised evidence;
a backup or retry setting alone is not proof of recovery.

Choose synthetic tests by the changed risk: ambiguous identity, wrong scope,
revoked proxy, stale versions, concurrent scheduling, unknown allergy, amended
result, duplicate command, lost acknowledgement, timezone/DST boundary, audit
failure and downtime recovery. Obtain qualified workflow review without claiming
clinical, privacy or regulatory certification from software tests.
