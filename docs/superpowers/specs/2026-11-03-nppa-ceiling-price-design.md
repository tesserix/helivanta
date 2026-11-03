# NPPA ceiling prices at the counter (#1371)

Resolves: #1371. Builds on the outlet catalogue, schedule declarations
(#1386 A2) and counter sales (#172, #1386 B).

## Why

For scheduled formulations under the Drugs (Prices Control) Order, 2013, the
NPPA notifies a ceiling price that can be lower than an old pack's printed MRP.
Today the counter prices every line from the batch MRP and every invoice says
`nppa_ceiling_checked: false`; nothing knows a ceiling exists.

## The law, as read (2026-11-03)

From the consolidated DPCO 2013 text (Department of Pharmaceuticals):

- A ceiling price is fixed by the Government for scheduled formulations
  (para 2(1)(d)), per unit of dosage (para 11(2)), excluding taxes (the
  order's forms: "Ceiling Price (in Rs. per unit, excluding taxes)").
- A manufacturer's MRP may not exceed the ceiling price plus local taxes as
  applicable (paras 13(1), 16(4) and 24(1)), now GST; for stock already in the
  market the manufacturer has 45 days to bring its MRP within it.
- Para 26 binds the seller: no person shall sell a formulation to a consumer
  above the price in the current price list or on the label, whichever is
  less.

This is a reading of the order for the product's controls, not legal advice.
Activation for a real outlet needs its compliance reviewer (see below).

## Decisions (product owner, 2026-11-03)

1. **Per outlet.** The outlet's catalogue is not linked to the drug master, so
   each outlet declares a product's price control itself, as it declares its
   schedule class.
2. **Two people.** A declaration applies only once someone other than its
   declarer approves it.
3. **No grace period.** A ceiling applies from its effective date. Whether a
   pharmacy may sell old-label stock above a new ceiling during the
   manufacturer's 45 days is for the compliance reviewer; until they say so,
   it may not.

## What it does

- **Declare.** `PUT …/catalogue/products/:id/price-control/:declaration`
  (`pharmacy.price_control.declare`): either `price_control: not_controlled`
  with a `source` (why it is not price-controlled), or
  `price_control: controlled` with
  `ceiling_minor` (paise per unit of issue, excluding GST), `order_reference`
  (the NPPA order or notification number), `order_date`, and a `source`. Both
  carry `effective_from`. Declarations are immutable; a correction is a new
  declaration. The same id with the same content answers `200`, other content
  `409`.
- **Review.** `POST …/price-control/:declaration/review`
  (`pharmacy.price_control.review`) `{decision: approved|rejected, reason}`, by
  someone other than the declarer (`409 four_eyes`), once, and only while it
  is the product's latest declaration (`409`: a newer one replaced it).
- **Read.** `GET …/price-control` (`pharmacy.catalogue.read`): the
  declaration in force today, the state (`undeclared`, `blocked`, `in_force`),
  a reason when blocked, and the history.
- **In force on a day:** none if the product has no declarations
  (`undeclared`). Otherwise the latest declaration must be approved, and the
  one in force is the approved declaration with the latest `effective_from`
  on or before the day; if either fails, the product is `blocked`. A
  declaration replaces every earlier one that starts on or after its own
  start, so a correction is never overridden by the mistake dated after it.
  Any declaration not yet approved and in force, including "not controlled
  from tomorrow", stops sales of the product until it is.
- **At the counter.** A `blocked` product cannot be sold or quoted (`409
not_sellable`, naming why). For a controlled product, each line's list price
  is the lower of the MRP share and the ceiling plus the line's GST: the most
  whose taxable value is at most `ceiling_minor × units`. The line says
  `price_limit` `mrp` or `ceiling`, and carries `price_declaration_id` and
  `ceiling_minor`. A discount runs on that list price as today.
- **Invoice.** `nppa_ceiling_checked` is true when every line's product had a
  price-control declaration in force (controlled or not); an undeclared
  product sells under its MRP alone and leaves it false.

## How it holds

- Declarations and reviews are append-only, audited, outlet-scoped under
  forced RLS, with the declarer and reviewer pinned to the acting subject and
  a trigger refusing self-review.
- A `BEFORE INSERT` trigger on sale lines reads the declaration in force on the
  sale date and refuses a line that is blocked
  (`pharmacy_sale_line_price_unknown`), names another declaration or ceiling
  (`pharmacy_sale_line_price_source`), or whose taxable value exceeds the
  ceiling × units (`pharmacy_sale_line_ceiling`). A handler bug cannot sell
  above the ceiling.
- The GST arithmetic is the counter's own: the cap is found by searching the
  tax-inclusive amount whose existing tax split stays within the ceiling, so
  the check and the split never disagree.

## Activation review

Required before an outlet relies on this: the responsible pharmacist and the
outlet's compliance reviewer confirm the reading above, the 45-day question,
and the unit a ceiling is entered in for each product (the catalogue's unit of
issue must be the order's unit of dosage).

## Not covered

Importing NPPA notifications; the drug master carrying ceilings for every
outlet; retail prices of new drugs (para 5); trade margins; ceilings on
non-scheduled formulations' annual increase (para 20); refunds of past
overcharges.
