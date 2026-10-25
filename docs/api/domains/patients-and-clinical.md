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

## Guardian authority ownership

Patientmaster additionally owns the reviewed guardian authority registry, its
hospital/subject RLS, immutable evidence references and terminal revocation.
Parental and appointed-guardian receipts remain separate from self-authority,
signed clinical declarations and authorized clinical release. Global API forwards
the [documented routes](../mediconnect.md#reviewed-guardian-authority-registry).
MediConnect claim approval alone cannot establish proxy consent or record access;
future consuming owners must check the live claim and approved purpose as well as
current authority and their own resource authorization.
