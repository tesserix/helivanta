# Affiliation verification (#1416 slice a) Implementation Plan

**Goal:** A pharmacy asks a same-tenant hospital to verify its declared affiliation;
the hospital's registrar-administrator accepts, declines or revokes; the pharmacy
can withdraw; both sides keep append-only, attributed history.

**Spec:** `docs/superpowers/specs/2026-10-29-pharmacy-affiliation-verification-design.md`

## Global constraints

- Subjects: `helivanta.in.pharmacy.affiliation_verification_requested.v1`,
  `helivanta.in.pharmacy.affiliation_verification_withdrawn.v1`,
  `helivanta.in.patientmaster.pharmacy_affiliation_decided.v1`.
- Consumers: `patient-pharmacy-affiliation-request`,
  `patient-pharmacy-affiliation-withdrawal`, `pharmacy-affiliation-decision`.
- Permission `patient.pharmacy-affiliation.review`, no role.
- Migrations `0023_pharmacy`, `0013_patientmaster`; append-only.
- Reason 1–500 characters after trim; ids UUIDv7.

## Task 1: Contracts

- `pharmacy/contract/events.go`: subjects + `AffiliationVerificationData`.
- `patientmaster/contract/events.go`: subject + `PharmacyAffiliationDecidedData`.
- Archtest PHI classifications for every field.

## Task 2: Pharmacy history and commands

- `pharmacy/affiliation_verification.sql`: `pharmacy_affiliation_verifications`
  (tenant, id v7, affiliation_id, sequence, kind, request_id, decision_id,
  actor_subject, reason, created_at); FK to the affiliation; unique
  (tenant, affiliation, sequence), unique (tenant, decision_id); a BEFORE INSERT
  trigger enforcing sequence = previous + 1, the transitions and same-round
  request ids; immutability trigger; audit; forced RLS.
- `pharmacy/affiliation_verification.go`: request PUT, withdraw POST, history
  ListRoute; derived status on list; decision consumer.
- Test `TestAffiliationVerificationLifecycle` (pharmacy side, with events injected).

## Task 3: Patientmaster review

- `patientmaster/pharmacy_affiliations.sql`: `patient_pharmacy_affiliation_reviews`
  (immutable claim + guarded status) and `patient_pharmacy_affiliation_decisions`
  (append-only, reviewer ≠ requester on acceptance), RLS via
  `patient_hospital_allowed` or the delivery actors.
- `patientmaster/pharmacy_affiliations.go`: two consumers, list, decide.
- Test `TestPharmacyAffiliationReview`.

## Task 4: Frontend

- `packages/api`: types and statuses.
- Pharmacy registry: status label, request, withdraw (ConfirmDialog).
- Patients page: `PharmacyAffiliationReviews` panel for reviewers.
- Vitest tests for both.

## Task 5: Docs and gates

- `pharmacy.md`, `patients-and-clinical.md`, ADR-0024 status line, contractdocs.
- Go lint, tests, archtest, contractdocs check, frontend turbo gates.

## Review focus

- A decision arriving for a closed round must not reopen it.
- Withdraw delivered to patientmaster before its request.
- Two requests racing for one affiliation.
- A reader (not registrar) grant holder with the review permission.
- Self-acceptance by the same subject who requested.
