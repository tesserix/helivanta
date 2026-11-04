# Pharmacy sale linked to its OPD encounter — implementation plan

**Goal:** #987 slice 2: a registered patient's counter sale may cite the OPD
encounter it serves; the database refuses any other.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-opd-encounter-link-design.md`

## Global constraints

- MediCore's `encounter_opened.v1` and `encounter_status_changed.v1` consumed
  as published; MediCore unchanged.
- New pharmacy migration (ID chosen at merge against open PRs); forced RLS on
  the new table; sales stay insert-only.
- The citable-encounters read is audited (#1555); worklist and summary carry
  no encounter id.

## Task 1: projection (`backend/internal/modules/pharmacy/opd_encounters.go`, `.sql`)

- `pharmacy_opd_encounters (tenant_id, encounter_id, hospital_id, patient_id
  NULL, status NULL, version int NULL)`; consumers `pharmacy-encounter-opened`
  and `pharmacy-encounter-status-changed`.
- Tests first: opened then status; status then opened; stale version ignored;
  replay idempotent; a conflicting replay (other patient or hospital) errors;
  bad event errors.

## Task 2: the sale cites it (`sale.go`, the `.sql` guard)

- `encounter: {encounter_id}` on the sale input; `pharmacy_sales.encounter_id`.
- Trigger `pharmacy_sale_encounter_citable`: requires `patient_id`, no
  `admission_id`; locks the projection row `FOR SHARE`; refuses unless opened,
  same patient and hospital, status in (`arrived`,`in_consultation`,`closed`).
  Refusal is 409 `not_sellable` with the reason.
- Intent digest covers the encounter.
- Tests first: each refusal; a closed encounter accepted; the invoice read
  carries `encounter_id`; a replay with another encounter is 409.

## Task 3: counter read, worklist, shell

- `GET …/registered-patients/{patient}/encounters?hospital_id=`
  (`pharmacy.sale.create`; audited; citable only; newest first; bounded).
- Worklist and summary: `linkage` (`admission`,`opd`,`none`) and a worklist
  `linkage` filter.
- Shell: the counter offers the patient's encounters after confirming the
  patient; the Sales page shows and filters by linkage. Tests with
  `renderWithProviders`.

## Task 4: docs

Domain doc section; the #987 worklist section updated; contract docs and
inventory regenerated.

## Review focus

- An encounter cancelled while a sale citing it posts.
- A status event older than the one held arriving last.
- An encounter of the right patient at another hospital.
