# OPD encounter and patient safety browser journey — #1389

## Scope and owners

Add a real Playwright journey for the existing OPD encounter and safety strip.
Workforce owns hospitals and directory grants, IAM owns role assignments,
patientmaster owns patient registrations, MediCore owns encounters/allergy/weight
facts and consumes patient registrations and published drug molecules. Drugmaster
owns the synthetic reference catalogue and publishes it through its release CLI.
All browser/API calls enter the shell's same-origin Global API path.

Use per-spec development identities. The administrator grants the other per-spec
identity the existing doctor role through IAM, plus explicit workforce,
patient-registry and MediCore grants. A second directory-visible hospital has no
MediCore grant. No role or hospital directory entry substitutes for clinical
scope. All names and records are conspicuously synthetic and newly allocated.
This exercises IN-ID scope/registration and IN-CL-08 unknown-versus-negative
allergy semantics, not clinical terminology approval or medical treatment.

## Journeys

1. Register a patient through patientmaster's API, and wait until MediCore's
   clinical read confirms the registration projection. Open the encounter in the
   UI, assert its patient/hospital and arrived state, start consultation and
   assert the persisted in-consultation state after reload.
2. Open Patient safety. Assert unknown allergies first; explicitly attest no
   known allergy and verify the recorder; record a coded allergy selected from
   the seeded drug master; verify its recorded state and molecule reference;
   record a synthetic weight and verify its value, recorder and today's display.
3. Select the ungranted hospital and assert the no-access panel and backend 404.
   The scoped encounter list and opening control must be absent there.

Each test is independent. Read the actual UI and owner responses, with bounded
polling for genuine asynchronous projection/permission completion. No browser
route interception, static response substitute, arbitrary sleeps or cross-module
SQL seeding. A broken consumer or missing fixture is a test failure.

## Fixture and activation

A dedicated development-only seed target uses the existing drugmaster stage,
separate-approver publish and molecule replay commands. The input is synthetic,
not an imported medical catalogue. Repeated seeding publishes the same facts
under a fresh fixture release identity, preserving owner history. Database
connections are restricted to the local development database and its existing
least-privilege stager/approver accounts.

CI explicitly seeds this fixture after normal migrations/seeding, and enables
`MEDICORE_ALLERGY_RECORDING_ENABLED` only on its synthetic e2e API process.
The default dev/prod flag and clinical lists do not change. This is not approval
to enable allergy recording in production.

## Alternatives and limits

- Mocking API responses would miss registration/catalogue consumer failures.
- Writing projections or another module's tables directly would bypass the very
  ownership boundary the journey must prove.
- Using the administrator for clinical actions would not demonstrate the narrower
  clinician grants; use the separately assigned doctor identity instead.
- Enabling recording globally would activate an unreviewed production policy;
  use an explicit e2e-only process environment.

No pharmacy sales, B-2c patient linkage, #1371, #1388, production records,
prescribing, treatment advice or new application contracts are in scope.
