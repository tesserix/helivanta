# Dispense: the pharmacist states each row's medication

Resolves #1272. Covers the legacy tenant dispense queue (`/pharmacy`, `docs/api/domains/pharmacy.md` "Legacy dispense queue").

## Problem

The queue had one header "Medication" box, pre-filled with `Paracetamol 500mg`. Every row's Dispense button posted that one value, and `fulfil` stored it, so a pharmacist who did not retype it recorded paracetamol for every patient. The API accepted whitespace-only medication.

## Decisions

- **D1 No default.** No code path supplies a medication the pharmacist did not type.
- **D2 Per-row entry.** Each pending row the user may fulfil has its own form: a `Medication` field (`useZodForm` + `Field`, `noValidate`) and a Dispense button. A value typed in one row never reaches another. A blank value shows "Medication is required" inline, and nothing is posted.
- **D3 Server rejects blank.** `fulfil` trims the medication and answers 400 when it is empty, before any read or write, so no client can record a blank dispense (fail closed). `DispenseInput.medication` in `contracts/api/clinical.openapi.json` gains `pattern: "\\S"`. The stored value is the trimmed one.

## Not covered

- Picking from the formulary, or checking against a prescription. Visits carry no prescription (`docs/api/domains/patients-and-clinical.md`), so the queue stays free text.
- Page header and loading/error states belong to the PharmaConnect tidy-up (#1258).
