# Discharge orders Implementation Plan

**Goal:** A doctor orders and withdraws discharge for an admission (#96 slice 1).

**Spec:** docs/superpowers/specs/2026-10-30-discharge-orders-design.md

## Global Constraints

- Migration `0008_medicore`, append-only; forced RLS by `medicore_hospital_allowed`.
- Permission `medicore.discharge.order`, doctors.
- Events `helivanta.in.medicore.discharge_ordered.v1`, `…discharge_withdrawn.v1`; ids,
  disposition and times only.
- `409` codes: `already_ordered`, `not_admitted`, `conflict`.

## Review Focus

- An order racing a release: never an active order created on a released admission.
- Two doctors ordering at once: one order.
- A replay with a different disposition or note: refused.
- Withdrawal after release: refused.
- Reader without the hospital grant: 404, and the order is invisible on the bed board.

### Task 1: Schema, handlers, events

`medicore/discharge.sql`, `medicore/discharge.go`, `module.go` (permission, migration,
routes, publishes), `contract/events.go`, archtest PHI classification and permission matrix,
`beds.go` (board and admission views), `discharge_test.go`.

Tests: `TestADoctorOrdersAndWithdrawsDischarge`, `TestDischargeOrdersRefuseReleasedAdmissionsAndOtherWriters`,
`TestConcurrentDischargeOrdersLeaveOne`.

### Task 2: Frontend

`packages/api/src/medicore-beds.ts` types and schema; ward board badge and doctor actions;
Vitest.

### Task 3: Docs

`docs/api/domains/beds-and-admissions.md`, portfolio, regenerated reference.
