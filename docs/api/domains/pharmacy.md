# PharmaConnect integration specification

[Contract index](../README.md) · descriptive current slices plus outstanding contracts

## What the domain covers

The `pharmacy` module is pharmacy management, not a hospital add-on. It runs a
standalone retail chemist with no hospital at all, and a hospital's own
pharmacy, which is the same outlet with an optional affiliation. Selling,
stock, registers, tax and payables work the same for both.

How a patient pays differs, and the module does not own that difference:

- **At the counter** (a retail chemist, or a hospital pharmacy's OTC counter),
  the pharmacy asks for the payment itself, with `purpose=counter_sale`
  ([payment requests](#payment-requests-1386-c-4a)).
- **For an inpatient,** medicines are charged to the hospital bill and paid at
  discharge. Hospital billing asks for that payment; the pharmacy reports what
  was dispensed against the encounter and does not collect it.

## Scope and authority

The hierarchy is tenant → pharmacy organization → outlet, with optional hospital affiliations. Independent pharmacies do not need a fake hospital. Route permission and a current explicit outlet grant are separate. An affiliation, even a verified one, is neither patient consent nor prescription/stock authority. See [ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md), [country/trading boundaries](../../adr/0026-pharmaconnect-country-and-trading-boundaries.md) and [existing API detail](../../architecture/pharmaconnect-api.md).

The [runtime reference](../reference/README.md#pharmacy) lists every implemented path. The outlet root is `/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`. Organization/outlet administration and the legacy `/medications`/`dispenses` surfaces are separate route families; do not equate the legacy dispense task with the outlet stock ledger.

## Current contract families

| Family                                        | Owning records / schema                                                                                                                                                          | Rules and consumer boundary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registry and grants                           | Pharmacy organizations, outlets, optional hospital affiliations, outlet access; `contracts/pharmacy.json`                                                                        | UUIDv7 path records; verified session tenant; country/timezone explicit; administrator access management does not imply operational grant                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Private catalogue                             | Product fields, ingredients, packs, unit of issue, MRP/currency, status/version                                                                                                  | Exact strengths/quantities and pack conversions; identifiers and lifecycle/version in catalogue events, not a global shared product table                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Batches and locations                         | Lot, manufacturing/expiry dates, storage/quarantine location and rack/shelf address                                                                                              | Dates interpreted in outlet timezone; a lot is not a clinical authorization to dispense                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Stock ledger                                  | Immutable movement, receipt/correction evidence, quantity, location and batch                                                                                                    | Quarantine receiving is not clinically usable stock; read current safety/expiry state before fulfillment                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Batch safety journal                          | Immutable concern records and context references                                                                                                                                 | Recording an unreviewed concern does not silently implement regulatory recall/release decisions                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Stock register, reconciliation and CSV export | Scoped period/register rows, expiry view, auditable export                                                                                                                       | Preserve period identity and filters; report freshness and excluded/missing scope; no floating-point quantities or cross-outlet leakage                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Operational accounts                          | Purchase/expense payables, external settlement, credit/reversal references                                                                                                       | Immutable attributable entries, versioned policies, currency and exact minor units; recording external settlement is not charging a payment provider                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Tax setup (#1283)                             | Outlet tax registrations and product tax classifications, both append-only and effective-dated; `contracts/pharmacy.json`, scheme structure in `contracts/country-profiles.json` | The country profile describes how tax works (India: GSTIN, HSN, CGST+SGST/UTGST or IGST) but carries no rate; the hospital records each product's rate. `pharmacy.tax.manage` (owner) only. Nothing in force before the outlet's today; a same-date correction needs a reason. A country with no scheme is `409 tax_scheme_unavailable`. `GET …/tax-registrations/in-force` answers the registration in force today; products carry their classification in force. Not covered: sales, GSTN verification, approval of classifications, composition scheme, cess, e-invoicing |
| Overview (#576)                               | Counts only, owned by the lists they count                                                                                                                                       | `GET …/outlets/{outlet}/overview` (`pharmacy.catalogue.read`) returns, for the outlet's date: stock lines expired / within 30 / within 90 days (`pharmacy.stock.read`), safety concerns recorded in the last 30 days (`pharmacy.stock.read`), bills with an amount outstanding overdue / due within 7 days (`pharmacy.accounts.read`), unclassified products, and whether a tax registration is in force (`pharmacy.tax.manage`). Each count uses the same scope as its list: `?due=overdue                                                                                  | week`on payables,`?recorded=30d`on safety incidents (outlet-wide; otherwise`batch_id`is required), the stock register's`expiry`/`balance`, the catalogue's `tax=unclassified`. A section the caller may not see is absent, never zero. Waiting patients come from the counted `/dispenses/waiting`. Not covered: verification queue, fulfilment blockers, reorder levels, Schedule H1/X register, recalls |
| Paged outlet lists (#576)                     | Counted pages over the same filters as the uncounted lists                                                                                                                       | Counted (`page.total`), newest first, each guarded by the outlet grant inside its snapshot: `GET …/accounts/payables/counted` (`?due=overdue\|week`), `…/accounts/inventory-audit/counted` (`pharmacy.accounts.read`); `…/stock/safety-incidents/recent` (last 30 days) (`pharmacy.stock.read`); `…/catalogue/products/counted` (`q`, `status`, `tax=unclassified`, which is a 400 where the outlet has no tax scheme; `pharmacy.catalogue.read`). The uncounted routes are unchanged.                                                                                       |
| Legacy dispense queue                         | Visit-linked pending/dispensed task                                                                                                                                              | Guarded one-time completion; retry after completion returns 409; no signed prescription validation or stock deduction established by this handler The Dispense queue reads `GET /v1/pharmacy/dispenses/waiting` (oldest first, `?q=` patient-name search) and `/dispensed` (newest by `dispensed_at`), both counted (#1295); `GET /dispenses?status=` is unchanged.                                                                                                                                                                                                          |

Canonical request fields and constraints live in [contracts/pharmacy.json](../../../contracts/pharmacy.json), generated `input_types.go` and `pharmacy-contract.ts`. Queries and response projections live in their owning handlers. Existing [API documentation](../../architecture/pharmaconnect-api.md), [stock-register delivery](../../delivery/2026-10-17-pharmacy-inventory.md), [batch-safety acceptance](../../delivery/2026-10-18-batch-safety-acceptance.md) and [stock export](../../delivery/2026-10-18-pharmacy-stock-export.md) provide detailed slice evidence.

Use client resource IDs and exact `expected_version` semantics of each handler; supplying a valid-looking UUID does not make every PUT replaceable or every POST retry-safe. The optional expected-tenant header returns `409 session_changed` if session context drifted. Never retry an in-flight action under a newly selected tenant.

### Outlet drug licences (#1386 A1)

An outlet may sell only while it holds an approved licence that is valid on the outlet's own date ([counter sale design](../../superpowers/specs/2026-10-27-counter-sale-design.md)).

| Route                                      | Behaviour                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/outlets/{outlet}/licences/{uuidv7}` | Records a licence. Requires `pharmacy.licence.manage` and the outlet editor grant. Fields: `licence_kind retail\|wholesale`, `form_label` as printed, number, issuing authority, `valid_from`/`valid_until`, and the pharmacist in charge with their registration. Idempotent; reusing an id with different details is 409 |
| `POST …/licences/{id}/review`              | Records `{decision: approved\|rejected, reason}`. Requires `pharmacy.licence.review`. The reviewer must not be the person who entered the licence: the database refuses it, and the API returns 409 `four_eyes`. A licence gets one decision; correct a mistake by entering a new licence                                  |
| `GET …/licences`                           | Cursor page of licences with their decision                                                                                                                                                                                                                                                                                |
| `GET …/licence-status`                     | `{in_force, today, licence, reason}`, judged on the outlet's date                                                                                                                                                                                                                                                          |

Later slices call `pharmacy_outlet_licence_in_force(tenant, outlet, day)` inside their own transactions. Not covered yet: uploading the licence document (the reviewer checks the original) and renewal notices.

### Declared schedule class (#1386 A2)

Until the drug master is loaded, each product's Drugs Rules class (`none|G|H|H1|X`) and NDPS flag are declared by the outlet, together with their source. A second person must approve the declaration.

| Route                                             | Behaviour                                                                                                                                     |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/catalogue/products/{id}/schedule/{uuidv7}` | Declares `{regulatory_class, ndps, source}`. Requires `pharmacy.schedule.declare`. Idempotent                                                 |
| `POST …/schedule/{declaration}/review`            | Records `{decision, reason}`. Requires `pharmacy.schedule.review`. The reviewer must not be the declarer (database-enforced; 409 `four_eyes`) |
| `GET …/catalogue/products/{id}/schedule`          | Returns `{in_force, origin: declared_by_outlet, reason, history}`                                                                             |

Only the **latest** declaration counts, and only once it is approved. If the latest one is still awaiting review or was rejected, the product has no class, so an older approval cannot bypass a stricter reclassification. Later slices call `pharmacy_product_schedule_in_force(tenant, outlet, product)`.

### Sellable stock (#1386 A3)

Every location is either `quarantine` or `sellable`, fixed when it is created. Receipts and corrections only ever land in quarantine. Stock reaches a sellable location only through a release.

| Route                           | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/stock/batches/{id}`      | Each new batch now carries the `mrp_minor` printed on the lot (exact paise, at least 1) and an optional `purchase_cost_minor`. A batch's MRP never changes. Batches received earlier have no MRP and can never be released                                                                                                                                                                                                                                                                                                                                                                                              |
| `PUT …/stock/locations/{id}`    | Accepts an optional `stock_status: quarantine\|sellable`. The default is `quarantine`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `PUT …/stock/releases/{uuidv7}` | Moves `{batch_id, from_location_id, to_location_id, quantity_units, reason}`. Requires `pharmacy.stock.release`. It writes a negative movement from quarantine and a positive one into sellable, in one transaction, under the overdraft guard. It returns 409 `not_releasable` with the reason unless every check passes: the move is quarantine to sellable, the batch expires after the outlet's today, it has an MRP, it has no safety incident, the product is active and tax-classified, and the product has an approved schedule class other than X or NDPS. The release records the class it was released under |
| `GET …/stock/balance`           | Reports the location's real `stock_status`. `dispensable_units` stays 0; what a sale can take is `GET …/sales/availability` (below)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

### Counter sale (#1386 B-1)

An OTC sale at the counter: one command takes sellable stock earliest expiry first, prices it at the batch MRP, computes GST inside that price, and posts an invoice numbered in the outlet's gap-free series for the Indian financial year.

| Route                                  | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/invoice-profile/{uuidv7}`       | Records `{legal_name, address, invoice_prefix}`, the seller details an invoice prints. The prefix is 1 to 4 capital letters or digits. Requires `pharmacy.tax.manage`. The latest profile is in force; invoices keep a snapshot                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `GET …/invoice-profile`                | Returns `{profile}`, or `null` when none is recorded. Requires `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `GET …/sales/availability?product_id=` | Returns `{sellable_units, nearest_expiry}`, using the same predicate as allocation, `pharmacy_sellable_buckets`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `POST …/sales/quote`                   | Prices `{lines}` exactly as a sale would, running the same checks, and returns `{currency, total_minor, taxable_minor, cgst_minor, sgst_minor, lines}` without posting anything. Requires `pharmacy.sale.create`. If stock moves before posting, the sale refuses tenders that no longer match                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `PUT …/sales/{uuidv7}`                 | Posts `{lines: [{product_id, quantity_units}], tenders: [{kind: cash\|upi\|card\|upi_verified, amount_minor, tendered_minor?, reference?, payment_request_id?}]}`. A `upi_verified` tender names a counter payment that is `paid`, of this outlet, for exactly the tender's amount and the sale's currency, and on no other sale (enforced by a foreign key, a unique index and an insert trigger); otherwise 409 `not_sellable`. `upi` stays the reference-only tender. Requires `pharmacy.sale.create` and the outlet `editor` grant. Returns 201 with the invoice, or 200 for a replay of the same intent; a different intent is 409. Every refusal is 409 `not_sellable` with its reason, and nothing is kept |
| `GET …/sales/{id}`, `GET …/sales`      | Return the invoice (seller snapshot, lines, tenders) and the paged list. Require `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |

**What a sale requires.** The outlet must have:

- a reviewed **retail** drug licence valid today (wholesale does not count);
- a GSTIN in force, registered in a state. A union territory, which levies UTGST, is refused
  until that tax head is modelled;
- an invoice profile.

Each product must:

- be active;
- be priced in the outlet's currency;
- be tax-classified on the sale date;
- have an approved class `none` that is not NDPS.

Schedule G, H and H1 are sold only against a recorded prescription line (B-2b, below). They are never sold as OTC. X and NDPS are refused until their controls exist.

**Allocation and stock.**

- A sale draws from sellable buckets whose batch has an MRP, expires after the outlet's date, and has no safety incident.
- Buckets are used earliest expiry first. Each batch used becomes its own invoice line, up to 200 lines.
- Short stock refuses the sale and names each short product.
- Bucket locks are taken in one global order before balances are read, so two tills cannot sell the same last unit.

**Pricing and GST.** Per line:

- `amount = floor(mrp × units ÷ pack)`, so a line never exceeds MRP. The database checks it.
- CGST and SGST are equal halves of the tax inside the amount, rounded half up.
- The taxable value is the remainder.
- The place of supply is the outlet's GSTIN state.
- The total is the exact sum of the lines, with no round-off.
- NPPA ceiling prices (#1371) are not checked, and the invoice reports `nppa_ceiling_checked: false`.

**Tenders.**

- At most one cash tender, carrying the amount handed over; change is computed from it.
- UPI and card tenders carry a reference.
- The tenders must sum to the total.

**Database guarantees.**

- The invoice number is `PREFIX/FY/N`, at most 16 characters. The series only steps by one, inside the sale's transaction.
- Invoices, lines and tenders are immutable, and nothing can be added to a committed sale.
- At commit, a deferred check proves the sale equals its lines, is paid by its tenders, and has one stock movement per line.
- A `sale` movement must take exactly its line's units off a sellable location.
- Each line is checked against its source: the batch is the product's and sellable on the
  sale date, and its lot, expiry and MRP are the batch's. The class is the approved
  declaration in force, OTC and not NDPS. So the never-above-MRP check compares against
  the batch's own MRP.
- A line that prices to nothing (a few units of a cheap pack) refuses the sale.
- On the counter screen, a failure other than a refusal keeps the sale's id, so a retry
  replays it and never posts a second invoice.

**Events and scope.**

- `sale_posted.v1` carries ids, the invoice number, currency, total, and each line's product, batch and units. It carries no buyer or prescription data.
- `sale_posted.v1` carries no buyer or prescription data. Prescription records and their scanned pages are B-2a; selling against them and walk-in buyers are B-2b; registered patients are B-2c; discounts are B-3 (below).

### Payment requests (#1386 C-4a)

The pharmacy asks the [payments core](payments.md) to collect money and
records what it is told; it never reads a payments table. `purpose` says
what the money is for — `counter_sale` today — so later pharmacy flows add a
purpose rather than a new event.

| Route                                | Behaviour                                                                                                                                                                                                                                                                       |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/counter-payments/:id`         | Outlet editor with `pharmacy.sale.create`. Body `{amount_minor}`; currency from the outlet's country. Creates the request (`201`) and publishes `payment_requested.v1` in the same transaction; the same id with the same amount returns it (`200`), with another amount `409`. |
| `GET …/counter-payments/:id`         | The request's state for the counter: `requested`, `awaiting_payment` (with the QR image URL and expiry), `cancelling`, `paid` (provider payment id, RRN, `late`), `failed` (reason), `expired` or `cancelled`.                                                                  |
| `POST …/counter-payments/:id/cancel` | Only from `requested` or `awaiting_payment` (`409` otherwise). Moves to `cancelling` and publishes `payment_cancelled.v1`; payments answers cancelled, or paid if the money landed first.                                                                                       |

Outcome events move a request with status-guarded updates: a redelivered or
reordered event changes nothing it should not, `paid` is never undone, and a
payment reported after expiry or cancel makes the request `paid` with
`late=true` rather than going unrecorded. A paid report whose amount or
currency differs from the request is ignored. Outcome consumers hold no
outlet grant; each sees and writes only the request its event names
(`app.pharmacy_payment_outcome`).

`GET …/counter-payments` (a page, `pharmacy.sale.create`) lists the outlet's paid requests on no sale yet, newest first, so money taken for a sale that then failed to post can still be put on a sale of the same amount (#1386 C-4b).

### The Schedule H1 register (#1386 C-2, #1062)

Drugs Rules 1945, Rule 65(3)(h): an H1 supply is recorded in a separate register at the time of supply. The entry gives the prescriber's name and address, the patient's name, the drug and the quantity. The register is kept for three years and is open for inspection.

| Route                                 | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/registers/h1?from=&to=`        | The paged register, newest first, for a period of outlet dates of at most 366 days. Each entry has the supply date, invoice and line, the prescriber's name, registration and address, the patient's name and address, the prescription date, the drug (SKU, brand and generic name), batch and expiry, units supplied, units since returned on credit notes, and the selling pharmacist. Requires `pharmacy.prescription.read` (pharmacist) |
| `GET …/registers/h1/export?from=&to=` | The same entries as a CSV, oldest first. Formula-like cells are neutralised. It is refused above 1000 entries rather than truncated, and returns its row count and SHA-256                                                                                                                                                                                                                                                                   |

- **A read, never a copy.** Each entry is an H1 sale line, written in its sale's transaction and never changed. A return is shown against its supply, never by removing it. Nothing in the register can be edited.
- **Kept.** Sales, prescriptions and credit notes are insert-only and never purged. A future retention job must keep H1 entries for at least three years.
- **Audited.** Every page read writes an `audit_events` row (`register_read`) naming the period. Every export writes one (`register_export`) and logs the row count and digest. The pharmacy owner cannot read the register: it shows patient and prescriber details.
- **Not covered.** Schedule X and NDPS registers (#165, #171), and printing as a bound register.

### The daily sales report (#1386 C-3, #1060)

| Route                           | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET …/reports/daily?from=&to=` | One row per outlet date and currency with a sale or credit note, oldest first, for a period of at most 366 days. Each row has the sales' count, total, taxable value, CGST, SGST, discount and units; the credit notes' count, total, taxable value, CGST, SGST and units; the net; tenders and refunds by kind, with verified UPI (`upi_verified_minor`) apart from reference-only UPI. Requires `pharmacy.sale.read` (pharmacist, owner) |

- **A read of posted records.** Sales and credit notes are insert-only, so the report is summed afresh and cannot drift. A credit note counts on its own date, not its sale's.
- **Never across currencies.** A row is one day and one currency.
- **Cash is what was applied to the sale.** Change handed back is not income.
- **No row for a quiet day.** A day with no sale or credit note is absent, not shown as zeros.
- **Not audited.** It holds totals only, with no patient, prescriber or buyer detail.
- **Not covered.** Shift or till close, purchase cost and margin, item-wise and patient-wise reports, and a CSV export.

### Returns as credit notes (#1386 C-1)

A return is a GST credit note against one posted invoice. The rules followed are CGST Act 2017, s.34, and CGST Rules 2017, Rule 53(1A), as published by CBIC. Returned medicine goes to quarantine and is sold again only after a pharmacist releases it (A3).

| Route                                                    | Behaviour                                                                                                                                                                                                                                                                                                                                                                     |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/credit-notes/{uuidv7}`                            | Takes `{sale_id, lines: [{sale_line, quantity_units, quarantine_location_id}], refunds: [{kind, amount_minor, reference?}], reason, recipient: {name, address}}`. Requires `pharmacy.sale.return`, held by the pharmacist, with the outlet `editor` grant. Returns 201, or 200 for a replay; a different intent is 409. Every refusal is 409 `not_returnable` with its reason |
| `GET …/credit-notes/{id}`, `GET …/credit-notes?sale_id=` | The credit note (and the paged list). It carries the original invoice's number and date, the seller, the recipient, and each line's HSN, batch, units, rate, taxable value and tax credited. Requires `pharmacy.sale.read`                                                                                                                                                    |
| `GET …/sales/{id}`                                       | Each line now reports `returned_units`                                                                                                                                                                                                                                                                                                                                        |

- **Amounts reverse the sale line.** A credit uses the line's charged amount and tax, net of any discount, and the sale's own rate. Credits are the cumulative pro-rata step, `floor(v × (k+r) ÷ q) − floor(v × k ÷ q)`, of the taxable value and of each tax half, and the amount credited is their sum. So no step is negative, and all the returns of a line together credit exactly what it charged.
- **Limits.**
  - The units returned on a line, across all credit notes, never exceed the units sold.
  - Every line goes to a quarantine location.
  - The refunds must equal the total.
  - The credit note is refused after 30 November following the end of the sale's financial year (s.34(2)). The other limit in s.34(2), the date the annual return is filed, is not known to the system and is the outlet's to observe.
- **The number** is `PREFIX/FY/Cn`. It comes from its own gap-free series per outlet and per financial year of the credit note's date.
- **Database guarantees.**
  - Each credit line locks its sale line and checks the units and the pro-rata amounts.
  - A credit note is sealed to its own transaction.
  - At commit, the note must equal its lines, be refunded in full, and have one `return` movement per line.
  - A `return` movement is accepted only as its credit line's units of that line's batch, into its quarantine location.
- **Events.** `credit_note_issued.v1` carries ids, the number, currency, total and each line's product, batch and units. It carries no recipient details.
- **Not covered.**
  - Destruction and disposal, and a recorded condition or cold-chain judgement: every return goes to quarantine.
  - Returns from wards and failed deliveries.
  - Credit notes without goods, and debit notes.
  - E-invoicing (#539).
  - A digital signature: the credit note records who issued it.

### Discounts (#1386 B-3)

A discount recorded on the invoice at the time of supply is outside the value of supply (CGST Act 2017, s.15(3)(a), as published by CBIC; CBIC Circular 92/11/2019-GST), so GST is computed inside the discounted amount.

| Route                             | Behaviour                                                                                                                                                                                               |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/discount-ceiling/{uuidv7}` | Records `{max_bps 0–10000, reason}`, the outlet's ceiling in basis points. Requires `pharmacy.discount.manage`, which the pharmacy owner holds. The latest record is in force, and every record is kept |
| `GET …/discount-ceiling`          | Returns `{ceiling}`, or `null`. With no ceiling recorded the outlet allows no discount. Requires `pharmacy.sale.read`                                                                                   |

- **Giving a discount.**
  - A sale or quote line takes an optional `discount_bps` (1–10000). It needs `pharmacy.sale.discount`; without it the request is 403 before anything is priced.
  - That permission has no system role; a tenant administrator assigns it.
  - A discounted sale needs `discount_reason` (5–200 characters), and only a discounted sale may carry one.
  - A discount above the ceiling, or one that leaves nothing to charge, is 409 `not_sellable`.
- **Arithmetic.** Each batch line's discount is `floor(list amount × bps ÷ 10000)`, and the line charges the rest, with CGST and SGST inside it. The invoice shows each line's `list_amount_minor`, `discount_bps` and `discount_minor`, and the sale's `discount_minor` and `discount_reason`.
- **Database guarantees.** A line's discount must be its rounded percentage, its list amount must be within MRP, and something must be charged. The discount must be within the ceiling in force when the line is written, so a ceiling lowered after a quote refuses that sale. A discounted sale must record its reason.
- **Not covered.** Fixed-amount discounts, whole-sale discounts, approval by a second person, and promotions or schemes.

### Ward medicine billed to the admission (#1387)

A sale may be billed to the admission occupying a bed instead of collected at the counter. See the [design](../../superpowers/specs/2026-10-29-inpatient-medicines-to-admission-account-design.md).

- **Pharmacy's copy of who is in which bed.** `pharmacy_bed_occupancies` is fed by `pharmacy-bed-occupancy-started` and `-ended`, from MediCore's events. It holds ids and the ward, room and bed codes. Either event may arrive first, and a stay starts and ends once.
- **Resolve.** `GET …/outlets/:outlet/admission-occupant?hospital_id=&bed_code=` (or `room_code=`) needs `pharmacy.sale.create`, at a hospital that has verified the pharmacy (otherwise `404`).
  - It answers the one open stay, as ids and location codes, without when the stay began.
  - An empty bed is `404 no_occupant`; a room with several occupied beds is `409 ambiguous`.
  - Every resolve is audited (`audit_events`, `pharmacy_admission_resolve`), a miss included.
- **Confirm.** The counter confirms the patient with patientmaster's lookup by `patient_id`.
- **Post.** The sale carries `admission: {occupancy_id}` and one `admission_account` tender for the total. Nothing is collected. The sale is re-resolved under a lock, and it stores:
  - the admission, the stay, the bed code and the resolution time;
  - the occupant as its registered patient, so the B-2c rules apply too.

  It publishes `admission_charge_posted.v1`. The database refuses:
  - a sale on a stay that is no longer open;
  - an admitted sale with any other tender;
  - an `admission_account` tender on any other sale.

- **Daily report.** It shows `admission_account_minor` and `admission_account_refund_minor`, billed and returned to admission accounts rather than collected or paid out, so sales still reconcile to tenders.
- **Returns.** A credit note on an admitted sale refunds only through `admission_account`, and publishes `admission_charge_reversed.v1`. A wrong-bed sale is corrected by a full credit note and a new sale. Migration `0027_pharmacy`.

### Registered patients at the counter (#1386 B-2c)

A sale may name a registered patient instead of a walk-in buyer: `patient: {patient_id, hospital_id}`, never with `buyer` (`400`). The pharmacist finds the patient with patientmaster's [pharmacy lookup](patients-and-clinical.md#pharmacy-patient-lookup-1416b) by exact MRN at a hospital that has verified the pharmacy, and confirms the name, age and sex shown.

The sale posts only if the database accepts both conditions; otherwise it is `409 not_sellable`:

- `(patient_id, hospital_id)` is in `pharmacy_registered_patients` (a foreign key);
- the outlet's pharmacy holds an affiliation with that hospital whose latest verification step is `accepted` (trigger `pharmacy_sale_patient_affiliated`).

The sale takes the affiliation's lock, so a verification step being applied is ordered wholly before or after it. Pharmacy stores only the two ids (`patient_id` and `patient_hospital_id` on the sale read). `sale_posted.v1` carries no patient. The invoice says "Registered patient" with the hospital. Migration `0026_pharmacy`.

### Selling against a prescription (#1386 B-2b)

The source is Drugs Rules 1945, Rule 65 (CDSCO 2022 compilation): Schedule H and H1 are sold only on a prescription (65(9)), which names the patient's address and the total quantity (65(10)). It is dispensed only as often as the prescriber allows (65(11)), and nothing is supplied in its place (65(11A)). Schedule G is held to the same rule by product decision; that is stricter than Rule 65.

- **What was prescribed.** `PUT …/prescriptions/{uuidv7}` now requires `lines`, 1 to 20 of them. Each line is `{prescribed_as, product_id, quantity_units (1–100000), dispensings_allowed (1–12)}`. An optional `prescriber_address` is accepted. A prescription and its lines are recorded in one transaction and never change. `GET …/prescriptions/{id}` returns each line with `units_remaining` and `dispensings_remaining`. Prescriptions recorded before B-2b have no lines and cannot be sold against.
- **The counter's lookup.** `GET …/prescriptions/open-lines?product_id=` returns the lines for a product that can still be dispensed, newest first, at most 50. Each carries `addresses_on_file`.
- **The sale.** A sale or quote line takes an optional `prescription: {prescription_id, line_number}`:
  - It is required for G, H and H1 and refused on an OTC line.
  - The cited line must be for the same product, dated on or before the sale, and have enough units and a dispensing left.
  - For H and H1, the prescription must give both the prescriber's and the patient's address.

  Each refusal is 409 `not_sellable` with its reason. A sale whose allocation spans several batches is one dispensing. A replay dispenses nothing more.

- **Database guarantees.** The sale-line guard locks the cited prescription line and checks product, date, units, dispensings and addresses itself. A line carries a citation exactly when its class is not `none`. A bug in the handler cannot dispense more than the prescriber allowed.
- **Walk-in buyer.** A sale takes an optional `buyer: {name?, phone?}` (phone `^\+?[0-9]{6,15}$`). It is shown with the invoice to `pharmacy.sale.read` holders and never published.
- **Not covered.** Directions to dispense at intervals (the pharmacist records the number of dispensings), an age limit (the rules set none), Schedule X and NDPS (#165, #171), and the H1 register report (#1062).

### Prescription evidence (#1386 B-2a)

The outlet's record of a prescription, and its scanned pages. Nothing sells against it until B-2b. Every route needs the pharmacist's `pharmacy.prescription.record` (writes, with the outlet `editor` grant) or `pharmacy.prescription.read` (any outlet grant). The pharmacy owner holds neither: a prescription is patient data.

| Route                                             | Behaviour                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PUT …/prescriptions/{uuidv7}`                    | Records `{prescriber_name, prescriber_registration, prescriber_council, prescribed_on, patient_name, patient_address?}`. The date cannot be after the outlet's today. Returns 201, or 200 for a replay by the same pharmacist; any other body or recorder is 409. A prescription is never changed or deleted |
| `GET …/prescriptions/{id}`, `GET …/prescriptions` | The record and the paged list                                                                                                                                                                                                                                                                                |
| `PUT …/prescriptions/{id}/images/{uuidv7}`        | An upload intent `{filename, content_type: image/png\|image/jpeg, size ≤ 10 MiB, sha256}`, valid for 30 minutes. A replay with different details is 409. Without private storage configured it is 503 `evidence_unavailable`                                                                                 |
| `PUT …/images/{file}/content`                     | The exact bytes: matching type, size and checksum, and a passive image, before anything is written to quarantine. Returns 202 `scan_pending` and queues `prescription_image_scan_requested.v1`                                                                                                               |
| `POST …/images/{file}/retry/{attempt}`            | After `scan_error`, queues the next attempt only                                                                                                                                                                                                                                                             |
| `GET …/images/{file}/download`                    | Only an `available` page whose released generation, size and checksum match. Sent `private, no-store`, `nosniff`, as an attachment under a sandbox CSP. Every download writes an audit `read` event                                                                                                          |
| `GET …/prescriptions/{id}/images`                 | The paged pages with their status: `upload_pending`, `scan_pending`, `available`, `rejected` (`malware_detected`, `unsupported_content`, `integrity_mismatch`) or `scan_error`                                                                                                                               |

**The pharmacy evidence worker** (`cmd/pharmacy-evidence-worker`) consumes the scan request through Temporal (`pharmacy-prescription-image-v1`) and connects as `helivanta_pharmacy_evidence_worker` with `PHARMACY_EVIDENCE_WORKER_DATABASE_URL`; it never falls back to another module's credential. Its session names one file, and row security shows it that file only. It holds no privilege on prescriptions, can change only the lifecycle columns, and can write only its own audit rows and a `prescription_image_changed.v1` whose status the row holds. No page can be revoked yet. Once a sale can cite a prescription (B-2b), revoking a cited page would remove a posted sale's evidence, so that rule is decided there.

Until the worker is rolled out (#1406), uploaded pages stay `scan_pending` and are never downloadable.

### Stock across outlets (#1372)

`GET /v1/pharmacy/pharmacies/{pharmacy}/stock/positions` is a read-only,
pharmacy-level view of the existing outlet-owned stock ledger. It requires
`pharmacy.stock.read` **and a current explicit grant on every included outlet**;
a directory manager or tenant administrator receives no implicit stock grant.
No granted outlet in this pharmacy is 404; missing route permission is 403;
unavailable outlet authorization is 503. No hospital affiliation grants access.
The staff page is `/pharmacy/stock-overview`, through the Global API as usual.

- **Filters and paging.** An explicit `as_of` (RFC3339, microsecond precision,
  not future), `expiry=all|expired|unexpired|today|due_30|due_90|due_180`,
  `balance=all|on_hand`, and optional `outlet_id`. Unknown, repeated and empty
  filters are 400. A requested outlet outside the caller's grants or pharmacy
  is 404. Standard `limit`/`cursor` paging is bounded across the combined result,
  newest batch first with a scoped position hash as tie-breaker. Each outlet
  evaluates expiry in its own timezone, using the same source and filters as
  its stock register. Physical units include quarantine/expired stock;
  `dispensable_units=0` remains deliberately non-authoritative.
- **Scope disclosure.** `X-Pharmacy-Stock-Coverage` is `all_accessible` or
  `restricted_outlets_excluded`, including empty pages. The latter explicitly
  means that totals do not cover the whole pharmacy. Excluded outlet names,
  quantities and values are never disclosed. Coverage, totals, current grants
  and rows use one read-only REPEATABLE READ snapshot with an eight-second SQL
  statement timeout and ten-second request query budget. Every page/export
  reauthorizes; `as_of` freezes the movement cutoff, not future grant changes.
- **Product totals.** Each position includes the owning outlet ID/code/name,
  currency, `product_on_hand_units`, `known_purchase_value_minor` and
  `uncosted_units`. The last three are exact decimal strings and cover every
  matching position of that **outlet/product/unit**, before pagination. They
  repeat on each position and **must not be added together**. Catalogue product
  IDs are outlet-owned; identical SKUs or names are not a shared formulation
  identifier. Cross-outlet identity consolidation awaits an authoritative
  shared product linkage; this view never invents one or sums different units.
- **Valuation.** Existing batch `purchase_cost_minor` is the recorded purchase
  cost **per pack**, as on the receiving form. For each product, known purchase
  value is `floor(sum(on_hand_units × known batch pack cost) / pack_size)`.
  All arithmetic is PostgreSQL numeric; rounding happens once per product,
  so splitting a batch between locations cannot change its value. Missing
  cost contributes to `uncosted_units`, not a fabricated zero cost. Recorded
  zero cost is a known zero. Landed cost, freight, tax adjustments, margin and
  sale/NPPA price compliance are not inferred by this report.
- **Combined value.** `X-Pharmacy-Stock-Valuation` is a JSON array, bounded to
  20 currencies, with `{currency, known_purchase_value_minor, uncosted_products}`
  (the latter two exact decimal strings). It sums each product value **once**
  within its currency, and identifies incomplete valuation. Currencies and
  quantities in different units are never combined. An empty filtered result
  returns `[]`, not a fabricated currency/value row. The UI validates both
  metadata headers and refuses to present unconfirmed coverage/value as stock.
- **CSV.** `GET …/stock/positions.csv` uses the same filters, without pagination
  parameters. It exports all matching accessible positions, with outlet scope,
  coverage, product totals and the existing register fields; it refuses over
  1000 positions or 8 MiB, never truncates. Product totals repeat per position.
  Formula-like text is neutralised, downloads are `no-store`/`nosniff`, and
  successful exports log actor, tenant/pharmacy, cutoff, row count and SHA-256
  without cell values. Row count and digest are also response headers.

Canonical additional fields are in `contracts/pharmacy.json` (generated Go/Zod).
Migration `0024_pharmacy` changes the read predicates on four inventory tables
only. Existing single-outlet editor `WITH CHECK` predicates remain intact;
prescription, sales, financial and other private tables gain no overview access.
This implements the operational read aspects of IN-PH-01/03 and ORG-AC-01/02/07/08 explicit
scope/fail-closed principles, not a new stock master, transfer, legal valuation
policy or clinical access path. There are no new events or consumers.

### Hospital affiliation verification (#1416)

A declared hospital link grants nothing until that hospital verifies it. The declaration stays immutable. Each step is a new row in `pharmacy_affiliation_verifications`, and the latest step is the link's `verification_status`: `pending_validation`, `requested`, `verified`, `declined`, `revoked` or `withdrawn`. Same tenant only. A link to another customer's hospital needs a separate data-sharing agreement. See the [design](../../superpowers/specs/2026-10-29-pharmacy-affiliation-verification-design.md).

| Route                                                            | Behaviour                                                                                                                                                                                                                                                                                |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/hospital-affiliations/:id/verification-requests/:request` | `pharmacy.access.manage`. Opens a round (`requested`) and publishes `affiliation_verification_requested.v1` in the same transaction. A retry of the same round is `200`. A new round while one is `requested` or `verified`, or a round id already used, is `409 verification_conflict`. |
| `POST …/verification-requests/:request/withdraw`                 | `pharmacy.access.manage`. Body `{reason}`, 1 to 500 characters. Ends a `requested` or `verified` round and publishes `affiliation_verification_withdrawn.v1`, which carries the request's whole claim. A retry with the same reason is `200`; anything else is `409`.                    |
| `GET …/hospital-affiliations/:id/verifications`                  | Paginated history with actor, reason and the hospital's decision receipt.                                                                                                                                                                                                                |

The hospital decides in patientmaster ([patients](patients-and-clinical.md#pharmacy-affiliation-review-1416)). The `pharmacy-affiliation-decision` consumer applies a decision only to the round that is still open with that id. A decision for a withdrawn or already-decided round is a no-op, so redelivery and reordering converge. A database trigger enforces the transitions and gap-free numbering. Both directions are delivered until confirmed. Patientmaster confirms each request and withdrawal it handles (`pharmacy_affiliation_received.v1`), and pharmacy confirms each decision (`affiliation_decision_received.v1`). Each module's redelivery loop re-sends an unconfirmed step with exponential backoff from 2 minutes up to 60, and never gives up. So a dead-lettered withdrawal or revocation cannot leave the two sides disagreeing. A verified link is the precondition for patientmaster's [pharmacy patient lookup](patients-and-clinical.md#pharmacy-patient-lookup-1416b). Pharmacy also keeps `pharmacy_registered_patients`, the ids-only list of the tenant's registrations built by `pharmacy-patient-registered`. Registrations made before the consumer existed arrive through the #1377 replay. B-2c will accept a patient on a sale only if the id is in this list for a hospital the outlet's pharmacy is verified with.

## Integration requirements

Published catalogue, stock, payable, settlement and safety events mostly carry scoped references only. `dispense_recorded.v1` carries dispense/visit IDs. The current visit-intake consumer still receives patient name from MediCore and opens a pending task automatically; it is not a prescription order.

A clinical dispense contract must require an authorized current prescription/version, patient/hospital linkage, outlet authority, pack/unit conversion, available safe batch quantities, substitution policy and audit. Local stock reservation and dispense posting need durable idempotency and concurrency constraints. Billing uses a stable source-line ID. After physical dispense, billing failure is `charge_pending` recovery; deleting the dispense is not compensation.

Supply transfer requires a source dispatch, destination receipt, quantities/units/batches and immutable discrepancy record; only the receiving outlet owner updates its stock. Bilateral commercial sharing requires current country policy, licences, identity and approved relationship, not common ownership. Payment adapters must distinguish settlement recording from provider-side capture/refund and reconcile ambiguous responses.

## Remaining work and acceptance

Trace requirements to [#1133](https://github.com/tesserix/helivanta/issues/1133), [#159](https://github.com/tesserix/helivanta/issues/159), [#162](https://github.com/tesserix/helivanta/issues/162), [#166](https://github.com/tesserix/helivanta/issues/166), [#167](https://github.com/tesserix/helivanta/issues/167), [#987](https://github.com/tesserix/helivanta/issues/987), [#1060](https://github.com/tesserix/helivanta/issues/1060) and the [reconciled backlog](../../delivery/2026-10-18-pharmaconnect-backlog.md). Do not label these whole stories complete because a foundation exists.

Approve full sales/returns/recalls/expiry release, signed prescription fulfillment, GST/tax policy beyond the tax setup above, price schemes and regulated reporting per country before defining stable external contracts. Required tests include negative/overflow quantity, concurrent receipt/correction, duplicate command with changed content, expired/blocked batch, cross-outlet grants, stale tenant, local midnight expiry, export scope, settlement reversal and consumer retry after commit. Database failure denies a stock/financial mutation; broker lag leaves outbox work pending; a report must expose unavailable data rather than show a false zero.
