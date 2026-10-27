# Pharmacy counter sale

Resolves #1386, with the parts of #172 (billing), #162 and #167 (stock and expiry) that a
sale needs. Owner: PharmaConnect (`pharmacy` module). Later slices build on it:

- #163 and #202: returns and credit notes;
- #1062: the H1 register;
- #1060: sales reports;
- #1387: billing an admission by bed;
- #1388: service charges;
- #170: dispensing against e-prescriptions.

## Why

The commonest pharmacy transaction is a person at the counter with a paper prescription, or
none for an OTC item. PharmaConnect cannot record it. The cashier has no invoice, stock never
goes down, and nothing records the prescription a Schedule H drug was sold against.

## What exists (verified in source, 2026-10-27)

- **Stock.** Batches (lot and expiry) sit in an append-only movement ledger, under a
  per-bucket advisory lock that refuses overdraft (`inventory.sql`). **Every location is
  `quarantine` and the migration allows nothing else.** Kinds are only `receipt | correction`,
  and the design says quarantine stock "must not be interpreted as sellable". Nothing is
  sellable today.
- **Catalogue.** Products are per outlet, with brand, generic, form, ingredients, pack and
  unit of issue. **MRP is per product** (`mrp_minor`), not per batch. `regulatory_status` is
  always `pending_review`. There is no schedule class and no link to the drug master.
- **Tax.** Effective-dated outlet registrations (GSTIN with its state) and per-product
  classifications (HSN, nature, rate in ppm, exemption basis) exist (`tax.sql`).
- **Outlets.** `regulatory_status` is always `pending_review`. There is no drug-licence
  record.
- **Drug master (#476).** It holds schedule class, NDPS and recall status per product, but is
  empty until a licensed dataset is approved.

## Decomposition

A correct sale needs stock that is allowed to be sold, at a known price, from an outlet
allowed to sell it. So this ships in three slices; each is correct on its own.

### Slice A: sellable stock

- **A1 Outlet drug licence.** One record per outlet:
  - licence kind (retail or wholesale, by form), number, issuing authority, validity dates,
    the registered pharmacist in charge, and an evidence file (the existing private-file
    flow);
  - reviewed by a second person, who cannot be the person who entered it.

  An outlet sells only while it holds a reviewed licence valid today. An expired or missing
  licence refuses every sale (fail closed).
  The record follows SupplyConnect's existing licence lifecycle (`supply_licenses` with
  private evidence, independent review and renewal notices, #1135): same states, same
  evidence flow and same reviewer rule. It is not a second design.

- **A2 Product coding.** Linking a catalogue product to a drug master product fixes its
  schedule class, NDPS flag and recall status. The link is optional, but required to sell
  any drug other than OTC (see decision 2).
- **A3 Batch price.** Each receipt records the batch's printed MRP (exact paise) and its
  purchase cost. The product-level `mrp_minor` becomes a default for new receipts only. A
  batch's MRP never changes; a mislabelled batch is corrected by a correction movement and
  a new batch.
- **A4 Release from quarantine.** A pharmacist moves units from quarantine to a `sellable`
  location with a recorded reason, and the release is checked:
  - the product is active and tax-classified;
  - the batch is unexpired;
  - the batch has an MRP;
  - the drug master shows no recall.

  This is the only way stock becomes sellable. New location kind `sellable`; new movement
  kinds `release`, `sale` and `sale_reversal`.

### Slice B: the sale

- **B1 One command.** `PUT /pharmacy/outlets/:outlet/sales/:uuidv7` with:
  - `{customer, prescription?, lines: [{product_id, quantity_units}], payments: [...]}`.

  In one transaction it allocates, prices, taxes, posts the invoice and payment, decrements
  stock and enqueues events. The same intent replays; a different intent is 409.

- **B2 FEFO allocation.** Lines are filled from sellable batches, earliest expiry first. A
  batch is skipped if it expires on or before the outlet's local date, is recalled, or
  belongs to a product no longer active. Several batches may fill one line, and each shows
  as its own invoice line, because MRP and expiry differ by batch. Not enough stock is a
  409 listing what is short; nothing is partly sold. The existing bucket lock makes two
  tills unable to sell the same last unit.
- **B3 Price.**
  - The default price per unit is batch MRP ÷ pack size, rounded down to the paisa. The
    line total is computed on the whole quantity, then rounded down, so it never exceeds
    MRP. Per-unit rounding cannot raise it.
  - A discount (percentage or amount) needs the `pharmacy.sale.discount` permission and a
    reason.
  - Nothing can price above MRP: the line is refused, not capped silently (#172).
  - NPPA ceiling prices (#1371) are not checked in this slice, and the screen says so.
- **B4 Tax.**
  - GST is computed per line from the product's classification in force on the sale date.
    GST is MRP-inclusive: taxable value = price ÷ (1 + rate).
  - Place of supply for a counter sale is the outlet's state, so CGST and SGST are each half
    the rate.
  - Rounding is per line to the paisa. The invoice total equals the sum of its lines, and
    any round-off line is shown explicitly.
  - An unclassified product cannot be sold.
- **B5 Prescription evidence, by the drug master class.**
  - `none` (OTC): none required.
  - `G` or `H`: prescriber name, registration number and council, the prescription date,
    and a prescription image (private file, scanned).
  - `H1`: the same, plus patient name and address, captured for the register (#1062).
  - `X` and NDPS: refused until #165 and #171 exist.
  - A product with no drug master link sells only as OTC (decision 2).
  - Evidence missing means the sale is refused, never a warning.
- **B6 Customer.** A registered patient is referenced by patientmaster id under the
  outlet's affiliation. Otherwise a walk-in is recorded: an optional name and phone, both
  required for H1. They are pharmacy-owned data, not a second patient master.
- **B7 Invoice.**
  - The number comes from a gap-free series per outlet and Indian financial year (prefix
    plus sequence, at most 16 characters, as GST rules require), assigned in the sale
    transaction.
  - The invoice carries the seller's GSTIN and address, and per line: HSN, batch, expiry,
    MRP, quantity, price, discount, taxable value, CGST and SGST.
  - An invoice is immutable. Corrections are credit notes (slice C).
  - B2B invoices with a buyer GSTIN, and e-invoicing (IRN, #539), are out of this slice.
- **B8 Payment.**
  - Recorded at sale time as tenders: `cash` (amount tendered and change) and `upi` or
    `card` (reference number). The tenders must equal the invoice total.
  - No credit sale in this slice.
  - A gateway (#210, #211) later confirms tenders instead of the cashier typing references.
- **B9 Permissions.**

  | Permission               | Holders                            |
  | ------------------------ | ---------------------------------- |
  | `pharmacy.sale.create`   | pharmacist                         |
  | `pharmacy.sale.discount` | pharmacy manager (tenant-assigned) |
  | `pharmacy.sale.read`     | pharmacist and accounts            |

  All of these also require the outlet grant, `editor` for selling. 404 across tenants and
  outlets.

- **B10 Events.** `sale_posted.v1` carries ids, totals and line product and batch ids. It
  carries no customer name or phone, and no prescription details.
- **B11 UI.** A counter screen with:
  - a product search, showing each product's stock and nearest expiry;
  - the basket, with the batch actually allocated and its MRP;
  - an evidence panel, shown only when a line's class needs it;
  - tenders and change;
  - a printable invoice.

### Slice C: after the sale

Returns as credit notes against the original invoice line and batch, with stock back to
quarantine for pharmacist re-release (#163, #202). Also:

- the end-of-day report: sales, tenders, credit notes and stock consumed (#1060);
- the H1 register report (#1062);
- the payment gateway (#210, #211).

## Not covered by A–C

Admission and bed billing (#1387), service charges (#1388), e-prescription dispensing (#170),
insurer splits, NPPA ceilings (#1371), credit sales and customer credit, B2B and e-invoicing.

## Decisions only the product owner can make

1. **Licence gate (A1).** Should an outlet without a reviewed, valid drug licence be unable
   to sell anything? I recommend yes. Selling without one is illegal, and a pilot outlet
   only needs its licence entered once.
2. **Selling before the drug master is loaded (A2, B5).** The drug master stays empty until
   a licensed dataset is approved, and without it no product has a schedule class. The
   options:
   - **(a) OTC only.** Sell only products the outlet marks OTC, with a pharmacist's
     attestation, until the drug master is loaded.
   - **(b) Outlet-declared class.** The pharmacist records the class on the product with
     its source, reviewed by a second person. The drug master overrides it once linked.

   I recommend (b), so pilots can run, with each declared class shown as "declared by the
   outlet" on invoices and registers.

3. **Discounts (B3).** Who may discount, and is there a ceiling, such as a percentage of MRP
   per line?
4. **Invoice series (B7).** One series per outlet per financial year, or a hospital-wide
   series?
