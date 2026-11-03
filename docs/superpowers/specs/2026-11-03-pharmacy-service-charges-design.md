# Service charges on pharmacy bills (#1388)

Resolves: #1388. Builds on counter sales (#172, #1386 B), admission sales
(#1387), credit notes (#1386 C-1), the daily report (#1060) and the reviewed
declaration pattern of NPPA ceiling prices (#1371).

## Why

A hospital pharmacy adds charges to a medicine bill: delivery to a ward or
room, a handling fee at the counter. Medicines sell at or below batch MRP
(#172) and, when controlled, within the NPPA ceiling (#1371). A charge folded
into a medicine's price would breach those checks and misstate the line's
tax, so a charge has to be its own line on the invoice.

## The law, as read (2026-11-03)

CGST Act, 2017 (CBIC text):

- s.2(30): a composite supply is two or more supplies naturally bundled and
  supplied together in the ordinary course of business, one of which is the
  principal supply. Its own illustration: goods packed and transported with
  insurance, where the goods are the principal supply.
- s.2(90): the principal supply is the predominant element of a composite
  supply, to which the others are ancillary.
- s.8(a): a composite supply is treated as a supply of its principal supply.
- s.8(b): a mixed supply is treated as the supply attracting the highest rate.
- s.15(2)(c): the value of supply includes incidental expenses, including
  commission and packing, charged by the supplier in respect of the supply.

So a delivery or handling fee charged by the pharmacy on its own medicine
sale is probably taxed as part of the medicines, at their rate, not at a
service rate of its own. No CBIC circular on delivery charged by a seller of
goods was found. **Which treatment applies to each charge is the tenant's
accountant's decision.** The software records it, has it reviewed, and never
picks a rate itself.

## Decisions (product owner, 2026-11-03)

1. Each charge version records its tax treatment, reviewed by a second
   person:
   - `own_service`: its own SAC, supply nature and rate, for a service the
     accountant confirms is not bundled with the sale;
   - `principal_supply`: taxed with the medicines on the invoice, at their
     rate. If the medicines on one invoice do not share one supply nature and
     rate, the charge is refused (fail closed) until the accountant gives a
     rule for splitting it.
2. Fixed amounts only. No percentage, slab or distance rules.
3. Channels: `counter` (a counter sale) and `ward_delivery` (a sale billed
   to an admission, #1387). Home delivery waits for #161.
4. The cashier adds configured charges to a sale; a charge never touches a
   medicine line's price, discount, MRP or ceiling check. The invoice records
   the charge version applied. A charge is reversed only by a credit note.
5. The daily report shows medicine and service-charge revenue apart.

## What it does

### Configuration

- A **charge** is an outlet's code (`^[A-Z][A-Z0-9_]{1,19}$`, unique per
  outlet, never changes). Its terms live in **versions**, append-only:
  - `name` (1–80 characters, printed on the invoice);
  - `channel`: `counter` or `ward_delivery`;
  - `amount_minor`: what the buyer pays, tax inclusive, 1 to 100,000,000
    minor units. Tax is inside it, as for a medicine line (spec B4 of #1386);
  - `available`: false withdraws the charge from that date;
  - `tax_treatment`: `own_service` or `principal_supply`;
  - for `own_service` only: `sac_code` (`^99[0-9]{4}$`), `supply_nature`
    (`taxable`, `nil_rated`, `exempt`), `rate_ppm` (multiple of 100;
    100–1,000,000 when taxable, else 0) and, for `exempt`, an
    `exemption_basis`. For `principal_supply` all four are absent;
  - `tax_basis`: 5–500 characters, who decided the treatment and on what
    authority (the accountant's note, the circular). Required for both;
  - `effective_from`: a date.
- A version is reviewed by someone other than its declarer: approved, or
  rejected with a reason. Only the latest version of a charge may be
  reviewed, once. Declaring and reviewing serialise per charge.
- **In force on a day** is the approved version with the latest
  `effective_from` on or before the day that is not superseded by a newer
  approved version (higher `seq`) whose `effective_from` is on or before its
  own — the #1371 rule. If the charge's latest version is pending or
  rejected, nothing is in force: a pending change, including a withdrawal,
  stops the charge at once (fail closed: the counter never charges terms a
  second person has not seen). Withdrawal is therefore immediate on
  declaration and stays withdrawn when approved.
- Permissions: `pharmacy.service_charge.declare` and
  `pharmacy.service_charge.review`, both granted to the pharmacy owner (the
  outlet's manager role). Approval therefore needs two different people with
  those permissions; a tenant administrator can grant the second.
- An outlet holds at most 50 charges, so the counter reads them in one bounded
  list. Charges are never deleted and codes never reused: a withdrawn or
  mistaken charge keeps its place, and reaching 50 needs a later slice that
  retires charges. Reading charges takes `pharmacy.sale.read`, so the
  counter can list them.

### At the counter

- A quote takes `charges: [{charge_id}]`, at most 5, distinct; a sale takes
  `charges: [{charge_id, version_id}]`, the version the quote priced, and is
  refused when another version is in force by then.
  The sale's channel is `ward_delivery` when it is billed to an admission,
  else `counter`; a quote names its `channel` (default `counter`).
- Each charge becomes a charge line numbered 1–5, apart from the medicine
  lines, copying from the version in force on the sale date: version id,
  code, name, treatment, SAC, supply nature, rate and amount. Tax is the
  same tax-inside split as a medicine line, and the schema checks the
  arithmetic.
- For `principal_supply`, the line takes the supply nature and rate of the
  medicine lines; its SAC is empty and the invoice says it is supplied with
  and taxed as the medicines. At commit every medicine line on the sale must
  have that same nature and rate.
- The invoice total, taxable value and tax halves are the medicine lines plus
  the charge lines; tenders pay that total. A sale still needs at least one
  medicine line: a charge alone is not a sale.
- A charge whose version changed between quote and sale, that is not in
  force, is withdrawn, or is for the other channel is refused with 409
  `not_sellable`, asking the cashier to price again.
- `SalePosted` and `AdmissionChargePosted` keep their shapes; their totals
  include the charges, which is what was billed.

### Credit notes

- A credit note takes `charges: [{sale_charge}]` beside `lines`; it needs at
  least one of either. A charge line is credited whole, once, at exactly its
  amount, taxable value and tax. No stock moves for it.
- The credit note's totals are its medicine lines plus its charge lines and
  are refunded as now (to the admission account for an admitted sale).

### Report

- Each day of the daily report adds `service_charges_minor`,
  `service_charges_taxable_minor`, `service_charge_credits_minor` and
  `service_charge_credits_taxable_minor`. Sales and credit totals stay the
  invoice totals; medicine revenue is the difference, which the screen shows
  as its own row.

## How it holds

- Configuration rows are insert-only with audit, forced RLS by outlet, and
  checked by the schema: the treatment's fields present exactly when it is
  `own_service`, the exemption basis exactly when exempt.
- A trigger refuses a review by the declarer, of a version that is not the
  charge's latest, or twice.
- A trigger on each charge line checks it against
  `pharmacy_service_charge_in_force` for the sale date and the sale's channel,
  and that every copied value equals the version's.
- Deferred at commit: the sale balance includes charge lines; a
  principal-supply charge matches every medicine line's nature and rate; the
  credit-note balance includes credited charge lines.
- Charge lines and credited charge lines are sealed to their document's
  posting transaction, like medicine lines.

## Activation review

Before an outlet configures a charge, its accountant decides each charge's
treatment and records the basis. Open for compliance review, not decided
here: whether a separately stated delivery fee on a medicine sold at MRP is
allowed at all, given MRP is printed inclusive of all taxes under the Legal
Metrology (Packaged Commodities) Rules and DPCO 2013 para 26. Until that is
answered the feature is inert: no outlet has a charge until one is declared
and approved.

## Not covered

- Home delivery and delivery addresses (#161).
- Percentage, slab, distance or time-of-day rules; automatic charges.
- Splitting a principal-supply charge across medicines with different rates.
- Room and bed tariffs (MediCore #1047); hospital billing charges (#1138).
- GSTR exports and the HSN/SAC summary.
