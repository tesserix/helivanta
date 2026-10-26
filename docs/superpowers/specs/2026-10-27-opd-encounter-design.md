# OPD encounter, slice 1: a visit that knows its patient, hospital and consult mode

Resolves the first slice of #80. Prerequisite R1 of the
[prescriptions spec](2026-10-27-prescriptions-design.md) (#83). Owner: MediCore.

Status: design.

## Why

A prescription must be written inside an encounter that names a real, registered patient at
a known hospital, and records how the consultation happened (Telemedicine Practice
Guidelines 2020 make prescribing rights depend on mode and on first versus follow-up). Today
MediCore's visit cannot carry any of that:

| Needed                          | Today (`medicore/module.go`, `visits.go`)                                                                                                                       |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Patient                         | `patient_name` required; `patient_id` optional and caller-asserted. Nothing proves it exists or belongs to this hospital (patient-master spec D7 records this). |
| Hospital                        | none. Patients are hospital-scoped (`patientmaster/hospital.sql`); visits are not.                                                                              |
| Consult mode, first / follow-up | none                                                                                                                                                            |
| Status                          | always `open`; no route changes it, though `medicore.visit.update` is declared                                                                                  |
| Who may see it                  | any `doctor`/`nurse` in the tenant, across hospitals                                                                                                            |

## Goals

- An OPD encounter belongs to one hospital and one patient registered **at that hospital**,
  checked by MediCore, not asserted by the caller.
- It records consult mode (`in_person` | `teleconsultation`) and visit type (`first` |
  `follow_up`), fixed when the encounter opens.
- It has a real lifecycle: `arrived → in_consultation → closed`, plus `left_without_being_seen`
  and `cancelled`, each change attributed and kept.
- Only staff granted MediCore access at that hospital can see or change its encounters.

## Anti-goals

- **A second visit master.** The encounter is the visit, evolved in place; no parallel table.
- **Trusting the client for the patient or hospital link.** A patient MediCore cannot resolve
  at that hospital is 404, never an encounter with a dangling id.
- **Breaking today's journey.** Pharmacy and lab keep consuming `visit_created.v1` unchanged.

## Decisions

- **E1 Evolve `medicore_visits` into the OPD encounter (my call).** One owner, one table;
  `visit_created.v1` consumers keep working. New columns: `hospital_id`, `consult_mode`,
  `visit_type`, `version`, `closed_at`; `status` gains a CHECK over the six states. Existing
  rows have no hospital, so they are **legacy**: RLS hides rows with no `hospital_id` from the
  new hospital-scoped routes (as patientmaster quarantined its unassigned rows), they stay
  readable on the old list, and they can never hold a prescription. Nothing is guessed into a
  hospital.
- **E2 MediCore resolves patients from its own projection.** A consumer
  `medicore-patient-registered` on `helivanta.in.patientmaster.registered.v1` writes
  `(tenant_id, patient_id, hospital_id)` to `medicore_patients`, idempotently. No name or
  contact data. Opening an encounter requires that pair to exist; otherwise 404 "patient not
  found at this hospital". Fails closed: a missed event means no encounter, not one with an
  unverified patient. Patients registered before the consumer exists are not in the projection and cannot open encounters until patientmaster replays their registration events (#1377: MediCore never reads patientmaster's tables). _Changed during the build: the spec first proposed an operator backfill command, which would have read another module's tables._
- **E3 Hospital access is a MediCore grant.** `medicore_hospital_access (tenant_id,
hospital_id, subject)`, administered with a new `medicore.access.manage` permission,
  checked in the same transaction as every hospital-scoped read and write, as
  `patient_hospital_access` does. Tenant admins administer grants but do not read encounters
  without one. Missing grant or unknown hospital is 404.
- **E4 Routes are hospital-scoped.**
  - `PUT /medicore/hospitals/:hospital/encounters/:id` opens one (client UUIDv7, idempotent
    by intent digest; replay 200, different intent 409). Body: `patient_id`, `consult_mode`,
    `visit_type`.
  - `GET /medicore/hospitals/:hospital/encounters` (paged, `status` filter) and
    `GET …/encounters/:id`.
  - `POST …/encounters/:id/transitions` `{to, expected_version, reason?}`.
  - The old `POST /medicore/visits` and `GET /medicore/visits` stay for legacy callers;
    POST stops accepting new visits only when slice 2 moves the UI over (stated, not silent).
- **E5 One transition function.** A fixed table: `arrived → in_consultation | left_without_being_seen | cancelled`,
  `in_consultation → closed`. Anything else is 409. Optimistic `version`; stale is 409.
  `cancelled` and `left_without_being_seen` need a reason. Each change writes
  `medicore_encounter_transitions` (from, to, actor subject, reason, at), append-only,
  audited. Closed encounters are immutable (guard trigger).
- **E6 Events.** Opening an encounter publishes a new
  `helivanta.in.medicore.encounter_opened.v1` (`encounter_id, hospital_id, patient_id`). Consult mode and visit type are left out: they are clinical context and no consumer needs them on the wire (changed during the build, data minimisation). It does **not** publish `visit_created.v1`: that
  payload requires a patient name, and an encounter has no client-typed name to send, while
  an empty name is the #827 failure (a blank name written into a clinical record). So in
  slice 1 an encounter opens no pending dispense or lab order; pharmacy and lab move to
  `encounter_opened` in slice 2, and the OPD page says so. Legacy visits keep publishing
  `visit_created.v1` unchanged. Transitions publish
  `helivanta.in.medicore.encounter_status_changed.v1`. All through the outbox in the
  business transaction.
- **E7 Permissions.** Open: `medicore.visit.create` (doctor, as today). Read:
  `medicore.visit.read` (doctor, nurse). Transition: `medicore.visit.update`, granted to
  doctor and nurse (today nurse only, and unused). Grants: `medicore.access.manage`, no
  default role beyond tenant admin. Every encounter route also needs the hospital grant (E3).
  Clerk and triage roles arrive with #80's later slices.

## Slices

1. **This spec.** Projection, hospital grants, encounter columns and lifecycle, routes,
   events, an OPD page listing a hospital's encounters with open and status actions.
   Backend + migration + UI + tests in one PR.
2. Pharmacy and lab consume `encounter_opened`; the UI stops creating legacy visits;
   `visit_created.v1` retires once nothing reads it.
3. The rest of #80: tokens and queues per doctor and session, triage and vitals as
   observations, payer context, appointment intake, FHIR `Encounter`.

## Not covered

IPD (#81); practitioner assignment (needs #1374); vitals and weight (#1373, slice 3);
appointments; billing; FHIR projection.

## Verification

- Opening for a patient registered at another hospital, in another tenant, or unknown → 404.
- Without a hospital grant → 404 on every route, including a known encounter id.
- Illegal and stale transitions → 409; a closed encounter cannot change (trigger, tested).
- Replayed open with the same intent → 200 and one row, one event; different intent → 409.
- Legacy rows are not visible on hospital routes and cannot be transitioned.
- The projection consumer is idempotent under redelivery.
