# UPI at the hospital billing desk (#198 slice 3, #211)

Resolves: the billing-desk part of #211 (UPI) and the UPI mode of #198 (cashier
management). Builds on cash counters (#198 slice 1), the daily collection report
(#198 slice 2), deposits and refunds (#199), and the payments core with UPI at the
pharmacy counter (#1386 C-4a/C-4b, spec `2026-10-28-payments-upi-counter-design.md`).

Decisions taken with the product owner on 2026-11-01:

1. **Verified UPI only.** The billing desk has no typed-reference UPI. An unchecked
   reference is the fraud surface #198 exists to close, and billing has no outlets
   already relying on a static QR.
2. **A confirmed payment becomes a receipt by itself.** While a QR is open, its amount
   is held against the balance due. A shift cannot close while one of its QRs waits for
   an answer. A payment that lands after its QR expired or was cancelled is recorded
   anyway: the money is real.
3. **Not in this slice:** card, netbanking, wallets; refunds over UPI; handover,
   denominations and mid-shift pickups.

## Why

The billing desk takes cash only. A patient's relative who pays by phone has no way to
pay, and a desk that takes UPI on its own printed QR leaves no record a system has
checked. The payments module already confirms UPI with Razorpay for the pharmacy; the
desk should take UPI the same way, with every rupee on a numbered receipt in the
cashier's shift.

## What it does

- **Asking for UPI.** A cashier with an open shift asks for a UPI payment or deposit on
  an admission's account: `PUT /billing/hospitals/:hospital/admissions/:admission/upi-requests/:id`,
  body `{purpose: "payment"|"deposit", amount_minor}`, INR. The id is the client's
  UUIDv7; the same id with the same body replays (`200`), with another body is `409`.
  - A payment asks for no more than the balance due less UPI payments already waiting
    (`409 over_balance`). A deposit needs an open account, as a cash deposit does.
  - Billing records the request and publishes `helivanta.in.billing.payment_requested.v1`
    in the same transaction. Payments makes a Razorpay single-use QR for 300 seconds and
    answers with its intent events, as for the pharmacy.
- **Following it.** `GET …/upi-requests/:id` returns its state for the desk:
  `requested | awaiting_payment | cancelling | paid | failed | expired | cancelled`, the
  QR image URL and expiry, the failure reason, and once paid the Razorpay payment id,
  UPI RRN, paid time and whether it was late. An admission's requests are listed newest
  first, so a desk that reloads finds a QR still waiting.
- **Cancelling.** The cashier who asked cancels a `requested` or `awaiting_payment`
  request (`POST …/upi-requests/:id/cancel`). It waits in `cancelling` for payments'
  answer: cancelled, or paid if the money landed first.
- **Paid makes the receipt.** When payments reports the intent paid for the request's
  amount and currency, the request becomes `paid` and, in the same transaction, a
  receipt is recorded in the requesting shift: mode `upi`, purpose as asked, numbered
  next on the counter like cash, with the request's cashier. Its id is the request's id,
  so one payment is one receipt. The database writes the receipt, not the handler.
- **Late money.** A payment reported after the request was told expired or cancelled
  still makes its receipt, marked late, in the shift that asked, even if that shift has
  since closed. A late payment can take the account past what it owes; the account shows
  the surplus as credit.
- **The drawer.** UPI never enters the drawer: expected cash counts cash receipts only,
  a UPI receipt cannot be voided (the money is at the bank; voiding is handing cash back),
  and a refund's drawer check counts cash only.
- **Closing.** A shift does not close while one of its requests is `requested`,
  `awaiting_payment` or `cancelling` (`409 upi_waiting`), unless that request is more
  than 15 minutes old: payments fails a request with no QR after 45 seconds and decides
  an open QR at its 300-second expiry, so a request still unanswered at 15 minutes means
  the event path is down, and the dead-letter alert is the control, not a cashier who
  cannot go home.
- **The account** shows, per currency, what UPI payments are waiting (`upi_waiting_minor`).
  Cash payments and deposit applications are capped at the balance due less that amount,
  so the same rupees are not paid twice while a QR is open.
- **The daily collection report** keeps its cash columns cash-only and adds UPI payments
  and UPI deposits per shift and in the totals, and in the CSV. The net is cash; UPI is
  reported beside it. A late UPI receipt can change the UPI line of a past day's closed
  shift; its cash figures never change.

## Ownership and events

Payments owns money state; billing owns what the money is for and its receipt. They talk
only through the outbox, as the pharmacy does:

```
billing  ── payment_requested.v1 / payment_cancelled.v1 ──▶ payments
billing  ◀── intent_opened / failed / paid / expired / cancelled ── payments
```

- New contract package `billing/contract` with the two subjects and their data:
  request id, purpose (`admission_payment`, `admission_deposit`), amount, currency,
  `expires_in_seconds`; the cancel carries the amount so it can stand in for a request it
  overtakes. No patient, admission or hospital data goes on the wire.
- Payments gains requester `billing` (migration `0002_payments` widens the requester
  check) and two consumers for billing's subjects, sharing the pharmacy's code path.
- Billing consumes the five outcome events, ignores other requesters' outcomes, and
  moves the request with status-guarded updates, so a redelivered or reordered event
  changes nothing. Paid wins over every other status and is never undone; an outcome for
  a different amount or currency pays nothing.

## Rules enforced in the database (migration `0011_billing`)

- A request is asked by the acting cashier in their own open shift at that hospital, for
  an admission there; its identity, amount and purpose never change.
- A payment request fits the balance due less waiting UPI payments, under the admission
  lock that every charge, receipt and application takes. A cash payment and a deposit
  application are bounded the same way.
- Only the cashier who asked moves a request to `cancelling`; every other change comes
  from billing's outcome consumers (`billing_consumer()`), only along the allowed edges.
- A `paid` request has its receipt, inserted by the transition's trigger. A UPI receipt
  names its paid request, matches it in shift, counter, cashier, admission, purpose,
  amount and currency, is written only by a billing consumer, and is unique per request.
  A cash receipt names no request.
- Expected cash, the refund drawer check and voids count cash receipts only.
- Append-only, forced row security by the billing grant (consumers by
  `billing_consumer()`), audited.

## Permissions

`billing.cash.collect` asks, follows and cancels, as for cash. Listing an admission's
requests is `billing.account.read`. No new permission.

## Not covered

- Card, netbanking, wallets (since added: `2026-11-02-card-netbanking-wallet-design.md`), payment links and collect requests to a VPA.
- Refunds over UPI, including for a late payment's surplus credit. Finance refunds those
  in the Razorpay dashboard; recording such a refund in billing is a later slice (as
  #1459 did for the pharmacy). A credit balance is not refundable from the drawer by
  this slice either.
- Handover between cashiers, denominations, mid-shift pickups, GL export and events such
  as `ShiftClosed`.
- UPI from MediConnect, OPD, lab or emergency.
- Releasing a patient on an unconfirmed payment.
- A request event dead-lettered by repeated database failures never reaches payments;
  its billing request stays `requested`. The desk treats a request with no QR after a
  minute as failed, and it stops blocking the shift's close at 15 minutes.

## Prerequisites outside this repo

The same as the pharmacy's: a Razorpay account with QR Codes enabled, set up by a tenant
administrator; OpenBao Transit for its secrets; the webhook URL on public ingress (or the
poller alone confirms payments).
