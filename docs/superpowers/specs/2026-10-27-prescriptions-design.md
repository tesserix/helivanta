# Prescriptions

Resolves #83. Owner: MediCore (`docs/api/portfolio.md`: "MediCore owns encounters and
prescriptions; PharmaConnect owns dispensing"). DoctorConnect is MediCore's clinician-facing
surface, not a separate service. Feeds #170 (Prescription fulfilment).

Status: design. Nothing here is implemented, and no prescription can be signed until the
prerequisites in [Prerequisites](#prerequisites) exist.

## Why

Doctors write on paper; the pharmacy re-keys it, guesses at handwriting and substitutes on
judgement. PharmaConnect's dispense queue takes a free-text medicine name with no prescription
behind it (`pharmacy/dispenses.go`). A structured, signed, immutable prescription is what
makes dispensing safe (IN-PH-02, IN-PH-04) and what every later pharmacy capability —
verification (#169), fulfilment (#170), billing (#172), H1 registers (#171) — hangs on.

Prescribing is the highest-risk write in the platform. A wrong strength, a missed allergy or
an adult dose on a child causes direct harm. Paper cannot prevent these; structured
prescribing can, but only if its checks actually run.

## Goals

- A doctor composes a prescription against an open encounter: many lines, each naming a drug
  from a drug master (never free text), with strength, form, route, frequency, timing,
  duration, PRN, and a derived total quantity.
- Safety checks run before signing. A check that cannot run blocks signing.
- Signing is attributable to an authorised prescriber and freezes the prescription: drug
  identity, schedule class and check results are snapshotted at signing.
- A signed prescription publishes one `prescription_issued` event that PharmaConnect can
  dispense against (#170).

## Anti-goals (what would make this a failure even if it worked)

- **A prescription signed without its checks.** "Checks unavailable, sign anyway" is never
  offered. An unavailable allergy list, drug master or interaction source means no signature.
- **Authority from a role.** `doctor` role or a council number on file is not prescribing
  authority (IN-CL-10).
- **Invented clinical content.** No dose ranges, interaction pairs or schedule rules written
  from memory (IN-CL-11, AGENTS.md). Every rule comes from a named, versioned source.
- **Mutating a signed prescription.** Changes after signing are #84's revision, never an
  UPDATE (IN-CL-12).
- **Alert fatigue.** Hard stops stay rare; warnings need a typed reason; override rates are
  measured per rule so noisy rules can be tuned.

## Current state (verified in source, 2026-10-27)

| Needed                                                                | Exists today                                                                                                                                  | Evidence                                                      |
| --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Encounter                                                             | MediCore visit: `patient_name`, optional `patient_id` (no FK), `department` OPD/IPD, `status` always `open`, no consult mode, no update route | `medicore/module.go:39-73`, `visits.go:35-40`                 |
| Patient identity, age                                                 | `patients.id`, `dob`, `dob_estimated`                                                                                                         | `patientmaster/module.go:76-110`                              |
| Weight                                                                | none                                                                                                                                          | —                                                             |
| Allergies (with unknown ≠ no known allergy, IN-CL-08)                 | none anywhere                                                                                                                                 | #1373                                                         |
| Drug master (molecule, strength, form, route, schedule, interactions) | none. Pharmacy's catalogue is per outlet, has no schedule, and is not a prescribing source (IN-PH-02)                                         | `pharmacy/catalogue.sql:62-85`; #476, #159 open (Sam123ben)   |
| Prescriber authority                                                  | practitioner module exists but is **not registered**, has no link to an IAM subject, and its spec excludes prescribing                        | `bootstrap/modules.go:48-58`, `practitioner/records.sql:3-13` |
| Schedule / telemedicine rules                                         | none                                                                                                                                          | #87 open                                                      |
| Digital signature                                                     | none                                                                                                                                          | #88 open                                                      |

## Decisions

- **P1 MediCore owns it.** New tables in the MediCore module, routes under
  `/medicore/encounters/:visit/prescriptions`. Migrations `NNNN_medicore`, forced RLS,
  UUIDv7 ids, audit trigger, as `medicore/module.go` does.
- **P2 One prescription, many lines, bound to one encounter and one patient.** The encounter
  must carry a `patient_id` that resolves to a registered patient in the tenant; a visit with
  only a `patient_name` cannot hold a prescription. The patient is read from the encounter,
  never from the request.
- **P3 Lines reference the drug master by id and revision.** A line stores the drug master
  entry and the revision it was composed against. At signing, molecule, strength, form,
  route and schedule class are copied onto the line, so a later master change cannot
  reinterpret a signed prescription (IN-PH-01). Brand is an optional annotation.
  `no_substitute` requires a reason.
- **P4 Dosage is structured; quantity is derived.** dose value + unit, route, frequency code,
  timing relative to food, duration days, PRN. Total quantity is computed server-side from
  dose × frequency × duration and shown; a doctor may raise it only with a reason (e.g. a
  whole pack). PRN lines require an explicit maximum quantity. Frequency codes are a closed
  set defined with the drug master's units, not invented here.
- **P5 Lifecycle: `draft → signed`, plus `discarded` for drafts.** Drafts are editable by
  their author only and invisible to pharmacy and patient. A signed prescription is immutable
  (guard trigger, as `practitioner/registrations.sql`). Cancel and supersede are #84.
- **P6 Idempotent drafts.** `PUT …/prescriptions/:clientRxId` with a client UUIDv7 and an
  intent digest, as `practitioner/registration_verification.go:183-238`: same digest replays
  200, different digest 409. Concurrent edits use an expected version; stale is 409.
- **P7 Checks are one `Evaluate` with graded findings.** `info | warning | hard_stop`.
  Checkers: allergy (by molecule and cross-reactivity class), interaction (against this
  prescription and the patient's active medications), duplicate therapy, dose range (age;
  weight for children, within a recency window), schedule and consult-mode eligibility
  (delegated to #87's verdict). **Each checker returns `unavailable` when its source is
  missing, stale or erroring, and `unavailable` blocks signing like a `hard_stop`.** That is
  the fail-closed direction: an unchecked prescription is the harm this feature exists to
  prevent.
- **P8 Signing re-checks everything on the server.** `POST …/:id/sign` with
  `{expected_version, overrides: [{finding_id, reason}]}` re-runs `Evaluate` inside the
  signing transaction against the stored draft. Client-sent findings are never trusted.
  Every `warning` needs an override with a typed reason; any `hard_stop` or `unavailable`
  returns 409 with the findings. Overrides, the rule source versions and the #87 verdict are
  stored on the prescription.
- **P9 Who may sign.** The signer must be (a) the authenticated subject, (b) linked to a
  practitioner record with a currently verified registration, (c) holding a prescribing
  privilege at the encounter's facility, and (d) allowed by #87 for each line's schedule and
  the encounter's consult mode. Any of these unavailable → 503/409, never a signature. Reads
  follow encounter access; nurses may read, never sign. 404 across tenants.
- **P10 Signature strength (my call).** Signing is an attributable act by the authenticated
  prescriber with a fresh step-up authentication, recorded with time and subject. It is
  **not** claimed to be an IT Act digital signature; that is #88, and the prescription
  records which signing method was used so a later DSC/eSign does not reinterpret old ones.
- **P11 Event.** `helivanta.in.medicore.prescription_issued.v1`, published through the outbox
  in the signing transaction, exactly once. Payload: prescription id, version, encounter id,
  patient id, signed-at, and per line the snapshotted drug identity, schedule class, dose,
  route, frequency, duration, quantity and `no_substitute`. No patient name, no free-text
  instructions (minimised; PharmaConnect reads detail through an authorised query).
- **P12 FHIR is a projection, later.** Lines map to R4 `MedicationRequest`; the mapping is
  documented, but no FHIR endpoint ships in these slices (no FHIR claim without a conformance
  test, AGENTS.md).

## Prerequisites

Signing needs all of these. Each is its own issue and spec; this spec does not build them.

| #   | Prerequisite                                                                                                            | Issue                  | Owner                             |
| --- | ----------------------------------------------------------------------------------------------------------------------- | ---------------------- | --------------------------------- |
| R1  | Encounter carries a resolved `patient_id`, consult mode (in-person / tele, first / follow-up) and an open/closed status | #80 (OPD)              | unassigned                        |
| R2  | Drug master: molecule, strength, form, route, schedule class, units, versioned revisions                                | #476                   | Sam123ben                         |
| R3  | Interaction, dose-range and cross-reactivity data from a named, licensed source                                         | #476                   | Sam123ben — **source not chosen** |
| R4  | Patient allergy list with "unknown" distinct from "no known allergy", and weight observations                           | #1373                  | unassigned                        |
| R5  | Practitioner module registered, practitioner ↔ IAM subject link, facility prescribing privilege                         | #1374 (continues #474) | unassigned                        |
| R6  | Schedule and teleconsult eligibility verdict                                                                            | #87                    | unassigned                        |

## Slices

Each slice is correct on its own; none signs a prescription before R1–R6 exist.

1. **Drafts.** Tables, draft PUT/GET/discard, structured lines against R2, derived quantity,
   prescriber-only edit, RLS, audit. `Evaluate` with every checker wired; checkers whose
   source is missing return `unavailable`. `sign` exists and returns 409 `checks unavailable`
   naming the missing sources. Doctor UI: compose and evaluate on the encounter. Needs R1, R2.
2. **Signing.** Turns on as R3–R6 land: server re-evaluation, overrides, prescriber gate,
   immutability, the event, read API for PharmaConnect. Needs R1–R6.
3. **Dispense against it** — #170, PharmaConnect, consuming slice 2's event.

## Not covered

Life after signing (cancel, supersede, refills, sharing with the patient: #84); rule
content (#87); interruptive decision support beyond these checks (#86); inpatient drug charts
and administration (Nursing); dispensing and substitution (#169, #170); ABDM publication.

## Decisions only the product owner can make

1. **Interaction and dose-range data source (R3).** It must be a licensed, versioned
   dataset; nothing can sign until one is chosen. This is the critical-path decision.
2. **Who builds R2/R3.** #476 is Sam123ben's.
3. **Signature strength.** P10 ships authenticated attestation, not a digital signature;
   confirm that is acceptable for the pilot hospitals until #88.
