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

From B-2b, a product whose class in force is `G`, `H` or `H1` is sold only against a recorded
prescription line; it is never sold without its evidence. Plans:
`docs/superpowers/plans/2026-10-28-counter-sale-slice-b1.md`,
`docs/superpowers/plans/2026-10-28-counter-sale-slice-b2a.md`,
`docs/superpowers/plans/2026-10-28-counter-sale-slice-b2b.md`.

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

#### B-2b decisions (2026-10-28)

**Source.** Drugs Rules 1945, Rule 65, from CDSCO's 2022 compilation (India knowledge source
S09), read for this slice:

- **65(9)(a):** Schedule H, H1 and X are sold by retail only on, and in accordance with, a
  registered medical practitioner's prescription.
- **65(10):** the prescription is written, signed and dated. It names the patient with
  their address, and states the total quantity to be supplied and the dose.
- **65(11)(a)–(b):** it is dispensed once, unless the prescriber states it may be dispensed
  more often, and then only as stated.
- **65(11)(c):** at dispensing, the seller's name and address and the date are noted on the
  prescription.
- **65(11A):** no other preparation may be supplied in its place.
- **65(3):** every supply on prescription is entered in the prescription register (or
  numbered cash memo books). The entry includes the prescriber's name and address, the
  patient's name and address, the drug and quantity, and for Schedule C, H and H1 the
  manufacturer, batch and expiry.
- **The H1 register:** a separate register with the prescriber's name and address, the
  patient's name, the drug and the quantity supplied, kept for three years.
- **No validity period.** The rules state no age after which a prescription stops being
  valid. They limit how often it is dispensed.

**Decisions** (product owner, 2026-10-28):

1. **What was prescribed is recorded.** A prescription has 1–20 lines. Each line holds:
   - the drug as written;
   - the catalogue product it is dispensed as;
   - the total units to be supplied;
   - the dispensings allowed: 1 unless the prescriber states more, at most 12.

   Recording a prescription now needs at least one line. Prescriptions recorded in B-2a
   have none and can never be sold against.

2. **A prescription sale cites a prescription line.** A G, H or H1 sale line names the
   prescription line it dispenses, and the database checks it:
   - the line's product is the product sold, so there is no substitution (65(11A));
   - the units sold against the line, across every sale, stay within its total;
   - the sales citing it stay within its dispensings allowed (65(11)(a)–(b));
   - the prescription is dated on or before the sale date.

   An OTC line cites no prescription. Directions to dispense "at stated intervals" are not
   modelled: the pharmacist records the number of dispensings and applies the intervals.

3. **The prescriber's address is recorded.** H and H1 sales need both the prescriber's and
   the patient's address on the prescription (65(3), 65(10)(b)). A prescription without
   them can be read but not dispensed under H or H1. The pharmacist records a corrected
   prescription.
4. **No age limit.** The law sets none and none is invented. The counter shows the
   prescription date. Whether an outlet wants its own limit is open for pharmacist review.
5. **Schedule G needs a prescription.** This is our rule, stricter than 65(9), which covers
   H, H1 and X only. A Schedule G pack is labelled "prescription drug – caution", and
   treating it as OTC is the product owner's call to relax.
6. **Endorsement.** Every dispensing is recorded against its prescription line, with the
   sale, the outlet and the date. The counter reminds the pharmacist to note the seller's
   name, address and date on the paper prescription (65(11)(c)).
7. **Walk-in buyer.** The buyer's name and phone are optional on any sale, including H1.
   The patient's identity comes from the prescription. They are pharmacy-owned data. They
   never appear in events. They are shown with the invoice to `pharmacy.sale.read` holders
   only.

Not covered: Schedule X and NDPS (#165, #171), the H1 register report (#1062, slice C), and
registered patients (B-2c). The data the register needs is captured from this slice on.

#### B-2c is blocked (2026-10-28)

A registered patient is referenced "under the outlet's affiliation" (B6). No affiliation can be
verified today: `pharmacy_hospital_affiliations.verification_status` is constrained to
`pending_validation` and no verification flow exists. An unverified affiliation is not
authority to read or cite a hospital's patients, so B-2c waits for affiliation verification
(tracked in its own issue) rather than accepting a supplied patient id.

#### B-3 decisions (2026-10-28)

**Source.** CGST Act 2017, s.15(3)(a), as published on CBIC's tax information portal: the
value of a supply does not include a discount given before or at the time of supply and
recorded in its invoice. CBIC Circular 92/11/2019-GST applies the same rule. So the GST on a
discounted line is computed inside the discounted amount.

1. **A discount is a percentage of the line**, in basis points (1–10000). It applies to each
   batch line the product is allocated to: `discount = floor(list amount × bps ÷ 10000)`,
   with `amount = list amount − discount` and tax inside `amount` as before. Rounding down
   keeps the discount within the ceiling. Fixed-amount discounts are not in this slice: an
   amount cannot be split across batch lines without a rounding rule nobody has decided.
2. **Who may discount.** `pharmacy.sale.discount` has no system role. A tenant administrator
   assigns it, as decision 3 says ("pharmacy manager, tenant-assigned"). The person posting or
   quoting a discounted sale must hold it. A sale needs a reason (5–200 characters) whenever
   any line is discounted.
3. **The ceiling** is recorded per outlet as a percentage in basis points (0–10000), with a
   reason. The latest record is in force, and the record history is kept. An outlet with no
   record has a ceiling of 0, so discounts are refused until one is recorded. The pharmacy
   owner records it (`pharmacy.discount.manage`). No value is set by us: the ceiling is a
   commercial decision.
4. **The database checks it.** A line's discount must be the rounded percentage of its list
   amount, within the ceiling in force when the line is written. A discounted sale must carry
   its reason. A discount that brings a line to nothing is refused, like any line that prices
   to nothing.
5. **The invoice** shows each line's list amount and discount, and the sale's total discount.
   `sale_posted.v1` is unchanged; its total is the net amount charged.

### Slice C: after the sale

Returns as credit notes against the original invoice line and batch, with stock back to
quarantine for pharmacist re-release (#163, #202). Also:

- the end-of-day report: sales, tenders, credit notes and stock consumed (#1060);
- the H1 register report (#1062);
- the payment gateway (#210, #211).

#### Slice C pull requests

| PR  | Covers                                                                               |
| --- | ------------------------------------------------------------------------------------ |
| C-1 | Returns at the counter as GST credit notes, stock back to quarantine, refunds        |
| C-2 | The H1 register (#1062): H1 supplies and returns, from the sale and credit-note data |
| C-3 | The end-of-day report (#1060): sales, credit notes, tenders and refunds by day       |
| C-4 | The payment gateway (#210, #211), confirming UPI and card tenders                    |

#### C-1 decisions (2026-10-28)

**Sources:**

- CGST Act 2017, s.34(1) and (2), and CGST Rules 2017, Rule 53(1A), as published on CBIC's tax
  information portal:
  - a credit note is issued for goods returned;
  - it is declared no later than 30 November after the end of the financial year of the supply;
  - its serial number is consecutive, at most 16 characters and unique for a financial year;
  - it carries the corresponding invoice's number and date, the taxable value, rate and tax
    credited;
  - for an unregistered recipient it carries the recipient's name and address, the delivery
    address and the state with its code. No value threshold applies.
- Drugs Rules 1945, Rule 65(3): the register records each supply. A return is recorded against
  it, not by removing it.

**Decisions:**

1. **A return is a credit note against one posted sale of the same outlet.** Each credit line
   names a sale line, which is one batch. The units returned on a line, across all its credit
   notes, never exceed the units sold. A reason of 5–200 characters is required.
2. **Amounts reverse the original line, not today's prices or rates.** Credits are cumulative
   pro rata, so a full return credits exactly what was charged. With `k` units returned so far
   out of `q` sold:
   - the amount credited so far is `floor(amount × k ÷ q)`;
   - each tax half credited so far is `floor(half × k ÷ q)`;
   - the taxable value is the rest.

   Each credit line is the step from the previous cumulative total.

3. **The number** is `PREFIX/FY/Cn`, from a gap-free series per outlet and the financial year of
   the credit note's date. It is at most 16 characters; a full series refuses the return. The
   `C` keeps it apart from the invoice series.
4. **The recipient.** The person refunded is recorded by name and address (Rule 53(1A)(f)). The
   delivery address is the outlet, since it is a counter sale, and the state is the sale's
   place of supply.
5. **The deadline.** A credit note is refused after 30 November following the end of the sale's
   financial year (s.34(2)). The earlier limit, the date of the annual return, is not known to
   the system; filing that return before then is the outlet's responsibility to observe.
6. **Stock goes to quarantine, never straight back to a shelf.** Returned units are received into
   a quarantine location the pharmacist chooses, as movement kind `return`, which exists only
   under a credit note line. To sell them again, the pharmacist releases them (slice A3). That
   release re-checks expiry, MRP, safety and class. Medicine that left the pharmacy is not
   assumed fit to sell.
7. **The refund** is recorded as tenders equal to the credit note total: cash, or UPI or card
   with a reference.
8. **A prescription is not restored.** Units dispensed against a prescription line stay
   dispensed. The return appears in the register (C-2).
9. **Permissions.** `pharmacy.sale.return` (pharmacist) issues a credit note, with the outlet
   `editor` grant. `pharmacy.sale.read` reads it.
10. **Events.** `credit_note_issued.v1` carries ids, the number, the total and each line's product,
    batch and units. It carries no recipient details.

**Not covered:**

- destruction and disposal of returned stock;
- recording a cold-chain or condition judgement (all returns go to quarantine);
- returns from wards and failed deliveries;
- credit notes without goods (billing errors);
- debit notes;
- e-invoicing (#539);
- a digital signature. The credit note records who issued it.

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
