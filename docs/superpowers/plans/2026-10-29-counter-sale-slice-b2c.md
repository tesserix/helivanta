# Registered patients at the counter (#1386 B-2c) Implementation Plan

**Goal:** A counter sale can name a registered patient found by the audited MRN lookup, and
posts only if that patient is a real registration at a hospital the outlet's pharmacy is
verified with.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (B-2c decisions), plus
`2026-10-29-pharmacy-affiliation-verification-design.md` (lookup).

## Global constraints

- Migration `0026_pharmacy`. Check it is still free before merging, because Codex also works on
  pharmacy.
- `patient` and `buyer` are mutually exclusive.
- Every refusal is `409 not_sellable`.
- No event change.

## Task 1: Backend

- `registered_patient_sale.sql`:
  - `pharmacy_sales.patient_id` and `patient_hospital_id`, both null or both set;
  - an exclusion check against the buyer columns;
  - a foreign key to `pharmacy_registered_patients (tenant_id, patient_id, hospital_id)`;
  - the trigger `pharmacy_sale_patient_affiliated`.
- `sale.go`:
  - add `salePatient` to the input, the digest and the insert;
  - add both ids to the sale read;
  - take the affiliation lock;
  - map refusals to `not_sellable`.
- Test `TestARegisteredPatientSaleNeedsAVerifiedLink`:
  - an unknown patient, an unverified, revoked or withdrawn link, and a patient of another
    hospital are all refused;
  - a sale with both patient and buyer is a 400;
  - replays are idempotent;
  - the database refuses a direct insert.

## Task 2: Frontend

- `counter-patient.tsx`: pick a verified hospital, enter the MRN, look up, confirm.
- `counter-sale.tsx`: a customer choice (walk-in or registered patient) and the payload.
  The invoice line reads "Registered patient".
- Vitest coverage for both.

## Task 3: Docs and gates

## Review focus

- A link revoked between the lookup and the post.
- A patient id from one hospital sent with another hospital's id.
- A replay of a sale after the link is revoked: it must return the existing sale and not post
  a new one.
- A lookup refusal shown as "not found" without hinting which check failed.
