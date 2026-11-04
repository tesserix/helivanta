# A counter sale linked to the OPD encounter it serves (#987, slice 2)

Issue: #987 (slice 2 of 3; the issue stays open for the payer split). Builds
on registered-patient sales (#1386 B-2c), admission sales (#1387) and the
sales worklist (#987 slice 1, #1554). MediCore's encounter contract (#80) is
consumed as published; nothing in MediCore changes.

## Why

#987 asks for "counter, OPD, IPD and casualty pharmacy sales" filterable "by
store and linkage", "with clear patient and encounter context". An IPD sale is
already linked: it is billed to the admission in a bed. An OPD patient who
walks from the consulting room to the counter is today just a registered
patient; nothing says which visit the medicines were for. That link is what a
hospital needs to see pharmacy sales against its OPD visits, and what #1051
(sales into hospital bills) will build on.

## What MediCore publishes (verified in source, `medicore/encounters.go`)

- `encounter_opened.v1`: `{encounter_id, hospital_id, patient_id}`, when an
  OPD encounter opens with status `arrived`. References only.
- `encounter_status_changed.v1`: `{encounter_id, hospital_id, from_status,
to_status, version}`, for every transition, in the transaction that records
  it. Legal moves: `arrived → in_consultation | left_without_being_seen |
cancelled`, `in_consultation → closed`. `closed`,
  `left_without_being_seen` and `cancelled` are terminal.
- Encounters are OPD only (`department='OPD'`). Casualty is the emergency
  module's (`case_received`, `case_closed`), a separate contract.

## Decisions (agreed 2026-11-04)

1. **Pharmacy keeps its own projection** `pharmacy_opd_encounters`
   `(tenant, encounter_id, hospital_id, patient_id, opened_at, status, version)`, fed by
   two consumers, exactly as `pharmacy_bed_occupancies` is fed for
   admissions. Either event may arrive first: an `opened` fills the patient and
   `opened_at`, the time MediCore published it (the envelope's `occurred_at`),
   which orders and tells apart a patient's visits however late they arrive; a `status_changed` sets the status only if its `version` is
   higher than the one held. Replays and stale events change nothing; a
   conflicting replay (different patient or hospital for one id) is refused
   and retried, never overwritten. No patient name is held.
2. **What a sale may cite.** `encounter: {encounter_id}` on a
   registered-patient sale only (`patient` required; never with `admission`
   or a walk-in buyer). The database refuses the sale unless the projected
   encounter:
   - has been opened (its patient is known);
   - is for the sale's patient at the sale's patient hospital, which has
     verified the pharmacy (the B-2c rule already enforced on the sale);
   - is `arrived`, `in_consultation` or `closed`. `cancelled` and
     `left_without_being_seen` are refused: the visit did not happen.
     A closed encounter is allowed because patients collect medicines after the
     consultation ends. There is no time limit after closing (agreed
     2026-11-04: there is no source for one); the worklist shows the link for
     anyone to question, and a limit can be added as a reviewed outlet setting.
3. **Ordering.** The sale takes the projection row `FOR SHARE`, so a status
   change being applied is wholly before or after the sale; the check runs in
   the trigger, so no code path can post a sale against a refused encounter.
   A later cancellation does not undo a posted sale (insert-only), and the
   worklist shows the encounter's current status beside the link.
4. **What the counter sees.** After the pharmacist confirms the registered
   patient (the existing audited MRN lookup), the counter lists that
   patient's citable encounters at that hospital, newest first, from
   `GET …/outlets/:outlet/opd-encounters?patient_id=&hospital_id=` (needs
   `pharmacy.sale.create`; audited, as it is a read about one patient). The
   pharmacist picks one or none: citing is optional, because OTC purchases
   by a registered patient need not be for a visit.
5. **The worklist and summary.** A row gains `linkage`: `admission`, `opd` or
   `none`, and the worklist a `linkage` filter. They show no encounter id
   (no identifiers in those reads, #987 decision 3); the invoice read gains
   `encounter_id` and is already audited (#1555).
6. **Events.** `sale_posted.v1` is unchanged: it carries no patient, and an
   encounter id with a sale is a pointer to a patient's visit. Publishing the
   link for hospital billing belongs to #1051's contract.
7. **Migration.** A new pharmacy migration: the projection table with forced
   RLS, `pharmacy_sales.encounter_id`, and the sale guard. Main holds `0029`
   and `0031`; `0030` is reserved for Codex's #1528 and #1477 must renumber,
   so the ID is taken at merge time against open PRs, not now.

## Not covered

- Casualty sales (the emergency module's contract; a slice of its own).
- The payer split between patient, insurer and hospital (slice 3, with #1051).
- Publishing the sale-to-encounter link to hospital billing (#1051).
- Linking a sale made before this change, or changing a sale's link (sales
  are insert-only; a wrong link is corrected by a credit note and a new sale,
  as a wrong bed is).
- Encounters at a hospital that has not verified the pharmacy (refused, as
  their patients are).
