# Registration replay

Resolves #1377. Owner: patientmaster.

## Why

MediCore's encounter projection (#80, spec E2) learns which hospital a patient is registered
at from `helivanta.in.patientmaster.registered.v1`. The stream keeps about a day of events,
so a consumer added later never sees older registrations, and those patients cannot open an
encounter. That fails closed. MediCore must not read patientmaster's tables, so the owner
re-announces them.

## Decisions

- **R1 One route, one page per call.** `POST /patients/hospitals/:hospital/registration-replays`
  re-publishes `registered.v1` for one keyset page of that hospital's patients. The page
  cursor makes it resumable, and a repeated page is harmless
  because consumers ignore duplicates. The response lists the patient ids announced and the
  next cursor. The page's probe row is announced on the next page, not twice.
- **R2 Who.** New permission `patient.registration.replay`, tenant admin only, **and** a
  patient-registry grant at that hospital, checked in the same transaction as for every
  patient read. No grant, or another tenant, is the same 404 / empty page.
- **R3 Attributable.** Each page is one transaction: the events go through the outbox and one
  `patient_access_audit` row (action `replay`, the ids announced) records who did it.
- **R4 Same payload.** The replayed event is the ordinary v1 payload (patient and hospital ids,
  no demographics). Consumers cannot tell a replay from a registration, and do not need to.

## Not covered

Replaying other patientmaster events; a UI (an operator calls the route); scheduling.

## Verification

- `TestRegistrationReplayAnnouncesEachPatientOncePerPage`: three patients over two pages of
  two; each announced exactly once more; one audit row per page; no grant 404; another tenant
  sees an empty page; a registrar without the permission 403.
- `TestReplayedRegistrationMakesAnExistingPatientOpenable` (journey): a patient MediCore does
  not know cannot open an encounter (404); after one replay call they can (201).
