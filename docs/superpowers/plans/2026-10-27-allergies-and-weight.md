# Allergies and weight implementation plan

**Goal:**

- **Allergy status:** a patient's allergy status reads as unknown until a clinician records
  something, and only drug master molecules can be coded.
- **Weight:** weight is a dated, attributed observation.

**Spec:** `docs/superpowers/specs/2026-10-27-allergies-and-weight-design.md` (#1373).

**Architecture:**

- The drug master announces each newly published molecule. MediCore projects those into a
  global, read-only table.
- MediCore owns these hospital-scoped, insert-only tables:
  - allergy statements (status rows);
  - allergy entries (each change a new row with a version);
  - weight observations;
  - a sensitive-read audit.
- Recording an allergy sits behind `MEDICORE_ALLERGY_RECORDING_ENABLED` (default off).

## Global constraints

- **Migrations:** `0003_drugmaster`, `0006_medicore`. UUIDv7 ids, forced RLS, `audit_record_change`.
- **Routes:** under `/medicore/hospitals/:hospital/patients/:patient/`. Each needs the
  hospital grant and the patient registered at that hospital, else 404.
- **Permissions:** `medicore.allergy.read|record` and `medicore.observation.read|record`,
  for doctor and nurse.
- **Events:**
  - `helivanta.in.drugmaster.molecule_published.v1`: `molecule_id`, `inn_name`, `atc_code`,
    `release_id`.
  - `helivanta.in.medicore.allergy_changed.v1` and `weight_recorded.v1`: ids and version
    only.

## Review focus

1. A patient with nothing recorded reads `unknown`, never `no_known_allergy`.
   - **Test:** a GET on a fresh patient.
2. "No known allergy" is refused while an active entry exists. Recording an entry
   supersedes "no known allergy".
   - **Test:** both orders.
3. A `drug_molecule` entry naming a molecule MediCore has not projected is 422. A
   `drug_unlisted` entry is accepted and flagged `needs_coding`.
   - **Test:** unknown id; free text.
4. A correction keeps history, and a stale `expected_version` is 409.
   - **Test:** resolve with the wrong version, then the right one; the history lists both.
5. Recording while the switch is off is 503. Reading still works.
   - **Test:** default harness and enabled harness.

## Tasks

1. **Drug master `molecule_published.v1`.**
   - `Decide` enqueues one event per molecule row the release created.
   - `drugmaster-release replay-molecules -by NAME` re-announces every published molecule.
     It runs as the approver and touches the outbox only.
   - The approver's outbox policy also admits the new subject.
   - **Tests:** publication emits one event per new molecule; a re-used molecule emits none;
     replay emits all.
2. **MediCore molecule projection.**
   - `medicore_drug_molecules` (global, `lintAllowlist`, written only by the consumer
     `medicore-molecule-published` under `WithSystem`).
   - It is a trusted platform event, so the consumer writes as a dedicated function:
     `SECURITY DEFINER`, owned by the schema owner, `search_path` pinned, insert-only,
     idempotent. `hms_app` holds SELECT only.
   - **Test:** redelivery is a no-op, and `hms_app` cannot insert directly.
3. **Allergies.**
   - **Tables:**
     - `medicore_allergy_statements` (`no_known_allergy` assertions with
       `retracted_by_entry`);
     - `medicore_allergy_entries` (identity: `id`, `version`, insert-only rows; current =
       highest version).
   - **Routes:**
     - `GET …/allergies`: status, active entries, `needs_coding`; audited.
     - `PUT …/allergies/:id`: record, idempotent.
     - `POST …/allergies/:id/corrections`: `{expected_version, status: resolved|entered_in_error, reason}`.
     - `PUT …/allergy-status/no-known/:id`: record "no known allergy".
     - `GET …/allergies/:id/history`.
   - **Tests:** review focus 1–5, plus cross-hospital 404, cross-tenant 404, and the audit
     row written.
4. **Weight.**
   - **Table:** `medicore_weight_observations`.
   - **Routes:**
     - `PUT …/weights/:id` (`{value, unit: kg|g, measured_at, method, encounter_id?}`);
     - `POST …/weights/:id/entered-in-error`;
     - `GET …/weights` (paged);
     - `GET …/weights/latest` (with `age_days`; 404 when none).
   - **Tests:** unit conversion, a future `measured_at` is 400, "latest" ignores
     entered-in-error rows, and the bounds 0.2–500 kg.
5. **UI.**
   - `PatientSafety` on each encounter row, expanded per row: allergy state ("Unknown — not
     yet asked" in a warning style with `role="status"`), active entries, "Record allergy"
     (when enabled), "No known allergies", latest weight and "Record weight".
   - Vitest for every state, and an e2e test of record and display.
6. **Docs.** Regenerate `contractdocs`; update `docs/api/domains/patients-and-clinical.md`.
