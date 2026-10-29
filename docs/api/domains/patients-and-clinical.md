# Patient identity and MediCore specification

[Contract index](../README.md) · current behavior and integration gaps

## Implemented patient boundary

`patientmaster` owns hospital registrations, MRNs, demographics, consent receipts, duplicate override evidence, reported blood group and patient-access audit. Read/write access requires the verified tenant, route permission and a current hospital grant. `hospital_id` is a query selector checked by the backend, not tenant authority. Registrar writes lock the grant through commit; a role or membership alone does not confer patient access.

Authenticated public patientmaster routes support the optional
`X-Helivanta-Expected-Tenant` precondition described in the
[HTTP contract](../http-contract.md). A mismatch or invalid supplied header returns
409 `session_changed` before domain data access; it does not select a tenant or
grant patient/hospital authority. Existing callers without the header are unchanged.

Current routes are `POST /v1/patients`, read-only `POST /v1/patients/search`, `GET /v1/patients/{id}`, and hospital-access management. The [inventory](../reference/README.md#patientmaster) lists exact paths and permissions. Search terms stay in JSON bodies to reduce disclosure through URLs. Invalid/invisible hospital or patient references return 404. Patient data responses use the custom JSON projection in `patients.go`, not the database row struct.

Registration requires a body `request_id`, given name, calendar DOB, notice version and patient/guardian consent attribution. Family name, sex, mobile and address have separate optional rules. Guardian and duplicate-override conditions are validated in the handler. A reported blood group is an attributed patient/guardian/document report, not a laboratory-verified transfusion result.

The request ID identifies registration intent; reusing it with different input is rejected. Confident duplicate matches block normal registration, possible matches are surfaced, and authorized overrides require a reason and audit. Do not merge on phone or normalized name. The registered patient and consent evidence commit together. The `registered.v1` event exposes patient and hospital IDs only; demographics must be fetched through the authorized owner.

Source: `backend/internal/modules/patientmaster/{patients,search,hospital,access}.go`, `contract/events.go`, module migrations and `matching/`. Related: [#980](https://github.com/tesserix/helivanta/issues/980), [#473](https://github.com/tesserix/helivanta/issues/473), [patient field review](../../architecture/patient-registration-field-review.md).

## Implemented clinical slice

MediCore currently supports:

| Operation                   | Request/response                                                 | State and recovery                                                                                             |
| --------------------------- | ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `POST /v1/medicore/visits`  | `{patient_name, department: OPD\|IPD, patient_id?}` → 202 `{id}` | Inserts an `open` visit and its outbox event in one local transaction. No documented request deduplication key |
| `GET /v1/medicore/visits`   | Standard cursor page of visits                                   | Tenant-scoped; nullable/omitted `patient_id` supports older records                                            |
| `GET /v1/medicore/insights` | Counts grouped by OPD/IPD                                        | An operational count, not authoritative bed occupancy or admission reporting                                   |

See the [typed current OpenAPI](../../../contracts/api/clinical.openapi.json). The optional patient reference is parsed as a UUID; this handler does not establish a hospital-scoped patient lookup or prove that the referenced patient exists or is visible. Consumers must not treat the field as a validated care relationship.

The current visit event includes `patient_name` and reaches lab/pharmacy pending-task consumers. This is a PHI-bearing legacy contract exception to newer metadata-only events. Document broker/outbox access and retention; do not copy this payload into general analytics or commercial CRM. A future metadata-only version requires consumer migration rather than removing the name from v1.

The lab intake currently creates a pending default `CBC` order for every visit, and pharmacy creates a pending dispense task. These are scaffolded integration behavior, not clinician-authorized diagnostic orders or prescriptions. A real clinical ordering workflow must replace that automatic fanout with explicit authorized order intent before clinical reliance.

### OPD encounters, allergies and weight

The OPD encounters design (#80, `docs/superpowers/specs/2026-10-27-opd-encounter-design.md`) covers encounters under `/v1/medicore/hospitals/{hospital}/encounters`. Allergies and weight (#1373, [design](../../superpowers/specs/2026-10-27-allergies-and-weight-design.md)) are patient records under `/v1/medicore/hospitals/{hospital}/patients/{patient}/`.

Each route requires:

- the caller's MediCore grant at that hospital;
- the patient registered there.

Anything else returns 404.

| Operation                                                                                                 | Contract                                                                                                                                                                                                                                                                                                                  |
| --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/allergies`                                                                                         | `{status: unknown\|no_known_allergy\|allergies_recorded, no_known_allergy, entries, needs_coding, recording_enabled}`. `unknown` is what no record means; it is never stored and never shown as "none" (IN-CL-08). Audited read                                                                                           |
| `PUT …/allergies/{uuidv7}`                                                                                | Records an entry. It is idempotent and returns 503 until the reaction and severity lists are clinically approved (`MEDICORE_ALLERGY_RECORDING_ENABLED`). A `drug_molecule` must be one the drug master published (422 otherwise). A `drug_unlisted` entry is accepted but `needs_coding`, which blocks prescribing checks |
| `POST …/allergies/{id}/corrections`                                                                       | `{expected_version, status: resolved\|entered_in_error, reason}`. Adds a new version, keeping the original. A stale version returns 409                                                                                                                                                                                   |
| `GET …/allergies/{id}/history`                                                                            | Every version of one entry. Audited                                                                                                                                                                                                                                                                                       |
| `PUT …/no-known-allergy/{uuidv7}`                                                                         | A clinician's "asked, none known". Refused (409) while any allergy is active. A later entry outranks it, and a later resolution returns the status to `unknown`                                                                                                                                                           |
| `PUT …/weights/{uuidv7}`, `POST …/weights/{id}/entered-in-error`, `GET …/weights`, `GET …/weights/latest` | Kilograms to three decimals, from kg or g. Each weight records when it was measured, how, and who recorded it. "Latest" skips entries in error and reports `age_days`. The recency a prescribing check requires is #87's rule                                                                                             |

Events `allergy_changed.v1` and `weight_recorded.v1` carry ids only. MediCore learns published molecules from `drugmaster.molecule_published.v1` into the global, read-only `medicore_drug_molecules`.

The real browser acceptance journey is
[`e2e/tests/opd-encounters.spec.ts`](../../../e2e/tests/opd-encounters.spec.ts)
(#1389). It creates synthetic hospital/grant/registration fixtures through owner
APIs, waits for MediCore's registration consumer, and verifies consultation,
unknown/negative/coded allergy states, weight attribution/recency and hospital
access denial. The synthetic catalogue uses drugmaster's release CLI and actual
molecule consumer. Allergy recording is opted into only on the synthetic e2e
API process; this does not activate or approve a production clinical policy.

## Target clinical contracts requiring review

MediCore owns encounter state, clinician orders, signed notes/prescriptions, nursing, clinical discharge, theatre/day-care and hospital charge intent. Do not create patient tables in each product. Every downstream request needs the owning patient registration, encounter, ordering resource/version, hospital scope, purpose and authorized actor.

Before implementing appointments/queues, prescribe/sign/amend, OT, oncology, ICU, maternity, blood-bank, radiology or discharge, specify:

1. Owner and lifecycle with legal transitions and sign-off authority.
2. Required patient/hospital reference validation, consent/purpose and emergency exceptions.
3. Durable request identity and concurrency behavior; amendment/correction history rather than overwrite.
4. Published facts and authorized read projection; no dependency on another module's writable table.
5. Exact financial/stock/diagnostic handoff and recovery owner, including irreversible clinical actions.
6. Synthetic positive, cross-tenant/hospital, stale-state, duplicate and dependency-failure acceptance cases.

Beds have a [separate detailed draft](beds-and-admissions.md). Use [#81](https://github.com/tesserix/helivanta/issues/81), [#983](https://github.com/tesserix/helivanta/issues/983), [#991](https://github.com/tesserix/helivanta/issues/991), [#993](https://github.com/tesserix/helivanta/issues/993), [#995](https://github.com/tesserix/helivanta/issues/995), [#996](https://github.com/tesserix/helivanta/issues/996) and the exploration ledger to retain detailed workflow coverage. Issue [#472](https://github.com/tesserix/helivanta/issues/472) requests FHIR R4 canonical resources; current visit/patient endpoints are not a FHIR implementation. FHIR profiles, terminology, validation and clinical mapping need their own approved contracts.

## MediConnect registrar identity review

Patientmaster owns the hospital-scoped link-review worklist and immutable evidence
attestation. `patient.register` and an explicit current hospital registrar grant
are both required; the requester cannot be the reviewer. See the complete
[MediConnect review contract](../mediconnect.md#explicit-hospital-registrar-review).
Approval validates the claimed registration and its captured version; it does not
establish proxy authority, clinical disclosure rights or patient/proxy consent.
Review input and outcome cross owners only through typed transactional events.

## Pharmacy affiliation review (#1416)

Patientmaster owns the hospital's decision on a pharmacy's claimed affiliation. The `patient-pharmacy-affiliation-request` and `patient-pharmacy-affiliation-withdrawal` consumers queue the claim from pharmacy events. Either event may arrive first. A withdrawal that overtakes its request is stored already withdrawn.

- `GET /patients/pharmacy-affiliations?hospital_id=` lists the hospital's requests, newest first, with each decision's reviewer and reason. Every read is audited.
- `PUT /patients/pharmacy-affiliations/:request/decision/:decision?hospital_id=` takes `{outcome, reason}`. The outcome is `accepted` or `declined` from pending, or `revoked` from accepted. The reason is 1 to 500 characters.

Both routes need `patient.pharmacy-affiliation.review`, which no role holds (tenant administrators only), **and** a current `registrar` grant at that hospital. Without the grant the answer is `404`. The requester cannot accept their own request. A changed input under the same decision id, or the wrong state, is `409 review_conflict`. Decisions are append-only in `patient_pharmacy_affiliation_decisions`. An insert trigger checks the session's own registrar grant, the state and self-acceptance, then moves the review's status, so status cannot drift from history. `pharmacy_affiliation_decided.v1` carries ids, outcome and time only; reviewer and reason stay here. Each decision is re-sent until pharmacy confirms it (`patient_pharmacy_affiliation_deliveries`, from 2 minutes up to 60, never given up). A revocation replaces an acceptance that has not yet been confirmed. Patientmaster in turn confirms every request and withdrawal it handles.

## Pharmacy patient lookup (#1416b)

A named pharmacist can find one of a hospital's patients by exact MRN, for a pharmacy whose link with the hospital is active on both sides. See the [design](../../superpowers/specs/2026-10-29-pharmacy-affiliation-verification-design.md#patient-lookup-slice-1416b).

- **When a link is active.** `patient_pharmacy_links` gets a row when pharmacy confirms it applied the acceptance (`applied: true`), the review is still `accepted` under a row lock, and no older round's link stands. The row is deleted in the same transaction as a revocation, or as a withdrawal reaching patientmaster.
- **Grants.** `PUT` and `DELETE /patients/hospitals/:hospital/pharmacy-lookup-access/:pharmacy/:subject` grant and revoke; `GET …/pharmacy-lookup-access?pharmacy_id=` lists them.
  - They need `patient.pharmacy-affiliation.review` and a registrar grant. Granting needs an active link; otherwise the answer is `409 no_active_link`.
  - A grant is tied to the link's round: it stops working when the link ends, and re-verifying the link starts with no grants.
  - The grant table is never consulted for registry access.
- **Lookup.** `GET /patients/pharmacy-lookup?hospital_id=&pharmacy_id=&mrn=` needs `patient.pharmacy-lookup` (pharmacist).
  - It locks the grant and the link, then matches the exact MRN (trimmed, upper-cased). It returns `{patient_id, name, age_years, age_estimated, sex}`.
  - Or it takes `patient_id` instead of `mrn`, never both (#1387), for a stay pharmacy resolved from a bed. The same grant, link, answer and audit apply. Row security admits the one patient the session names by MRN or by id, and a session naming both sees nothing.
  - No grant, no active link, another pharmacy or tenant, and no match all return the same `404`.
  - A lookup session sees at most the one `patients` row its MRN names, through `patient_pharmacy_lookup_allowed`. It needs the session subject's grant in the link's current round. Consents, identifiers, duplicate overrides and blood groups are refused to it outright.
  - Every lookup, a miss included, writes `patient_access_audit` (`pharmacy_lookup`, with `pharmacy_id`). If that write fails, the lookup fails without answering.

## Guardian authority ownership

Patientmaster additionally owns the reviewed guardian authority registry, its
hospital/subject RLS, immutable evidence references and terminal revocation.
Parental and appointed-guardian receipts remain separate from self-authority,
signed clinical declarations and authorized clinical release. Global API forwards
the [documented routes](../mediconnect.md#reviewed-guardian-authority-registry).
MediConnect claim approval alone cannot establish proxy consent or record access;
future consuming owners must check the live claim and approved purpose as well as
current authority and their own resource authorization.

The in-development proxy declaration uses a separate capture command and signed
schema v2 in the existing clinical-consent ledger. It requires a current reviewed
guardian authority and a capacity-specific approved notice; it does not reinterpret
self v1, grant ordinary patient reads or authorize record release. See the
[proxy declaration release gates](../mediconnect.md#proxy-clinical-sharing-declaration-work-in-progress).

## Minimal registration reference

`GET /v1/patients/{id}/registration-reference?hospital_id={hospital}` returns only
`patient_id`, `hospital_id` and `record_access_authorized:false`. It is a current
visibility read for a consumer resolving an owner reference, not record intake,
encounter validation, a durable permission receipt or clinical disclosure.
Existing `patient.read` and a current hospital grant are both required; the grant
is held through the scoped read and committed patient-access audit. The response
uses the data-only `patientmaster/contract.RegistrationReference` source and is
`Cache-Control: no-store`. Consumers must independently authorize their own
resource actions and recheck current dependencies before release.

Exactly one canonical nonzero hospital selector and a canonical nonzero patient
UUID are required; existing v4 patient IDs remain supported. Missing/foreign
patient or hospital is 404, missing read permission is 403, expected-tenant
mismatch is 409 and unavailable visibility/database/audit is 503
`registration_reference_unavailable`. Errors contain no identifiers or database
details. This route is implemented in the local development branch; deployment
acceptance remains unfinished. It adds no new grant ledger, role or schema.
