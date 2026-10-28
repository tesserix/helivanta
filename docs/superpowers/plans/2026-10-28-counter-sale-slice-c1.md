# Counter sale slice C-1 implementation plan: returns as GST credit notes

**Goal:** a pharmacist takes back units of a posted counter sale. The return issues a
gap-free GST credit note against the original invoice, puts the units into quarantine and
records the refund.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386; #163, #202):
"Slice C pull requests" and "C-1 decisions" 1–10.

**Architecture:** migration `0019_pharmacy` (`credit_notes.sql`), outlet-scoped with forced RLS,
audit, and insert-only tables apart from the series counter:

- `pharmacy_credit_note_series`, with the same step-by-one trigger as the invoice series;
- `pharmacy_credit_notes`, sealed by `posted_xact`;
- `pharmacy_credit_note_lines` and `pharmacy_credit_note_refunds`;
- the movement kind `return`. It is allowed only into quarantine, and only under a credit note
  line, as `return:<credit note id>:<n>`.

A deferred trigger checks that a credit note equals its lines, is refunded by its refunds, and
has one movement per line. A line guard locks the sale line and checks:

- the cumulative units are within the units sold;
- the amounts are the cumulative pro-rata step;
- the sale belongs to the outlet;
- the date is within the s.34(2) deadline.

## Global constraints

- **`PUT …/credit-notes/:uuidv7`** (`pharmacy.sale.return`, pharmacist, with the outlet
  `editor` grant) takes:

  ```
  {sale_id,
   lines: [{sale_line, quantity_units, quarantine_location_id}],
   refunds: [{kind, amount_minor, reference?}],
   reason,
   recipient: {name, address}}
  ```

  - It returns 201, or 200 for a replay; a different intent is 409.
  - Refusals are 409 `not_returnable` with a reason:
    - more units than are left to return on the line;
    - a location that is not quarantine;
    - past the s.34(2) deadline;
    - the series is full;
    - refunds that do not equal the total.

- **`GET …/credit-notes/:id`** and the paged **`GET …/credit-notes?sale_id=`**
  (`pharmacy.sale.read`).
- **`GET …/sales/:id`** gains `returned_units` per line.
- **Pro rata:** for a line with `q` units sold, `amount` A and tax half H, with `k` units
  returned before and `r` now:

  ```
  credit amount = floor(A(k+r)/q) − floor(Ak/q)
  credit half   = floor(H(k+r)/q) − floor(Hk/q)
  credit taxable = credit amount − 2 × credit half
  ```

- **Number:** `PREFIX/FY/Cn`, where FY is from the credit note's outlet-local date.
- **Deadline:** the credit note date must be on or before 30 November of the calendar year in
  which the sale's financial year ends, which is `20YY` for FY `XXYY`.
- **Recipient:** name 1–200 characters and address 1–500, trimmed. The state is the sale's
  place of supply.
- **Event:** `helivanta.in.pharmacy.credit_note_issued.v1` carries
  `{pharmacy_id, outlet_id, credit_note_id, sale_id, credit_note_number, currency, total_minor,
lines: [{product_id, batch_id, quantity_units}]}`.
- **Stock:** each line writes a `return` movement of `+units` into the chosen quarantine
  location for the line's batch.

## Review focus

1. Two returns of the last units of one sale line at once: exactly one succeeds.
2. Three partial returns of a line credit exactly the line's amount and tax in total.
3. A replay neither issues a second number nor moves stock twice.
4. A return dated 1 December after the deadline is refused; 30 November is accepted.
5. Returned stock is never sellable until released: it is not in `sales/availability`.

### Task 1: Migration and command

- [ ] **Failing tests** (`credit_note_test.go`):
  - `TestAReturnIssuesACreditNoteAndQuarantinesTheStock`;
  - `TestPartialReturnsCreditExactlyTheLine`;
  - `TestAReturnNeverExceedsWhatWasSold`;
  - `TestTwoReturnsCannotTakeTheSameLastUnit`;
  - `TestACreditNoteHasItsOwnGapFreeSeries`;
  - `TestTheReturnDeadlineIsThirtiethNovember`;
  - `TestReturnedStockIsSoldOnlyAfterRelease`;
  - `TestTheDatabaseKeepsCreditNotesHonest`;
  - the replay case.
- [ ] **Implement** the migration, `credit_notes.go`, the `PermSaleReturn` grant and the
      contract event.
- [ ] **Run and commit.**

### Task 2: Contracts and docs

- [ ] Update:
  - the archtest matrix, unpaginated allowlist and payload classification;
  - `contractdocs`;
  - `docs/api/domains/pharmacy.md`.
- [ ] **Commit.**

### Task 3: Screens

- [ ] **Counter:**
  - A "Returns" view finds a sale by invoice from the paged sales list.
  - It shows each line with units left to return.
  - It takes the units to return per line, a quarantine location, the recipient, the reason
    and the refund.
  - It prints the credit note, showing the original invoice number and date, each line's
    taxable value, rate and tax credited, and the total.
- [ ] **Tests:**
  - the return is sent;
  - more units than are left are refused;
  - the credit note prints its invoice reference.
- [ ] **Run the frontend gate and commit.**
