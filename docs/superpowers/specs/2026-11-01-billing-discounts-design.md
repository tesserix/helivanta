# Discounts on room charges (#200 slice 1)

Resolves: slice 1 of #200 (Discounts). Builds on admission accounts and bed days (#197),
cash and deposits (#198, #199), GST on rooms, tax invoices and credit notes (#193 slices 1–2).

Decisions taken by the product owner on 2026-11-01:

1. **No discount on a taxable room charge.** Whether a concession that brings a room under
   ₹5,000 a day takes it out of the 04/2022-CT(Rate) proviso is not settled by the sources
   used for the GST slice. Slice 1 refuses taxable charges; exempt rooms work fully. A tax
   ruling decides the next step.
2. **Limits per reason, at two levels.** Each reason carries what the desk may give alone and
   the most anyone may approve, each as a percentage and an amount. Payer-class and service
   dimensions wait for payer billing and other services: today every patient is self-pay and
   room days are the hospital's only charges.
3. **Not in slice 1:** the leakage report, `Discount*` events (nothing consumes them), package
   and payer-rate floors (neither exists), and deposit-requirement waivers.

## Why

A hospital cannot give a concession without editing a price, which the platform does not allow
and must not. Concessions are daily (staff and dependants, charity, service recovery) and
uncontrolled concession is the commonest revenue leak; NABH and audit expect each to be
authorised, reasoned and traceable.

## Reference

ICTHospital `5f6d251cd41f08da8fbada3c5ac98495337ee984` (`CashController`, payment migration):
one invoice-level discount, a free string, prorated to lines by gross share and rounded per
line, with no reason, limit or approver. Adopted: proportional allocation to lines. Not
adopted: per-line rounding (loses paise), free-form unattributed amounts.

## What it does

- **Reasons.** A tenant administrator (`billing.discount.manage`) sets a hospital's reasons:
  a code (`^[a-z0-9_-]{1,32}$`, unique among the hospital's active reasons), a name, and four
  limits in INR: `desk_percent_bp`, `desk_max_minor`, `approval_percent_bp`,
  `approval_max_minor` (basis points 0–10000, amounts 0–1000000000), the desk's never above the
  approval's. A reason is fixed once set; it is retired once, and a changed policy is a new
  reason. `PUT …/discount-reasons/:id` (UUIDv7; same again `200`, different `409`),
  `POST …/discount-reasons/:id/retire`, `GET …/discount-reasons` (paged, newest first,
  `billing.account.read`).
- **Asking.** The desk (`billing.bill.issue`) asks for a discount on an admission:
  `PUT …/admissions/:admission/discounts/:id` `{reason_id, amount_minor, justification}`,
  INR, justification 1–500 characters. The database decides, under the admission's lock:
  - **Within the desk limit** it is `applied` at once.
  - **Within the approval limit** it is `pending`.
  - **Beyond it**, `409 discount_over_limit` and nothing is recorded.
  - Limits are cumulative per reason and admission: the reason's net discount already applied
    on standing charges plus this one must be within the amount, and within the percentage of
    the **base**, the gross of the admission's standing untaxed room charges.
  - The amount must fit the **room** (`409 nothing_to_discount`): the sum, over room charges
    that are standing, untaxed, INR and on no tax invoice, of what each still carries after
    its discounts. It must also fit what is payable (`409 discount_over_balance`), so a
    discount never leaves a credit that only a deposit refund could return.
  - A retired or unknown reason is `409 reason_retired` / `404`. Same id again replays (`200`),
    different content is `409`.
- **Approving.** A tenant administrator (`billing.discount.approve`) decides a pending
  discount: `PUT …/discounts/:id/decision` `{approve, note}`, note 1–500 characters. Never the
  requester (`409 own_request`, also enforced in the database). Approval re-checks the
  approval limit, the room and the balance as they stand then, and applies the discount in the
  same transaction; rejection records the note. A decided discount is final
  (`409 already_decided`; the same decision again replays). `GET …/pending-discounts` lists a
  hospital's pending discounts for approvers.
- **Applying.** The database splits the amount over the eligible charges in proportion to what
  each still carries, flooring each share and giving the leftover paise to the largest
  remainders (then earlier day, then id), so the lines sum exactly to the amount. Each share
  is an account entry: source `discount`, kind `reversal`, against the room charge, document
  `Discount, bed day YYYY-MM-DD`, carrying the discount's id. Balances, bills, positions,
  deposits and refunds see it as they see any reversal. Entries for a discount are written only
  by the transaction that applies it, and at commit they sum to it.
- **A voided day.** When the census voids a discounted day, it first withdraws the day's
  discount lines (source `discount`, kind `charge`, same reference, document
  `Discount withdrawn YYYY-MM-DD`), then reverses the day in full, as before. The day nets to
  zero and both the concession and its withdrawal stay on record. Reversals of a charge,
  including standing discounts, never exceed it.
- **The tax invoice** (a bill of supply line for an exempt room) shows each room line's
  `discount_minor` and its value after the discount (CGST Rule 46(1)(i): value taking into
  account discount). A fully conceded day has value zero; invoice and credit-note line values
  may now be zero. A charge already on an invoice is not discountable: that path is a credit
  note, out of this slice.
- **Attribution.** Each discount keeps reason, amount, justification, requester and time,
  decider, decision, note and time. `GET …/admissions/:admission/discounts` lists them, paged,
  with status and the amount applied.
- **The shell.** The Billing page gets a Discount reasons panel for administrators. Each
  admission on the desk gets "Give a discount" (reason, amount, justification, the most it
  may take) and the list of its discounts; approvers see Approve and Reject on pending ones and
  a list of a hospital's pending discounts. Bill lines label discounts and withdrawals; the
  account summary shows discounts.

## Database rules (fail closed)

- `billing_discount_reasons`, `billing_discounts`: forced RLS under the hospital grant,
  recorded by the acting subject, audited, never deleted. A reason changes only by retiring
  once; a discount only from `pending` to `applied` or `rejected`, once, by someone other than
  its requester.
- The status of a new discount is set by the insert trigger, never by the caller.
- `billing_account_entries` gains `discount_id`; `source_module='discount'` exactly when it is
  set. A desk insert policy admits only `discount` entries, and a trigger admits a discount
  reversal only in the applying transaction of an applied discount, against an eligible charge,
  and a withdrawal only for a voided day's standing discount line, by billing's consumers.
- `billing_reversal_bounded` bounds a room charge by its bed reversals plus its standing
  discount lines.

## Not covered

Taxable room charges (tax ruling), pharmacy (it discounts its own sale on its own invoice),
payer classes, packages and rate-card floors (#196, #1045), credit notes for discounts on
invoiced charges, withdrawing a pending request (an approver rejects it), the leakage report,
`Discount*` events, deposit-requirement waivers, and line-level choice of which charge to
discount (the split is always proportional).

Finance review before production: limit defaults per reason, and whether concessions on exempt
rooms need any further disclosure on the bill of supply.
