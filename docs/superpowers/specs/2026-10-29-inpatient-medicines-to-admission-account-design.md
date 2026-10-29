# Inpatient medicines reach the admission account

Resolves #1387, and the first slices of #193 (hospital billing ledger) and #197 (admission
account). Builds on #106 slice 1 (beds and occupancy), #1416 (verified affiliation and patient
lookup) and #1386 (counter sale).

## Why

A ward requests medicine for an inpatient by bed. The hospital must bill it to that patient's
admission, not collect at the counter, or the patient pays twice. A bed is not an account:
patients transfer, and beds are reused.

## Decisions (accepted 2026-10-29)

1. **A new `billing` module owns the hospital ledger.** Every billing story feeds it through
   events. Pharmacy and MediCore never write its tables.
2. **The pharmacy GST invoice stays the tax document.** The admission account records what the
   patient owes the hospital against that invoice. It is not a second sale, so revenue is counted
   once (#1051).
3. **The pharmacist confirms the patient with the audited lookup by patient id** (option A). It
   is the same named-pharmacist grant and verified link as the MRN lookup (#1416b), with the same
   minimal answer and audit.
4. **One PR** for the vertical slice.

## Flow

1. MediCore's `bed_occupancy_started.v1` gains `ward_code`, `room_code` and `bed_code`
   (additive; codes are not patient data). Pharmacy keeps `pharmacy_bed_occupancies` from the
   started and ended events, ids and codes only. Either event may arrive first.
2. **Resolve:** `GET …/outlets/:outlet/admission-occupant?hospital_id=&bed_code=` or
   `&room_code=` (`pharmacy.sale.create`).
   - It refuses with `404` unless the outlet's pharmacy holds a verified affiliation with that
     hospital.
   - It answers the one open stay: `{occupancy_id, admission_id, patient_id, hospital_id,
ward_code, room_code, bed_code}`. Every resolve is audited, a miss included.
   - **Ruling:** a pharmacist without the named lookup grant can learn which opaque patient id
     occupies a bed at a verified hospital, but not who that patient is. The name, age and sex
     need the lookup grant (step 3), and nothing posts without them.
   - An empty bed or room is `404 no_occupant`. A room with several occupied beds is
     `409 ambiguous`, naming no patient.
3. **Confirm:** the counter calls `GET /patients/pharmacy-lookup?hospital_id=&pharmacy_id=&patient_id=`
   and shows name, age and sex. Patientmaster's row security admits that one patient for a
   granted pharmacist in the link's current round, as it does for an MRN.
4. **Post:** the sale carries `admission: {occupancy_id}`. In the sale transaction, pharmacy
   re-resolves the stay: it must still be open in its projection, at a verified hospital.
   - The sale stores `admission_id`, `admission_occupancy_id`, `admission_bed_code` and
     `admission_resolved_at` (the transaction's time). It also stores the occupant as
     `patient_id` and `patient_hospital_id`, so the B-2c foreign key and the verified-affiliation
     trigger apply as they do to any registered patient.
   - The sale is paid by one tender of kind `admission_account` for the total; nothing is
     collected. `admission` excludes `buyer` and `patient` (`400`).
   - A trigger refuses an `admission_account` tender on a sale with no admission, or an admitted
     sale with any other tender. Another trigger refuses a sale whose stay is not open for that
     admission, patient and hospital.
5. **Charge:** the sale publishes `helivanta.in.pharmacy.admission_charge_posted.v1`
   `{sale_id, invoice_number, admission_id, hospital_id, patient_id, outlet_id, amount_minor,
currency, posted_at}`.
6. **Reverse:** a credit note on an admitted sale refunds only through `admission_account`, and
   no other sale may use that refund kind. It publishes `admission_charge_reversed.v1`
   `{credit_note_id, credit_note_number, sale_id, admission_id, hospital_id, patient_id,
amount_minor, currency, reversed_at}`. A wrong-bed sale is corrected by a full credit note and
   a new sale to the right bed. History is kept.
7. **Billing.**
   - `billing_admission_accounts` (one per admission) opens on the first
     `bed_occupancy_started.v1` and closes on `admission_released.v1`. Any event may arrive first:
     an account is created by whichever names it, and closed only by the release.
   - `billing_account_entries` are append-only: `charge` or `reversal`, keyed on (source module,
     source reference, kind), so a replay never posts twice.
   - Reversals of one charge are taken one at a time and never exceed it. A reversal that arrives
     before its charge is parked and applied with the charge.
   - A charge on a closed account is still recorded, because late postings happen; it shows as
     posted after release.
   - `GET /billing/hospitals/:hospital/admissions/:admission/account` returns status, totals by
     currency, the outstanding amount (charges less reversals) and the entry count.
   - `GET …/entries` is a page of entries.
   - Access needs `billing.account.read` (nurse, receptionist) plus a billing hospital grant,
     managed with `billing.access.manage` (administrators). Without a grant the answer is `404`.

## UI

- **Counter customer:** "Bill to admission". Pick a verified hospital, enter a bed or room code,
  find the stay, confirm the patient by name, age and sex. The payment section then shows
  "Billed to the admission account", and the invoice says so with the bed.
- **Ward board:** for a holder of `billing.account.read`, an occupied bed shows its account's
  outstanding pharmacy amount. A tenant administrator can grant their own billing access at the
  hospital.

## Not covered

- A paid verified-UPI payment left unused when the cashier switches the customer type (this also
  happens today when switching tender kind). It stays on the unused-payments list for a later
  sale.

- Bed-day charges, tariffs, hospital invoices, and interim and final bills (#197, #193).
- Deposits and payment collection against the account (#199); discounts (#200).
- Ward requests as their own record (the counter sale is the request here), and pharmacy
  clearance as a recorded discharge gate (#1043).
- Cross-tenant affiliations.

## Tests that must exist

- Resolve: an empty bed, an ambiguous room, an unverified hospital, an ended stay, and
  started/ended arriving in either order.
- A stay ending between resolve and post refuses the sale. An admitted sale is never paid in cash.
- A credit note reverses only through the account. Charge and reversal events are published once.
- Billing: a replay is idempotent; events in any order converge; a closed account accepts a late
  charge; there is no access without a grant; another tenant sees nothing.
- The lookup by patient id has the same grant, link, 404 shape and audit as by MRN.
