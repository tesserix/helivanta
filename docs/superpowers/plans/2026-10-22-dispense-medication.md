# Dispense Medication Implementation Plan

**Goal:** Every dispense records the medication the pharmacist typed for that row. Nothing is defaulted, and blank is refused at the API.

**Spec:** `docs/superpowers/specs/2026-10-22-dispense-medication-design.md` (#1272)

## Task 1: API refuses blank medication (D3)

- Test `backend/internal/modules/pharmacy/module_test.go`:
  - `{"medication":"   "}` → 400, and the row is still `pending`;
  - `{"medication":"  Amoxicillin 250mg "}` → 200, and the list shows `"Amoxicillin 250mg"`.
- `dispenses.go` `fulfil`: `strings.TrimSpace(req.Medication)`, then `respond.BadRequest` when empty, before `WithTenant`.
- `clinical.openapi.json` `DispenseInput.medication`: add `"pattern": "\\S"`. Run `go run ./cmd/contractdocs -check`.

## Task 2: Per-row form (D1, D2)

- `dispense-list.tsx`: drop the header input. A `DispenseRow` form inside `Can permission="pharmacy.dispense.fulfil"`, using `z.object({ medication: z.string().trim().min(1, "Medication is required").max(200) })` with the field id `dispense-medication-<id>`. The mutation takes `{ id, medication }`.
- Tests in `dispense-list.test.tsx`:
  - the POST body is the medication typed in that row;
  - a blank submit shows "Medication is required" and posts nothing;
  - a value typed in row 1 is not in row 2.
- e2e `smoke.spec.ts` and `journey.spec.ts`: fill the row's Medication before clicking Dispense, and assert the dispensed row shows it.

## Verify

`pnpm turbo lint type-check test build format:check`, the pharmacy Go tests, `make lint-go`, and e2e smoke + journey on a production build.
