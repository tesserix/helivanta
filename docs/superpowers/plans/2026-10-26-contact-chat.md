# Contact us chat, slice 2: enquiry intake

**Goal:** Implement slice 2 of [the contact chat design](../specs/2026-10-26-contact-chat-design.md) (#1355): a visitor sends an enquiry from the home page, the team is notified in a `#onboarding-helivanta` discussion, and `sales@tesserix.app` gets a copy.

**Architecture:** A new platform-scoped `enquiry` module owns the `enquiries` table. `POST /v1/enquiries` is an unauthenticated route mounted through `bootstrap.MountUnauthenticated`. It validates the generated request model, applies IP and global limits, verifies Turnstile and writes the row and the `helivanta.in.enquiry.received.v1` outbox event in one `WithSystem` transaction. Two consumers deliver it in `AfterCommit`, outside the transaction and safe to repeat: `enquiry-discussion` finds or creates the Rocket.Chat discussion by name, and `enquiry-email` sends through Resend with the enquiry ID as idempotency key. A sweep logs overdue deliveries, and a purge job enforces the 180-day retention. The shell adds the contact launcher, form and waiting state.

**Contracts consulted:** `docs/api/http-contract.md`, `docs/api/global-connector.md`, `docs/api/integration-contracts.md`, `docs/api/schemas.md`, `docs/standards/backend.md` §6–7 and §11, `docs/standards/frontend.md`.

## Changes from the spec

- **The purge moves into this slice.** Any build merged to `main` can be promoted to production. Without the purge, the 180-day promise in the privacy notice would be false from the first enquiry. Slice 3 only adds transcripts to what the purge erases.
- **Reference format (my call).** `HV-` plus 8 Crockford base32 characters (40 random bits). It is unique in the table, and a collision retries up to 3 times. It appears in the discussion name, the email subject and the waiting state.
- **Email link (my call).** Each email links to the discussion if one was recorded before sending. Otherwise it links to `#onboarding-helivanta`. The email never waits for Rocket.Chat, so either channel can fail without blocking the other.
- **Alert surface.** The cluster has no metrics sink yet (#679), and nothing evaluates a `PrometheusRule`. So an overdue delivery is a structured `ERROR` log, `enquiry delivery overdue`, carrying the enquiry ID and the missing channel. Routing that log to a person is slice 1 work in tesserix-k8s. If no log-based alerting exists there, the PR records the gap under #679 and does not claim an alert.
- **Conversation token.** It moves to slice 3, with the routes that use it. Adding the field to the `202` body later is an additive change.

## Merge order

Production refuses to boot without the delivery secrets (D9). This PR therefore merges only after slice 1 has the Resend key, bot token and Turnstile secret in OpenBao and the ESO entries synced. Until then it stays open with checks green.

## Tasks

### 1. Prove Rocket.Chat 8.9 behaviour locally

- [x] Run `rocketchat/rocket.chat:8.9.0` with MongoDB in a disposable compose project under the scratchpad (images are pulled, not built). Create an admin, two members, a `bot`-role user with `start-discussion` granted (D10), and a public channel holding all four.
- [x] As the bot, with its personal access token:
  - `rooms.createDiscussion` with `prid`, `t_name`, `users` and `reply` succeeds;
  - the same call is refused before `start-discussion` is granted;
  - `rooms.getDiscussions` finds the discussion by name;
  - each invited member is notified of the first message (desktop event when online, push queued when offline);
  - visitor text with `@all`, `@here` or `@user` resolves mentions in the bot's discussion, and does not once U+200B follows the `@`;
  - `rooms.delete` by the bot erases the discussion;
  - the parent channel keeps the system message after the erase.
- [x] Record the results in the spec (D1, D2, D8, D10). A local subscription shows that a notification is due, not that a phone receives it. Production delivery to phones is checked by the team after slice 1, with a real test enquiry.

### 2. Canonical request contract

- [ ] Extend `scripts/lib/contract-codegen.mjs` with an `email` format and an `e164` format, and make `name` and `text` reject control characters (CR and LF included). Tests first, against an existing generator's `--check` output staying byte-identical.
- [ ] Add `contracts/enquiry.json` and `scripts/generate-enquiry-contract.mjs`. Together they generate `backend/internal/modules/enquiry/request_gen.go` and `packages/api/src/enquiry-contract.ts`. Fields: `first_name` (1–80), `middle_name` and `last_name` (optional, up to 80), `email` (up to 254), `phone` (E.164), `organisation` (optional, up to 160), `question` (1–1,000) and `turnstile_token` (1–2,048).
- [ ] Wire the generator's `--check` into the same CI step as the other generators.

### 3. Module, table and isolation

- [ ] `make new-module NAME=enquiry`, then replace the template's tenant shape. The module registers in `bootstrap.Modules()` and `archtest.allModules()`. It declares no permissions and no authenticated routes.
- [ ] Migration `0001_enquiry` creates `enquiries`:
  - `id uuid` (v7), unique `reference`;
  - the contact fields and `question`;
  - `discussion_room_id`, `discussion_at`, `emailed_at`, `overdue_reported_at`;
  - `created_at` and `last_activity_at`;
  - no client IP, and no Turnstile token.
- [ ] Forced RLS with `USING` and `WITH CHECK` both `NULLIF(current_setting('app.tenant_id', true), '') IS NULL`. Rows are therefore visible only to transactions with no tenant. Add `enquiries` to `lintAllowlist`, with that reason.
- [ ] Tests first:
  - a `WithTenant` transaction reads zero rows and cannot insert;
  - `WithSystem` reads and writes;
  - each assertion is shown to fail when the policy is removed.

### 4. Turnstile verification

- [ ] A `turnstileVerifier` interface in the module, with an HTTP implementation for `siteverify`. It sends `secret`, `response`, `remoteip` and an idempotency key, has a 5-second timeout, and requires `success`, `action == "enquiry"` and an allowed `hostname`.
- [ ] Tests against `httptest`:
  - a pass accepts;
  - a rejection, wrong action or wrong hostname returns `400 verification_failed`;
  - a timeout, 5xx or malformed body returns `503` (fail closed).

### 5. `POST /v1/enquiries`

- [ ] Add the route to `UnauthenticatedRoutes`, with its reason, and to `MountUnauthenticated`, with a nil-handler guard.
- [ ] Handler order:
  1. 8 KiB `MaxBytesReader` (`413`).
  2. Strict decode into the generated model, then validation (`400`).
  3. 5 per hour per `c.ClientIP()`, then 200 per hour in total (`429`, with `Retry-After`). Limits fail open, with a warning log.
  4. Turnstile.
  5. One `WithSystem` transaction that inserts the row and publishes the event.
  6. `202 {id, reference}`.
- [ ] Logs carry the enquiry ID and request ID only.
- [ ] Tests first, through the real engine:
  - the happy path writes exactly one row and one outbox row;
  - the outbox payload bytes contain no name, email, phone or question;
  - every validation case, including CRLF in a name, a non-E.164 phone, a 1,001-character question, an unknown field and a 9 KiB body;
  - Turnstile fail-closed;
  - the sixth request from one IP and the 201st overall;
  - a limiter error still admits the request.

### 6. Event contract

- [ ] `contract/events.go`: `SubjectEnquiryReceived = "helivanta.in.enquiry.received.v1"` and `EnquiryReceivedData{EnquiryID}`. Add the field to `eventPayloadReviewedNonPHI`, with its reason. `Publishes()` returns the subject.

### 7. Rocket.Chat discussion consumer

- [ ] A `rocketchat` client:
  - uses the bot's user ID and token headers, with a 10-second timeout;
  - methods for channel members, find discussion by name, create discussion and delete room;
  - tests assert the exact requests.
- [ ] Consumer `enquiry-discussion`. `Handle` decodes the contract payload. `AfterCommit`:
  1. Loads the enquiry, and returns if `discussion_room_id` is set.
  2. Finds or creates `Enquiry <reference>`, inviting every channel member except the bot, with the first message as `reply`.
  3. Records the room with `UPDATE … WHERE discussion_room_id IS NULL`.
- [ ] The first message lists name, organisation, email, phone and question. It uses no alias. A `neutraliseMentions` function inserts U+200B after every `@` not directly preceded by a letter or digit, and every visitor-supplied field passes through it (D1). Table tests cover `@all`, `(@all)`, `*@here*`, `> @all`, a newline-led `@here`, `_@here_`, an email address and Devanagari text.
- [ ] Tests first:
  - one discussion per enquiry across redeliveries;
  - a crash after creation but before recording finds the existing discussion and does not create a second;
  - a Rocket.Chat 5xx returns an error, so JetStream redelivers;
  - a purged enquiry is a no-op.

### 8. Email consumer

- [ ] A `resend` client: `POST /emails` with `Idempotency-Key: enquiry-<id>`, plain text only, and a 10-second timeout.
- [ ] Consumer `enquiry-email`, in `AfterCommit`:
  1. Loads the enquiry, and returns if `emailed_at` is set.
  2. Sends to `sales@tesserix.app` from `Helivanta enquiries <noreply@tesserix.app>`, with `Reply-To` set to the visitor and subject `Helivanta enquiry <reference>`.
  3. Records `emailed_at`.
- [ ] Tests first:
  - redelivery reuses the same idempotency key, and sends nothing once `emailed_at` is set;
  - a 5xx retries;
  - a `422` is logged with the enquiry ID and returned, ending in the DLQ rather than looping;
  - the body links to the discussion only when one is recorded.

### 9. Overdue sweep and retention purge

- [ ] `RunDeliverySweep` is started in `cmd/api/main.go` beside `bus.RunPruner`, following `iam/loginattempt_sweep.go`. Every minute it logs `enquiry delivery overdue` once per enquiry still missing a channel 10 minutes after creation.
- [ ] `RunRetention` runs hourly. For each enquiry whose last activity was more than 180 days ago, it deletes the discussion (`400 error-invalid-room` counts as already gone, as proved on 8.9.0), then the row. A Rocket.Chat failure keeps the row for the next run.
- [ ] Tests use an injected clock, cover both, and include a failed discussion delete.

### 10. Configuration and boot checks

- [ ] New configuration:
  - `ROCKETCHAT_URL`, `ROCKETCHAT_BOT_USER_ID`, `ROCKETCHAT_ENQUIRY_ROOM_ID`;
  - `ENQUIRY_EMAIL_TO`, `ENQUIRY_EMAIL_FROM`;
  - `TURNSTILE_ALLOWED_HOSTNAMES`.
- [ ] New secrets: `ROCKETCHAT_BOT_TOKEN`, `RESEND_API_KEY`, `TURNSTILE_SECRET`.
- [ ] `Require*` accessors refuse to boot outside dev. In dev, a missing value makes delivery fail visibly (an error, then the overdue log), never succeed silently.
- [ ] Secret types print `[redacted]`.
- [ ] `.env.example` uses Cloudflare's published always-pass Turnstile test keys.
- [ ] Tests cover each refusal message.

### 11. Global API

- [ ] Add `"enquiries": "platform"` to the owner map, and `"enquiry": "platform"` to `expectedOwners`.
- [ ] Gateway tests:
  - a same-origin `POST /v1/enquiries` reaches the platform upstream;
  - a cross-site one is refused;
  - the visitor's address reaches the API as `X-Forwarded-For`, never the shell's address.

### 12. Shell

- [ ] `middleware.ts`: exactly `/api/v1/enquiries` is public, with tests that `/api/v1/enquiries/x` and similar paths stay protected.
- [ ] CSP:
  - add `https://challenges.cloudflare.com` to `script-src` and `frame-src`;
  - tests in `lib/csp.test.ts`;
  - a case in `e2e/tests/csp.spec.ts` that loads the widget with no violation.
- [ ] `components/landing/`:
  - `contact-launcher.tsx`: a header button left of "Emergency help", and the floating button;
  - `contact-panel.tsx`, with sub-parts:
    - a form built with `useZodForm` and `Field` on the generated schema, `noValidate`, a country-code select preselected to India, and a privacy notice;
    - `turnstile.tsx`, which loads the script with the request nonce;
    - the waiting state.
  - The panel uses `useApiMutation`.
- [ ] Error copy:
  - `429`: "Too many enquiries from this network. Try again later, or email sales@tesserix.app."
  - `503`: "We couldn't check this request. Try again, or email sales@tesserix.app."
- [ ] Tests first with `renderWithProviders`:
  - inline errors for each field;
  - no submit before Turnstile completes;
  - the waiting state shows the question and the reference;
  - both error messages;
  - both buttons open the same panel.

### 13. Documentation and contracts

- [ ] `go run ./cmd/contractdocs`, then `-check`.
- [ ] Update `docs/api/global-connector.md` (route map), `docs/api/http-contract.md` (the public route and its errors) and `docs/api/integration-contracts.md` (Rocket.Chat, Resend and Turnstile, each with its failure behaviour).
- [ ] Correct the spec for the changes listed above, and record the task 1 results.

### 14. Verify

- [ ] Backend:
  - `make lint-go`;
  - `./scripts/coverage-gate.sh`;
  - `go test -race ./...`.
- [ ] Generators: every `--check` passes.
- [ ] Frontend on Node 24: `pnpm turbo lint type-check test build format:check`, and `make lint-shell`.
- [ ] Run the stack locally on port 4301 with the Turnstile test keys. Send an enquiry, check the waiting state, and check the row and outbox event. Check the CSP on `/`. Take screenshots at desktop and mobile widths.
- [ ] Open the PR with `Closes #1355` only if slice 3 is merged with it. Otherwise use `Part of #1355`. Name the contracts consulted and changed, and list what still needs review: the DPDP wording, production notification to phones, and alert routing.
