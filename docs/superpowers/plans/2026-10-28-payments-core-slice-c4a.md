# Payments core (#1386 C-4a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** A `payments` module that turns a requester's event into a Razorpay
UPI QR. It learns the outcome only from the Razorpay API, with webhooks and a
poller as triggers, and reports it back by events. PharmaConnect counter
payment requests are the first requester.

**Architecture:**

- Requester and payments talk only through outbox events.
- Payments holds the tenant's Razorpay account. Its secrets are sealed by
  OpenBao Transit.
- One `check` function decides paid, expired or cancelled from a Razorpay
  fetch. The webhook, the poller and cancel all call it.

**Tech Stack:** Go (gin, gorm, Postgres RLS), the outbox bus, OpenBao Transit,
Next.js shell with `@helivanta/api`.

**Spec:** `docs/superpowers/specs/2026-10-28-payments-upi-counter-design.md`

## Global Constraints

- Modules never import each other. Only `contract` packages cross.
- Each subject is a constant in its publisher's contract package.
- Every table:
  - is tenant-scoped with forced RLS;
  - is insert-only, except for the status columns, which change only through
    status-guarded updates;
  - has an audit trigger where the module already uses one.
- No VPA, payer name or contact is stored, logged or published.
- Secrets are never returned, logged or audited in plaintext. No OpenBao
  means `503`.
- Expiry for the counter is 300 s. A Razorpay call has a 10 s timeout and one
  attempt.
- Poll schedule:
  - +5 s, +10 s, +20 s and +30 s after opening, then every 30 s until expiry;
  - at expiry, close the QR and then fetch;
  - late checks at +2 min and +15 min after expiry.
- The webhook body is capped at 64 KiB, and the signature is HMAC-SHA256 over
  the raw body. Unknown tenant, unknown account and bad signature all get the
  same `401`.

## Review Focus

- **The same request event delivered twice:** one intent and one outcome
  event. Covered in Task 3.
- **A webhook for a QR of another tenant or account:** never touches it, and
  gets `401`. Covered in Task 4.
- **A payment whose amount or currency differs from the intent:** stored as
  `mismatch`, and the intent is not paid. Covered in Task 3.
- **A payment found after expiry or cancel:** `paid` with `late=true`, and
  exactly one `intent_paid` event. Covered in Tasks 3 and 5.
- **Pharmacy receiving an outcome twice, or out of order:** status guards hold,
  and `paid` is never overwritten. Covered in Task 5.

---

### Task 1: Module, contracts, schema

**Files:**

- `internal/modules/payments/{module.go,payments.sql}`
- `internal/modules/payments/contract/events.go`
- `internal/modules/pharmacy/contract/events.go` (request and cancel subjects)
- Register the module in `cmd/api/main.go`, `archtest.allModules()` and the
  Global API owners (`"payments": "platform"`).

**Subjects:**

- From pharmacy:
  - `helivanta.in.pharmacy.payment_requested.v1`
  - `helivanta.in.pharmacy.payment_cancelled.v1`
- From payments:
  - `helivanta.in.payments.intent_opened.v1`
  - `…intent_failed.v1`
  - `…intent_paid.v1`
  - `…intent_expired.v1`
  - `…intent_cancelled.v1`

**Payloads:**

- Requests carry `requester` (`"pharmacy"`), `request_id`, `amount_minor`,
  `currency`, `expires_in_seconds` and `description`.
- Every outcome carries `requester`, `request_id` and `intent_id`. Then, by
  event:
  - `opened` adds `qr_image_url` and `expires_at`;
  - `failed` adds `reason`;
  - `paid` adds `amount_minor`, `currency`, `provider_payment_id`, `rrn`,
    `paid_at` and `late`.

**Tables:**

- `payment_accounts`: one row per tenant, with `mode`, `key_id`,
  `key_secret_sealed`, `webhook_secret_sealed`, `updated_by` and `updated_at`.
- `payment_intents`:
  - unique on `(tenant, requester, request_id)`;
  - `provider_ref`, the Razorpay QR id, unique per tenant;
  - `status` (checked set);
  - `next_check_at`, `checks_done`, `expires_at` and `late_checks_left`.
- `payment_intent_transitions`: insert-only.
- `payment_provider_payments`:
  - unique on `(tenant, provider_payment_id)`;
  - `outcome` is one of `paid | excess | mismatch`;
  - also stores `rrn` and `amount_minor`.
- `payment_webhook_deliveries`: unique on `(tenant, account, event_id)`.

**Test:** `TestPaymentsSchemaIsTenantIsolated`. A row written under tenant A
is invisible under B, and a transition row cannot be updated.

### Task 2: Razorpay adapter

**Files:** `internal/modules/payments/{provider.go,razorpay.go,razorpay_test.go}`

**Interface (`provider`):**

- `OpenQR(ctx, creds, intentID, amountMinor, currency, closeBy) (qrID, imageURL, closeBy, err)`
- `CloseQR(ctx, creds, qrID) error`
- `QRPayments(ctx, creds, qrID) ([]providerPayment, error)`
- `Verify(ctx, creds) error`
- `VerifyWebhook(body, signature, secret) bool`

**Razorpay calls:**

- Basic auth, base URL from config (`RAZORPAY_API_URL`, default
  `https://api.razorpay.com`), and a 10 s client timeout.
- `providerPayment` holds `ID, Status, AmountMinor, Currency, RRN, CreatedAt`.
  It has no VPA field, by design.
- A non-2xx answer is `errProviderRejected` (4xx) or `errProviderUnavailable`
  (5xx, transport errors, timeouts).

**Test:** `TestRazorpayAdapter`, against an `httptest` Razorpay. It checks:

- the exact request bodies (`type=upi_qr`, `usage=single_use`,
  `fixed_amount=true`, `payment_amount`, `close_by`, and `notes.intent_id`
  only);
- Basic auth;
- the mapping of 4xx and 5xx;
- that a slow server gives unavailable;
- `VerifyWebhook`: true for the right HMAC; false for a flipped byte, a wrong
  secret or an empty signature.

### Task 3: Intents — request, check, cancel

**Files:** `internal/modules/payments/{intents.go,consumers.go,intents_test.go}`

**The request consumer:**

- If the intent already exists, do nothing.
- If the tenant has no account, insert an intent as `failed` and publish
  `intent_failed{provider_not_configured}`.
- Otherwise unseal the secrets and call `OpenQR`.
  - On success, insert `awaiting_payment` and publish `intent_opened`.
  - On a provider error, insert `failed` and publish the reason.

**The cancel consumer:** close the QR, then `check(final=true, cause=cancel)`.

**`check(tx, intent, final bool, cause)`:**

- Fetch the payments and store each new one.
- The first captured payment with matching amount and currency is `paid`.
  Any captured payment after that is `excess`. A captured payment that
  doesn't match is `mismatch`.
- If one is paid: move to `paid`, setting `late` when coming from `expired` or
  `cancelled`, and publish `intent_paid` once.
- Else, if `final`: move to `expired` or `cancelled` and publish.
- Else: schedule the next check.

**Test:** `TestIntentLifecycle`, with a fake provider. It covers:

- a redelivered request (one intent, one QR call);
- no account (`failed`, `provider_not_configured`);
- a provider 5xx (`provider_unavailable`);
- paid;
- mismatch (not paid);
- excess (paid once);
- expired, then late paid (`late=true`, one `paid` event);
- cancel when the payment has already landed (`paid`, not `cancelled`).

### Task 4: Account setup and webhook

**Files:**

- `internal/modules/payments/{account.go,webhook.go,account_test.go,webhook_test.go}`
- `internal/bootstrap/unauthenticated.go`
- `cmd/api/main.go`

**Account routes:**

- `GET /payments/razorpay-account` returns mode, key id, whether each secret
  is set, `updated_at` and `webhook_url`.
- `PUT /payments/razorpay-account` takes `{mode, key_id, key_secret?,
webhook_secret?}`. Secrets are required on first save.
- Saving calls `Verify` first. A rejection is `422 credentials_rejected`.
- Secrets are sealed with Transit key `helivanta-payments-credentials`, with
  derivation `tenant|account`.
- Without Transit, setup answers `503`.
- Permission: `payments.account.manage`, with no roles (administrators only).

**Webhook:**

- `POST /v1/payments/webhooks/razorpay/:tenant/:account`.
- Capped at 64 KiB, signature checked, deliveries deduplicated.
- `qr_code.*` and `payment.*` events that name a known QR id run `check`.
- Everything else gets `200`.

**Tests:**

- `TestAccountSetup`:
  - no Transit gives `503`;
  - rejected keys are not stored;
  - secrets never appear in a response;
  - a non-admin gets `403`;
  - a second tenant can't see the first's account.
- `TestWebhook`:
  - a valid nudge pays;
  - a bad signature, unknown account or unknown tenant each give `401`;
  - a duplicate event id produces no second check;
  - a body over 64 KiB gives `413`;
  - another tenant's QR id changes nothing.

### Task 5: Poller and pharmacy requests

**Files:**

- `internal/modules/payments/reconciler.go` (allowlisted for `WithAllTenants`)
- `internal/modules/pharmacy/{counter_payments.go,counter_payments.sql,counter_payments_test.go}`
- `internal/modules/pharmacy/consumers.go`

**`RunReconciler(ctx, db, bus, provider, transit)`:**

- Every 2 s it lists up to 50 due `(tenant, intent)` pairs, across tenants.
- Each runs `check` under `WithTenant`, with the intent row
  `FOR UPDATE SKIP LOCKED`.

**Pharmacy routes:**

- `POST/GET …/counter-payments[/:request]` and `POST …/:request/cancel`, as
  the spec describes.
- Five outcome consumers, using status-guarded updates.

**Tests:**

- `TestReconcilerExpiresAndFindsLatePayments`.
- `TestCounterPayments`:
  - idempotent `request_key`;
  - a reader (not an editor) gets `404`;
  - cancel on a paid request gives `409`;
  - a redelivered `paid` changes nothing;
  - a `paid` after `expired` gives `late=true`.

### Task 6: Setup page, docs

**Files:**

- `packages/api/src/payments.ts`
- `apps/shell/components/payments/razorpay-account.tsx` and its test
- the page route
- a zones entry with `payments.account.manage`
- `docs/api/domains/payments.md`
- the pharmacy domain doc
- `contractdocs` regeneration

**Page:**

- Shows the mode, key id, which secrets are set, and the webhook URL with a
  copy button.
- The form has fields for the key id and both secrets. The secret fields are
  never filled from a response.

**Tests:**

- a form validation error;
- a `503` explained as "Encrypted storage isn't available in this deployment";
- the saved state shows "set", not the values.
