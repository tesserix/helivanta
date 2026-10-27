# Patient allergies and weight

Resolves #1373. Owner: MediCore. Consumer: prescribing checks (#83 prerequisite R4).

## Why

#83 cannot run its allergy or child-dose checks: nothing records allergies or weight. The
dangerous default is silence. A patient with no allergy record must read as **unknown**,
never as "no allergies" (IN-CL-08).

## What exists (verified in source, 2026-10-27)

- MediCore has encounters at a hospital, `medicore_patients` (one registered hospital per
  patient, from `patientmaster.registered.v1`) and `medicore_hospital_access` grants. Every
  route checks the grant in the same transaction (`inHospital`).
- The drug master publishes molecules globally, but MediCore cannot read its tables, and
  no event names individual molecules. `catalogue_published.v1` carries only counts.
- Drug classes (penicillins, NSAIDs…) do not exist yet (drug master slice 2).
- Patientmaster has a patient-reported blood group, kept immutable. That is the only
  clinical fact there.

## Decisions

- **A1 MediCore owns both.** The prescribing checks also live in MediCore, so they read
  these tables in-module. Routes go under `/medicore/hospitals/:hospital/patients/:patient/…`.
  The caller's hospital grant and the patient's registration at that hospital are checked
  in one transaction. Anything else is 404.
- **A2 Allergy status is explicit, with three states.**
  - `unknown`: nothing recorded. This is the default and is never stored.
  - `no_known_allergy`: a clinician asked and recorded that there are none, with who and
    when.
  - `allergies_recorded`: at least one active entry.

  Recording an allergy after "no known allergy" supersedes it. "No known allergy" is
  refused while any active entry exists. Every status read returns the state, who set it
  and when, so the screen can show "Unknown — not yet asked" prominently.

- **A3 What an entry names.** The substance is one of:
  - `drug_molecule`: a drug master molecule id, plus its INN name as shown when recorded.
  - `non_drug`: a category from a closed list (`food`, `latex`, `environmental`,
    `contrast_media`, `other`) plus a short free-text description.
  - `drug_unlisted`: free text, for a drug the master does not hold.

  Brand names are never the coded substance (#1373). **Fail-closed rule for #83:** a
  `drug_unlisted` entry makes the allergy checker `unavailable`, so signing is blocked
  until a clinician recodes it. Free text can be recorded but never clears a check.

  Drug classes wait for drug master slice 2.

- **A4 MediCore knows which molecules exist.** The drug master gains
  `helivanta.in.drugmaster.molecule_published.v1`: molecule id, INN name and ATC code, one
  per molecule first published in a release. MediCore projects it into
  `medicore_drug_molecules`. That table is global reference data, read-only to the app and
  on `lintAllowlist`, like the drug master.

  An allergy naming a molecule MediCore has not seen is refused (422). A client-supplied id
  is never trusted.

  Releases published before this event existed are re-announced by a drug master
  `replay` command, run by the approver login.

- **A5 Entry fields.**
  - **Reaction:** a closed list (`rash`, `urticaria`, `angioedema`, `anaphylaxis`,
    `bronchospasm`, `gastrointestinal`, `other`) plus an optional note of up to 500
    characters.
  - **Severity:** `mild | moderate | severe | life_threatening | unknown`.
  - **Source:** `patient_reported | clinician_observed | prior_record`.
  - **Optional:** onset date (not in the future), and the encounter it was recorded in,
    which must be at the same hospital.
  - **Recorded:** who (the authenticated subject) and when (the server's time).

  **These lists are a draft pending clinical review.** Recording ships switched off until a
  named reviewer approves them (see Product owner decisions).

- **A6 Corrections keep history (IN-CL-12).** Entries and weights are insert-only.
  - An entry is retired by a new row. Allergy statuses are `active`, `resolved` (with a
    reason) and `entered_in_error` (with a reason). Weights have only `entered_in_error`.
  - Each row's version is checked with an expected version: a stale write is 409.
  - Guard triggers refuse UPDATE and DELETE, as the encounter tables do.
- **A7 Weight is an observation.**
  - **Values:** kilograms (0.2–500, three decimals). Input in kg or g is converted on the
    server.
  - **Timing and method:** measured-at (not in the future); method `measured | estimated
| reported`.
  - **Attribution:** the optional encounter, who recorded it and when.
  - **Reads:** "latest weight" returns the newest by measured-at that was not entered in
    error, with its age, so #83 can require a weight within its window. The length of that
    window is a clinical rule (#87), not decided here.
- **A8 Idempotent writes.** `PUT` uses a client UUIDv7 and an intent digest, as encounters
  do. The same intent replays; a different intent is 409.
- **A9 Sensitive reads are audited.** Each list or status read writes one
  `medicore_clinical_read_audit` row: actor, hospital, patient, what was read, time. Writes
  are audited by `audit_record_change`.
- **A10 Permissions.**

  | Permission                    | Holders       |
  | ----------------------------- | ------------- |
  | `medicore.allergy.read`       | doctor, nurse |
  | `medicore.allergy.record`     | doctor, nurse |
  | `medicore.observation.read`   | doctor, nurse |
  | `medicore.observation.record` | doctor, nurse |

  Pharmacist access comes with verification (#169), through the prescription.

- **A11 Events.** `allergy_changed.v1` and `weight_recorded.v1` carry ids only (patient,
  hospital, entry id, version). No substance and no value: consumers re-read through an
  authorised query.
- **A12 UI.** On the OPD encounter screen, a patient safety strip shows:
  - the allergy state, with "Unknown" visually distinct and announced to screen readers;
  - active entries;
  - "Record allergy" and "No known allergies" actions;
  - the latest weight with its age, and "Record weight".

  It uses `useApiQuery`/`useApiMutation`, `useZodForm`, and toasts.

## Slices

1. **This spec, as one PR:**
   - drug master `molecule_published.v1` and its replay command;
   - the MediCore molecule projection;
   - the allergy status, entries and corrections;
   - weight;
   - audit, events and UI;
   - tests and a browser test.
2. **Later:**
   - drug classes and cross-reactivity, after drug master slice 2;
   - the prescribing checkers themselves (#83).

## Not covered

- Height, BMI and other vitals.
- Allergy import from ABDM or other hospitals.
- Patient-entered allergies in MediConnect.
- Reaction and severity vocabularies beyond the draft in A5, which waits on clinical review.
- A patient registered at two hospitals. The existing projection keeps one, a deferred
  #80 minor that this inherits.

## Product owner decisions (2026-10-27)

1. **Free-text drug allergies are allowed** (A3), and each one blocks prescribing checks
   until recoded. Refusing them would risk a real allergy going unrecorded, which is the
   harm this feature exists to prevent.
2. **A clinician confirms the A5 lists before release.** The medical director, or the pilot
   hospital's drug and therapeutics committee, approves the reaction and severity lists.
   Until then, recording allergies is off behind a per-environment setting
   (`MEDICORE_ALLERGY_RECORDING_ENABLED`, default off), and the route answers 503 with the
   reason. Reading the status still works, so "Unknown" is still shown. When they approve,
   record the reviewer's name and the date here.
3. **No pharmacist access before #169.** Pharmacists will read allergies through the
   prescription they are verifying, not by browsing patients. If a pilot pharmacy depends
   on manual counter checks, that is a new decision.
