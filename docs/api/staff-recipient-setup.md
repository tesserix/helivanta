# Staff recipient account setup

Candidate consumer of the identity owner's [Staff setup contract](https://github.com/tesserix/onboarding-service/blob/77d085ab493216a4dc497067a2b31fa8ba451cf1/docs/api/staff-recipient-setup.md).
Credential setup grants no IAM membership, Staff/account link, employment,
pharmacy duty or clinical authority. Live acceptance remains open in the
[onboarding runbook](../qa/hospital-staff-pharmacy-onboarding.md).

## Browser and Global API boundary

The exact public page is `/staff/accept`. The browser sends only same-origin
POST requests through the Global API to `/v1/auth/staff-setup/{action}`;
`action` is one of `status`, `email`, `password`, `resend`. Other Staff pages
remain authenticated. Public session handling also covers `/signup/verify`.

The identity owner supplies canonical Go models, strict decoding and generated
Zod receipt validation. All actions require `invitation_id` and `ticket`.
Email/password require the native `code`; password also requires `password`.
Resend requires a distinct UUIDv7 `delivery_id`. Unknown, duplicate or
phase-inappropriate fields and query strings are rejected. Bodies are limited
to 4096 bytes. No browser-supplied actor, tenant or native user ID is accepted.

These endpoints authorize a bounded invitation capability, rather than a login
session. The identity owner reauthorizes the frozen tenant/product/creation
binding and ticket, native proof, expiry, supersession and account state.
The Global API checks browser origin and a shared per-IP rate limit (20/minute,
burst 10), then uses the dedicated workload credential to a fixed private
identity-owner destination. Browser cookies and authorization are never forwarded.
Missing limiter/credential, dependency failure, invalid response or changed receipt
identity fails closed. The handoff deadline is 30 seconds. No credential payload
is logged, persisted in HMS or put in a query cache.

## Recipient states and recovery

The page removes invitation fragments immediately and retains their secret
values only in memory. Malformed or ambiguous fragments cause no API request.
Verification/password commands require deliberate submission. Password
confirmation must match; fields clear before transmission. A superseded link
instructs the recipient to open their latest email and offers no proof action.
A verified link is redundant; completed accounts link to login and redirect
after seven seconds. Login still enforces the tenant's authentication policy.

Resend retains its delivery command ID across uncertain retries. An explicit
fresh delivery uses a new ID. The owner enforces 30-minute expiry, 60-second
phase cooldown, five retained delivery attempts per rolling day and ten proof
attempts per delivery. Native acknowledgement is retained before read-back;
projection lag never permits redispatch of an acknowledged verification.
Provider acceptance is not evidence of inbox delivery.

Invalid request returns 400; unknown capability returns 404; stale/consumed or
otherwise refused proof returns 409. Rate limiting returns 429 with bounded
`Retry-After`; unavailable authorization or dependencies return 503. The Global
API sanitizes owner errors and validates the receipt against the submitted
invitation/delivery ID. Responses use no-store handling and the acceptance page
sets no-referrer policy.

This slice does not expose protected account creation or silently enroll a
recipient. The owner-controlled ordinary membership, reviewed Staff links,
postings and effective operating permissions remain separate acceptance gates.
