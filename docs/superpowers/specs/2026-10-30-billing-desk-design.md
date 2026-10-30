# The billing desk (#197–#199 follow-up)

Resolves the gap left by #197, #198 and #199: every bill, payment, deposit and refund action
was reached from a bed on the ward board, which lists occupied beds only. A released patient,
whose final payment or deposit refund is due at exactly that moment, could not be settled
from the UI.

## What it does

- **Accounts list.** `GET /billing/hospitals/:hospital/accounts?view=` (paged, newest first):
  each admission account with its status, whether discharge is ordered, and its INR position:
  balance due (below zero is credit), deposits held, and what can be refunded now (the surplus
  over the bill, once a final bill exists). Views: `to_collect`, `to_refund`,
  `discharge_ordered`, `open` (in the ward), `released`, or all. Needs `billing.account.read`
  and the hospital's billing grant; every figure comes from the same database functions the
  money rules use.
- **A Billing zone** with one page, the billing desk: a hospital at a time, the cash desk, and
  the accounts list, each account opening the existing bills, payments, deposits and refunds
  panel. Its own zone because the billing desk (receptionists) holds no MediCore clinical read.
- **The ward board** keeps each occupied bed's balance line and bills; the cash desk moves to
  the billing desk.

## Calls I made (builder's)

- Default view **To collect**: the desk's first job.
- The patient's name comes from the existing patient lookup, one audited read per row (a
  batched lookup is #1378).

## Not covered

Search by patient or MRN; sorting by amount; a printed statement; the daily collection
report (#198).
