# Payments core

Owner: the `payments` module (#210, #1386 C-4a). Design:
[payments and UPI at the counter](../../superpowers/specs/2026-10-28-payments-upi-counter-design.md).

Payments owns money state: the tenant's provider account, payment intents
and what the provider reported. A requester (the pharmacy domain today)
owns what the money is for and whether it was used. They talk only through
outbox events; neither reads the other's tables.

## Provider account

| Route                               | Behaviour                                                                                                                                                                                                                                                                                                                                                                                       |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /v1/payments/razorpay-account` | `payments.account.manage` (tenant administrators only). Mode, key id, whether each secret is set, who saved it and when, and the webhook path; `webhook_url` only where `PAYMENTS_WEBHOOK_BASE_URL` names a public Global API origin. Never a secret, sealed or not.                                                                                                                            |
| `PUT /v1/payments/razorpay-account` | `{mode, key_id, key_secret?, webhook_secret?}`. Both secrets on the first save; a new key id needs its secret. The keys are checked with Razorpay first (`422 credentials_rejected`, `503 provider_unavailable`), then sealed with OpenBao Transit key `helivanta-payments-credentials`, bound to tenant and account. Without OpenBao: `503`, never plaintext. A concurrent save answers `409`. |

## Intents

| Event in                                     | Effect                                                                                                                                                                                                                                                                                                          |
| -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `helivanta.in.pharmacy.payment_requested.v1` | One intent per request (a redelivery does nothing). No account: `intent_failed{provider_not_configured}`. Otherwise one Razorpay UPI QR (`upi_qr`, `single_use`, `fixed_amount`, `close_by` = expiry; INR, 120–7200 s) with a 10 s timeout and no retry: `intent_opened` or `intent_failed{provider_unavailable | provider_rejected}`. |
| `helivanta.in.pharmacy.payment_cancelled.v1` | Closes the QR, then checks: `intent_paid` if money landed, else `intent_cancelled`. Retried until its request's intent exists.                                                                                                                                                                                  |

An intent is paid only by a payment fetched from the Razorpay API, `captured`,
with the intent's amount and currency. Every captured payment is stored once
(`paid`, `excess` after the first, `mismatch` for another amount); no payer
VPA, name or contact is stored, logged or published.

| Event out                                                         | Payload beyond `requester`, `request_id`, `intent_id`                       |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `helivanta.in.payments.intent_opened.v1`                          | `qr_image_url`, `expires_at`                                                |
| `helivanta.in.payments.intent_failed.v1`                          | `reason`                                                                    |
| `helivanta.in.payments.intent_paid.v1`                            | `amount_minor`, `currency`, `provider_payment_id`, `rrn`, `paid_at`, `late` |
| `helivanta.in.payments.intent_expired.v1`, `…intent_cancelled.v1` | —                                                                           |

## Convergence

- **Webhook** `POST /v1/payments/webhooks/razorpay/:tenant/:account`, outside
  the authenticated chain (`bootstrap.UnauthenticatedRoutes`): 120/min per
  account, 64 KiB body, `X-Razorpay-Signature` HMAC-SHA256 under the
  account's webhook secret, deduplicated by `X-Razorpay-Event-Id`. A verified
  `qr_code.credited` or `qr_code.closed` naming one of the account's QR codes
  runs the same API check as the poller; the body itself settles nothing.
  Unknown tenant, unknown account and bad signature are the same `401`.
- **Poller** (in the API process): checks at +5, +10, +20, +30 s, then every
  30 s; at expiry it closes the QR, then fetches, then decides; late checks at
  +2 and +15 min after closing. It reads due `(tenant_id, id)` pairs across
  tenants (system role, those columns only) and checks each under its
  tenant with the row locked `SKIP LOCKED`. A provider failure retries the
  intent in 30 s.

## Not covered

Refunds, release on unconfirmed payment, collect requests, deep links, cards
and other methods, payment links, settlement reconciliation, a second
provider, webhook secret rotation with overlap. Deployment needs Razorpay QR
Codes enabled, an OpenBao policy for `helivanta-payments-*`, and public
ingress to the webhook path for webhooks to arrive.
