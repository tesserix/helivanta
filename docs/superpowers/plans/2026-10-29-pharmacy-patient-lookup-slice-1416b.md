# Pharmacy patient lookup (#1416 slice b) Implementation Plan

**Goal:** A pharmacist the hospital has named can look up one of that hospital's patients by
exact MRN, only while the pharmacy's link with the hospital is active on both sides, and
pharmacy keeps the ids-only list B-2c will check sales against.

**Spec:** `docs/superpowers/specs/2026-10-29-pharmacy-affiliation-verification-design.md`
(Patient lookup)

## Global constraints

- Permission `patient.pharmacy-lookup`, role pharmacist. Grant management reuses
  `patient.pharmacy-affiliation.review` and a registrar grant.
- Migrations `0025_pharmacy` and `0014_patientmaster` (renumbered from `0024` after #1445 took it).
- The consumer is named `pharmacy-patient-registered`.
- Lookup answer: `{patient_id, name, age_years, age_estimated, sex}`. One `404` covers every
  refusal.

## Task 1: Pharmacy registrations list

- `pharmacy/registered_patients.{sql,go}` holds the table and its consumer.
- Test `TestPharmacyKeepsRegisteredPatientIds`: a duplicate or a different hospital, a bad
  event, and RLS.

## Task 2: Patientmaster links

- `patient_pharmacy_links`, with per-command policies.
- The receipt consumer inserts the link on a confirmed acceptance. A revoke or withdrawal
  deletes it.
- Test `TestPharmacyLinkActiveOnlyOnceBothSidesAgree`.

## Task 3: Lookup grants and lookup

- `patient_pharmacy_lookup_access`, the `patients` RLS branch, an audit column, and the action.
- Routes for grant, revoke, list and lookup.
- Test `TestPharmacyPatientLookup`: the grant, the link, a miss, the audit, no registry
  access, the same 404 in every refusal case, and a stale grant after re-verification.

## Task 4: Frontend

- The hospital panel manages lookup access on an accepted pharmacy, picking a member from
  `/iam/members`.
- Vitest covers it.

## Task 5: Docs and gates

## Review focus

- A lookup grant holder reading `/patients` or search: must be refused.
- A link removed while a lookup is in flight: the `FOR SHARE` locks serialise them.
- A grant from an old round after the link is re-verified.
- An MRN with a different case or extra spaces.
- An audit write failure: the lookup must fail, not answer unaudited.
