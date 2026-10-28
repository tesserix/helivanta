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

- **B1 One command.** `PUT /pharmacy/pharmacies/:pharmacy/outlets/:outlet/sales/:uuidv7` with:
  - `{lines: [{product_id, quantity_units}], tenders: [...]}`, plus `customer` and
    `prescription` from B-2.
  - `POST …/sales/quote` prices the same lines without posting, so the cashier knows what to
    collect.

  In one transaction it allocates, prices, taxes, posts the invoice and payment, decrements
  stock and enqueues events. The same intent replays; a different intent is 409.

- **B2 FEFO allocation.** Lines are filled from sellable batches, earliest expiry first. A
  batch is skipped if it expires on or before the outlet's local date, or is recalled (a safety incident).
  A product that is not active refuses the whole sale, with its reason. Several batches may fill one line, and each shows
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
  - Rounding is per line to the paisa. CGST and SGST on a line are equal: each is half the
    line's tax, rounded half up, and the taxable value is the line amount less both.
  - The invoice total is the exact sum of its lines, with no round-off. Rounding up to the
    rupee would charge above MRP; rounding down is a discount. Either is a product decision
    that has not been taken.
  - An unclassified product cannot be sold.
- **B5 Prescription evidence, by the class in force.**
  - `none` (OTC): none required.
  - `G` or `H`: prescriber name, registration number and council, the prescription date,
    and a prescription image (private file, scanned).
  - `H1`: the same, plus patient name and address, captured for the register (#1062).
  - `X` and NDPS: refused until #165 and #171 exist.
  - The class in force is the outlet's approved declaration (decision 2,
    `pharmacy_product_schedule_in_force`). A product without one cannot be sold; it is never
    treated as OTC.
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
  | `pharmacy.sale.read`     | pharmacist and owner               |

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

#### Found in B-1 review (2026-10-28)

- **Retail licence.** A counter sale needs a reviewed **retail** licence in force. A
  wholesale-only licence does not license sales to consumers
  (`pharmacy_outlet_retail_licence_in_force`).
- **UTGST, open for tax review.** A union territory without a legislature levies UTGST, not
  SGST. Until that tax head is modelled and reviewed, an outlet registered in a union
  territory (jurisdiction kind `union_territory` in the country profile) is refused at the
  counter, rather than invoiced under the wrong head.
- **The database checks each line against its source.** The batch must be the product's and
  sellable on the sale date. Its lot, expiry and MRP must be the batch's. The class must be
  the product's approved declaration in force: OTC, and not NDPS.

#### Slice B pull requests

| PR   | Covers                                                                               |
| ---- | ------------------------------------------------------------------------------------ |
| B-1  | B1–B4, B7–B11 for `none` (OTC) products only; no customer data is recorded           |
| B-2a | B5 evidence: the prescription record and its private, scanned image                  |
| B-2b | B5 and B6 at the counter: G, H and H1 sales citing a prescription; walk-in buyers    |
| B-2c | B6 registered patients, through a pharmacy projection of patientmaster registrations |
| B-3  | Discounts under the outlet ceiling (decision 3)                                      |

Until B-2b, a product whose class in force is `G`, `H` or `H1` is refused at the counter with
that reason; it is never sold without its evidence. Plans:
`docs/superpowers/plans/2026-10-28-counter-sale-slice-b1.md`,
`docs/superpowers/plans/2026-10-28-counter-sale-slice-b2a.md`.

B-2 was split (2026-10-28) because each part carries its own infrastructure:

- **The image** needs a scan worker of its own. Supply, workforce and practitioner each own a
  worker with a dedicated database role, and no module's worker processes another's files.
- **A registered patient** cannot be checked against patientmaster's tables. Pharmacy needs a
  projection of `patientmaster.registered.v1`, which carries ids only. An id the pharmacy has
  not seen is not accepted, because a supplied id is not authority.

#### B-2a decisions

- **A prescription is a record of its own,** owned by the outlet. It holds the prescriber's
  name, registration number and council, the prescription date and the patient's name, plus
  the patient's address where the prescription gives one. A sale cites it in B-2b; nothing
  sells against it in B-2a.
  - It is insert-only. A mistake is corrected by recording a new prescription; the old one
    stays, because a posted sale may cite it.
  - The date cannot be after the outlet's today. How long a prescription stays valid for
    dispensing is a legal and clinical rule that B-2b must source; B-2a records the date and
    decides nothing from it.
- **Images** are PNG or JPEG pages, up to 10 MiB each. Several pages may belong to one
  prescription. Each follows the supply licence evidence lifecycle: upload intent, exact-size
  checksummed upload, quarantine, ClamAV scan by the pharmacy evidence worker, release. Only a
  released image can be downloaded, and every download is audited.
  - An image cannot be revoked in B-2a. Once sales can cite a prescription (B-2b), revoking a
    cited image would remove the evidence for a posted sale, so the rule is decided there.
- **Who sees it.** `pharmacy.prescription.record` and `pharmacy.prescription.read` are held by
  the pharmacist alone. The owner holds neither: a prescription is patient data, and nothing in
  the owner's work needs to read it. Both also need the outlet grant, `editor` to record.
- **Events** carry ids, the scan attempt and a status, never prescriber or patient details.

### Slice C: after the sale

Returns as credit notes against the original invoice line and batch, with stock back to
quarantine for pharmacist re-release (#163, #202). Also:

- the end-of-day report: sales, tenders, credit notes and stock consumed (#1060);
- the H1 register report (#1062);
- the payment gateway (#210, #211).

## Not covered by A–C

Admission and bed billing (#1387), service charges (#1388), e-prescription dispensing (#170),
insurer splits, NPPA ceilings (#1371), credit sales and customer credit, B2B and e-invoicing.

## Product owner decisions (2026-10-27)

1. **Licence gate: yes.** An outlet with no reviewed licence valid today sells nothing.
2. **Outlet-declared class (option b).**
   - Until the drug master is loaded, a pharmacist records each product's schedule class
     and NDPS flag, with the source (for example, the pack label or the CDSCO list).
   - A second person reviews it before the product can be released or sold.
   - Invoices and registers show it as "declared by the outlet".
   - Once the product is linked to the drug master (later slice), the master's class wins,
     and a disagreement blocks sale until reviewed.
3. **Discounts: my call, recorded for review.**
   - Only `pharmacy.sale.discount` holders may discount, always with a reason.
   - Each outlet has a configured ceiling as a percentage of the line price. It defaults to
     0, meaning no discounts, until a manager sets it.
   - A discount above the ceiling is refused.
   - I set no ceiling value myself; that is a commercial decision.
4. **Invoice series: one per outlet per financial year.** GST invoice series run per
   registration, and an outlet is the unit that holds the GSTIN and the stock. A
   hospital-wide view is a report, not a shared counter.

### Built differently from A2

Linking a catalogue product to the drug master needs a pharmacy-side projection of drug
master products and classes (modules never read each other's tables). The declared class
(decision 2) carries slice A and B. The drug master link, and its override, become a slice
of their own once a licensed release exists.
