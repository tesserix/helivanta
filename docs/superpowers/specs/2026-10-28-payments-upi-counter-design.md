# Payments core and UPI at the pharmacy counter (#1386 C-4)

Resolves the first part of #210 (payment orchestration core) and #211 (UPI),
for the PharmaConnect counter of epic #1386. Decisions taken with the product
owner on 2026-10-28:

- The provider is **Razorpay**.
- The first slice is the payments core plus UPI at the pharmacy counter, and
  payment status comes from Razorpay, never from what the payer shows the
  cashier.
- A sale paid by gateway UPI does not post until Razorpay confirms the payment.
  The request times out, and the cashier can then take another tender.

## Why

A UPI tender today is a reference the cashier types in. Nothing checks that the
money arrived, so a screenshot of a "success" screen counts as payment, and the
daily report (C-3) adds up claims, not collections. A counter in India that
takes UPI needs a payment the system itself saw settle.

## Shape

C-4 ships as two slices, each one PR, each backend + frontend + migration +
tests.

- **C-4a — payments core.** A new `payments` module: the tenant's Razorpay
  account, payment intents, UPI dynamic QR, signed webhook ingestion, a
  reconciling poller and outcome events. PharmaConnect gains _counter payment
  requests_: it asks for a payment and records the outcome. UI: the Razorpay
  account setup page, and nothing at the counter yet.
- **C-4b — the counter.** The cashier's UPI QR panel, and a sale tender that
  references a paid counter payment request.

### Ownership and the event flow

Modules never import or read each other's tables. Payments owns money state
(accounts, intents, provider payments, webhook deliveries). A requester —
PharmaConnect now, hospital billing later — owns _what the money is for_
and whether it was used. They talk by events through the outbox:

```
pharmacy  ── payment_requested.v1 ──▶ payments   (creates intent + Razorpay QR)
pharmacy  ◀── intent_opened.v1 / intent_failed.v1 ── payments
pharmacy  ── payment_cancelled.v1 ──▶ payments   (closes the QR, then checks)
pharmacy  ◀── intent_paid.v1 / intent_expired.v1 / intent_cancelled.v1 ── payments
```

This departs from #210's `POST /v1/payments/intents`. The departure is
deliberate: a cashier's authority is an outlet grant that only PharmaConnect
holds. If payments served cashiers directly, it would need either a copy of
those grants or a read of pharmacy tables, and both are banned. The requester
authorises its own user; payments authorises nobody at the counter.

The pharmacy's request event is for the whole pharmacy domain, not only
the counter. Its `purpose` says what the money is for. `counter_sale` is
the first purpose, and later pharmacy flows (home delivery, credit
settlement) add purposes, not new subjects.

The request event carries:

- the requester's request id (the idempotency key: one intent per
  `(tenant, requester, request id)`);
- amount in minor units and currency;
- `expires_in_seconds`;
- a short description with no patient data.

The outcome events carry the request id, the intent id and, as applicable:

- the QR image URL and expiry (`opened`);
- a reason code (`failed`: `provider_not_configured`, `provider_unavailable`,
  `provider_rejected`);
- the amount, currency, Razorpay payment id, the UPI RRN and when it was paid
  (`paid`). `late` is true when the payment landed after the intent had been
  reported expired or cancelled.

No VPA, payer name or contact goes into an event, a log or a table:
payments does not keep payer identity.

### Payments module (C-4a)

**Razorpay account.**

- One account per tenant, holding: mode (`test`/`live`, as the admin
  declares it), key id, key secret and webhook secret.
- The key secret and webhook secret are encrypted with OpenBao Transit. The key
  is `helivanta-payments-credentials`, and the derivation context is
  `tenant id + account id`, so a ciphertext is useless under another tenant.
- Secrets are write-only: no response returns them, only "set" and when.
- Saving first calls Razorpay with the keys (a one-item QR list). Keys Razorpay
  rejects are not saved: `422 credentials_rejected`.
- Without OpenBao, setup answers `503`, matching the other Transit users. It
  never falls back to storing plaintext.
- Tenant administrators only (`payments.account.manage`, with no role grant).
- Every save is audited, recording who saved and what changed. Secret values
  are never part of the audit.
- The setup page shows the tenant's webhook URL to paste into the Razorpay
  dashboard when `PAYMENTS_WEBHOOK_BASE_URL` names the public Global API
  origin. The shell's own `/api` proxy requires a session, so Razorpay cannot
  deliver through it; where no origin is set, the page says webhooks are not
  exposed and the poller alone confirms payments.

**Intent lifecycle.**

- An intent moves `opening → awaiting_payment → paid | expired | cancelled`, or
  `opening → failed`.
- Each transition is an insert-only row in `payment_intent_transitions`, with
  its cause: request, provider, webhook, poll or cancel.
- An expired or cancelled intent can still become paid (late). That is the one
  edge out of a terminal state, and it is recorded.
- Razorpay QR: `type=upi_qr`, `usage=single_use`, `fixed_amount=true`,
  `payment_amount` = amount, `close_by` = now + `expires_in_seconds`. The
  counter uses 300 seconds; Razorpay requires between 120 and 7200. `notes`
  carries only the intent id.
- One attempt with a 10 s timeout. Any failure is reported at once as
  `intent_failed`, so the cashier is never left waiting on retries.
- A consumer redelivery after Razorpay already made a QR can leave a second,
  unseen QR. Nobody holds its image, and it closes itself at `close_by`. This
  is recorded as a limit, not hidden.

**What counts as paid.**

- A Razorpay payment counts only if all of these hold:
  - it was fetched from the Razorpay API (`GET /v1/payments/qr_codes/{id}/payments`);
  - its status is `captured`;
  - its amount and currency equal the intent's.
- Every captured payment is stored once, keyed by its Razorpay payment id. A
  second captured payment on the same intent is stored and flagged `excess`;
  it never re-pays the intent.
- A captured payment whose amount doesn't match is stored as `mismatch` and
  pays nothing.
- Refunds of excess money are not covered (see below).

**Webhooks are a nudge, not a source.**

- The route is `POST /v1/payments/webhooks/razorpay/:tenant/:account`. It is
  unauthenticated, listed in `bootstrap.UnauthenticatedRoutes`, and served via
  the Global API owner `payments`.
- The body is capped at 64 KiB. The handler checks `X-Razorpay-Signature`
  (HMAC-SHA256 of the raw body, keyed by that account's webhook secret, compared
  in constant time).
- Deliveries are deduplicated by `X-Razorpay-Event-Id` per account.
- A verified `qr_code.credited` or `qr_code.closed` that names a QR we
  issued triggers the same API check the poller runs. (`payment.captured`
  does not name the QR, so it is acknowledged and left to the poller.) The payload
  never settles anything by itself, so a malformed or replayed body can't
  either.
- An unknown tenant, unknown account or bad signature all get the same `401`
  with no detail. A verified but irrelevant event gets `200`.
- The webhook secret is per account. Secret rotation that accepts the previous
  secret is not covered.

**Reconciling poller.**

- Webhooks can't reach a laptop and can be lost in production, so a poller in
  the API process converges every intent.
- Checks fall at 5, 10, 20 and 30 s after opening, then every 30 s until
  expiry.
- At expiry the poller closes the QR, then fetches its payments, and only then
  decides `paid` or `expired`. The close comes first so that no payment can
  land after the decision.
- After expiry there are late checks at +2 min and +15 min, then the intent is
  left alone.
- Finding due intents means reading across tenants: only `(tenant_id,
intent_id)` pairs, through `WithAllTenants`. This is a reviewed entry in the
  archtest allowlist.
- Each check then runs under `WithTenant`, holding the intent row
  `FOR UPDATE SKIP LOCKED`, so two replicas never check the same intent at once.
- The decision not to use Temporal: the repo's Temporal workers are separate
  binaries that aren't deployed yet (#1406). A counter must converge wherever
  the API runs.

**Global API.** Add the owner `"payments": "platform"`.

**Permissions.**

- `payments.account.manage` (administrators only).
- No other human-facing route exists in C-4a.

### PharmaConnect counter payment requests (C-4a)

- `POST /pharmacy/pharmacies/:pharmacy/outlets/:outlet/counter-payments`:
  - needs an outlet editor and `pharmacy.sale.create`;
  - body: `{request_key, amount_minor}`, with currency from the outlet;
  - inserts a request in `requested` and publishes
    `payment_requested.v1` in the same transaction;
  - `request_key` is unique per outlet, so a retried click returns the same
    request (`200`) instead of making a second one.
- `GET …/counter-payments/:request` returns its state for the counter:
  `requested | awaiting_payment | paid | failed | expired | cancelled`, plus
  the QR image URL, expiry, amount, failure reason and the `late` flag.
- `POST …/counter-payments/:request/cancel`:
  - allowed only while `requested` or `awaiting_payment`;
  - status-guarded, otherwise `409`;
  - moves the request to `cancelling` and publishes `payment_cancelled.v1`.
- PharmaConnect consumes the five outcome events and moves the request with
  status-guarded updates, so a redelivered event changes nothing.
- A request that is `paid` stays paid. A late `paid` on an expired or
  cancelled request sets it to `paid` with `late=true`, so the money is visible
  and not lost.
- Using a paid request on a sale is C-4b.

### Counter (C-4b)

- The cashier picks **UPI (verified)**. The page creates the counter payment
  for the UPI part of the bill and shows the QR. The image comes from Razorpay,
  so the CSP `img-src` gains `https://rzp.io https://*.razorpay.com`.
- The page polls the request every 2 s through `useApiQuery`. It shows "Waiting
  for payment", a countdown, "Paid ₹…", "Expired — take another tender" and
  "Couldn't start UPI — take another tender". It never shows paid on anything
  but `paid`.
- A tender of kind `upi_verified` names the counter payment. Posting checks
  all of these:
  - the request is `paid`;
  - same outlet;
  - amount equals the tender amount;
  - not used by another sale (unique).

  Otherwise posting fails with `409`, and nothing posts.

- The existing typed-reference `upi` tender stays, labelled "UPI (reference
  only)". Many outlets take UPI on their own static QR, and removing it would
  stop them selling. Reports show the two separately.
- A paid request whose sale then fails to post (stock moved, say) stays paid
  and unused. The counter lists it and lets the cashier put it on a sale of the
  same UPI amount.

## Not covered (C-4)

- Refunds through Razorpay, including for excess, late or unused payments.
  Finance refunds those in the Razorpay dashboard, and recording that is a later
  slice. A credit note on a verified-UPI sale still refunds by the existing
  kinds.
- Releasing a patient on an unconfirmed payment (#211's override), collect
  requests to a VPA, `upi://` deep links, cards, netbanking, wallets and payment
  links (#212–#216).
- Settlement and bank reconciliation (#217).
- A second provider or country, beyond keeping provider calls behind one Go
  interface that names no call site's provider.
- Webhook secret rotation with overlap, circuit breakers, and an
  OpenTelemetry span chain.
- Billing, MediConnect and subscription requesters.

## Prerequisites outside this repo

- Razorpay must enable QR Codes on the merchant account; it is an on-demand
  feature. The tenant admin pastes the webhook URL into the Razorpay dashboard.
- OpenBao needs a Transit key and a policy for `helivanta-payments-*`
  (tesserix-k8s). Public ingress must reach `/v1/payments/webhooks/` on the
  Global API.
- Until then, setup answers `503` in a deployment, and locally the poller is
  how payments converge.
