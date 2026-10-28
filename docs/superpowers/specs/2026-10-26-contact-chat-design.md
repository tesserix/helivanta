# Contact us chat

Resolves #1355. Depends on the Global API route map ([spec](2026-10-24-global-api.md)) and on tesserix-k8s ADR-0020 (Rocket.Chat at `rocketchat.tesserix.app`). Feeds, but does not implement, prospect records (#807).

Status: approved 2026-10-26, including D1 and D10. The `helivanta` team was created on 2026-10-26. D1, D2, D8 and D10 were proved against a local Rocket.Chat 8.9.0 the same day; see [Proof against Rocket.Chat 8.9.0](#proof-against-rocketchat-890).

## Why

A hospital that likes the home page has no way to ask a question. The team already works in Rocket.Chat and sends product email through Resend, so an enquiry should land where the team is, notify them, and leave a copy with sales. The visitor should see the moment a real person picks the conversation up.

## What the visitor sees

1. **Contact us.** A header button left of "Emergency help", and a floating chat button at the bottom right. Both open the same panel.
2. **Short form.** First name (required), middle and last name (optional; many people use one name), email, phone with country code (India preselected, any country allowed), organisation (optional), and the question (up to 1,000 characters). A privacy notice states what is collected, why, how long it is kept and how to ask for deletion. A Cloudflare Turnstile check runs before sending.
3. **Waiting.** After sending, the panel shows the question as the first message and says "Sent to the Helivanta team. We'll reply here, or by email if you've gone."
4. **Connected.** When a team member joins, the panel says "Connected: Samyak from Helivanta joined", using the team member's first name only. Messages then flow both ways while the panel is open.
5. **Ended.** After 30 minutes without messages, or when the visitor closes the tab, the conversation ends for the visitor. The team still has the transcript and email address.

## What the team sees

1. The private Rocket.Chat team `helivanta` decides who is notified. `#onboarding-helivanta` is its auto-join channel, so adding someone to the team adds them to the channel. That channel also receives first-time signup notifications.
2. Each enquiry gets its own discussion in `#onboarding-helivanta`, named `Enquiry <reference>` (D8 explains why the name carries no personal data). The bot starts it with every channel member invited.
3. The discussion's first message lists the contact details and question. It is an ordinary message in a room each member belongs to, so every member gets a desktop and mobile notification under their own preferences.
4. Visitor messages are bot messages that begin with the visitor's name. Any team member's reply in the discussion reaches the visitor.
5. An email goes to `sales@tesserix.app` from `Helivanta enquiries <noreply@tesserix.app>`. Its `Reply-To` is the visitor's email, and it links to the discussion. `tesserix.app` is the domain verified in Resend that every Tesserix product sends from. Its inbound mail goes through Cloudflare Email Routing, and DNS cannot show whether a rule exists for `sales@`, so slice 1 confirms one.

## Decisions

**D1 — Bot relay through discussions, not Omnichannel livechat. Approved.** Rocket.Chat's Omnichannel livechat would give a native visitor queue, but enabling it opens Rocket.Chat's public `/api/v1/livechat/*` endpoints to the internet. That reverses ADR-0020's rule that internet visitors must not reach the workspace. Instead, the Helivanta API is the only internet-facing surface, and a bot account relays messages. This adds a third, non-human workspace account. ADR-0020 currently allows exactly two accounts, so it needs an amendment in tesserix-k8s. The bot is `helivanta-onboarding-bot`, following the product registry from tesserix-k8s #1605. It has only the shared `bot` role and belongs only to `#onboarding-helivanta` and the discussions it starts. #1605 removes `message-impersonate` and `mention-all` from that role. So the bot posts as itself and puts the visitor's name in the message text rather than an alias. Revoking `mention-all` is not enough on its own: a discussion's creator holds that room's `owner` role, which carries `mention-all` and `mention-here`. Visitor text inside a discussion could therefore notify everyone. Before relaying any visitor-supplied text (names, organisation, question, messages), the bot inserts U+200B after every `@` that is not directly preceded by a letter or digit. Email addresses and letter-led forms such as `x@all` never resolve, so they stay intact and copyable.

**D2 — Connected means a team member replied.** Members are invited when the discussion starts, so joining no longer shows that someone picked the enquiry up. The first message in the discussion from anyone except the bot marks the enquiry connected and records that member's first name. Discussions started from a public channel are public rooms (Rocket.Chat 8.9 `getDiscussionType` returns `c`). In this workspace, "public" means visible to the team only. On 8.9.0, an invited member who is online receives a desktop notification for the first message, and one who is offline gets a mobile push queued. That holds under the workspace defaults: desktop and push notifications set to `all`, and `Notifications_Max_Room_Members` at 100. The runbook checks those values in production. `Unread_Count` defaults to mentions only, so the unread badge stays 0 while the room still alerts.

**D3 — The Helivanta API owns enquiries.** A new `enquiry` backend module owns the record, the transcript and delivery state. It is Tesserix's own data, not a hospital tenant's, so its tables are platform-scoped. They are unreachable from any tenant context, under the repo's forced-RLS rules; the plan fixes the exact policy. Schemas live in tesserix-k8s `db-schema-bootstrap`. The Global API maps `/v1/enquiries` to the `platform` owner.

**D4 — Durable delivery through the outbox.** `POST /v1/enquiries` validates input, verifies Turnstile and writes the enquiry and an outbox event in one transaction. It then returns `202` with an enquiry ID and a conversation token. Two consumers act on `helivanta.in.enquiry.received.v1`. One creates the discussion. The other sends the email, with the enquiry ID as Resend's idempotency key. Payloads carry the enquiry ID only, no contact details. Failures retry through JetStream redelivery. An enquiry still undelivered after 10 minutes raises an alert. If Rocket.Chat is down, the email still arrives, and the reverse holds too.

**D5 — Conversation access is a bearer token, held only in the tab.** The token is 256 random bits. The server stores only its SHA-256 digest; the browser keeps it in `sessionStorage`. A wrong or expired token returns 404, never 403. Read access ends 24 hours after the last activity.

**D6 — Polling through the API, bounded on the server.** The panel polls `GET /v1/enquiries/{id}/conversation?after=<cursor>` with `useApiQuery`'s `poll` option, while the panel is open and the tab is visible. The server serves from its own transcript. It refreshes from Rocket.Chat history with a per-enquiry single flight, at most once every 2 seconds, so visitor traffic never multiplies Rocket.Chat calls. Visitor messages go through `POST /v1/enquiries/{id}/messages`: they are stored, then relayed by a consumer. A real-time socket is rejected for now, as the team is two people and polling stays within one request per poll interval per open panel.

**D7 — Abuse limits, and which controls fail closed.** These routes are unauthenticated, so they get their own buckets, keyed on the client IP verified by the edge. The address is never taken from a client-supplied `X-Forwarded-For`. The limits are:

- 5 enquiries per hour per IP.
- 200 enquiries per hour in total, to protect Resend and Rocket.Chat quota.
- 30 visitor messages per 10 minutes per enquiry.
- One poll per interval per enquiry.

Rate limits fail open, as `docs/standards/backend.md` requires. Turnstile is a security control, so it fails closed: if verification is unavailable, the form says "We couldn't check this request. Try again, or email sales@tesserix.app." Bodies are capped at 8 KiB. Names reject control characters, and email and phone are validated (phone normalised to E.164), so nothing can inject mail headers.

**D8 — Privacy and retention.**

- **Logs:** enquiry IDs only, never names, contacts or message bodies.
- **Retention:** contact details and transcripts are purged 180 days after the last activity. The same purge job erases that enquiry's discussion, human replies included, with `rooms.delete`. In 8.9 a room's creator gets its room-scoped `owner` role, which carries `delete-c`, so the bot can erase discussions it started. It cannot delete the parent channel. Erasing a discussion that is already gone returns `400 error-invalid-room`, which the purge treats as done. A channel-wide retention policy is rejected because it would also delete `#onboarding-helivanta`'s signup notifications.
- **Discussion name:** erasing a discussion leaves its system message in the parent channel, because 8.9 only unsets that message's discussion fields. The bot has no message-delete permission. So the name holds only the enquiry reference, never a name or organisation.
- **Email:** copies in the sales inbox follow that mailbox's own retention, and the privacy notice says so.
- **Review:** the notice wording needs review against the Digital Personal Data Protection Act 2023 for India and the applicable law in each later launch country.

**D9 — Secrets and configuration.** All of these are product secrets in OpenBao, one key per path under `<namespace>/helivanta-api/`, delivered through the API's existing `openbao-helivanta-api` SecretStore:

- Resend API key, restricted to sending from `tesserix.app`.
- The bot's personal access token.
- Turnstile secret.

The bot's user ID, the channel's room ID and the Turnstile site key are configuration. Missing secrets fail the API's boot in production rather than silently dropping enquiries. The CSP gains `https://challenges.cloudflare.com` for `script-src` (nonce-bound) and `frame-src`, with a `csp.spec.ts` case.

**D10 — Bots may start discussions. Approved.** In 8.9, `createDiscussion` requires `start-discussion` or `start-discussion-other-user` on the parent room. #1605 restricts the shared `bot` role to `view-joined-room`, so the bot has neither. The recommended option is to grant `start-discussion` to the `bot` role. Every product bot could then start discussions, but only in rooms it can already post to. The rejected option is making this bot owner of `#onboarding-helivanta`. That would also let it delete the channel, remove members and erase signup notifications. With this option, #1605's runbook changes its steady-state role to `view-joined-room` plus `start-discussion`, and its denial checks must still pass.

## Proof against Rocket.Chat 8.9.0

Run on 2026-10-26 against `rocketchat/rocket.chat:8.9.0` and MongoDB 8.0 in a disposable local compose project. The bot role was reduced to #1605's steady state, and its token was issued through the runbook's temporary grant.

| Claim                                                                                                                         | Result                                                                      |
| ----------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Bot with only `view-joined-room` cannot start a discussion                                                                    | `400 error-action-not-allowed`                                              |
| After D10, `rooms.createDiscussion` with `prid`, `t_name`, `users`, `reply`                                                   | `200`, room type `c`; bot holds room `owner`                                |
| Bot lists channel members; `rooms.getDiscussions` and `rooms.get?updatedSince` find the discussion by name                    | all `200`                                                                   |
| First message is posted as the bot, with no alias                                                                             | holds                                                                       |
| Online member gets a desktop notification; offline member gets a mobile push queued                                           | holds, under default notification settings                                  |
| Raw `@all`, `@here`, `@user`, `(@all)`, `*@here*`, `> @all` in the bot's discussion                                           | resolve as mentions                                                         |
| Same text with U+200B after `@`; `asha@example.com`; `x@all`; letter- or digit-led `@` in Latin, accented and Devanagari text | no mentions                                                                 |
| Bot deletes the parent channel                                                                                                | refused                                                                     |
| Bot erases its discussion; erasing again                                                                                      | `200`; then `400 error-invalid-room`                                        |
| Parent channel after the erase                                                                                                | keeps the `discussion-created` message with the name, discussion link unset |

Local proof shows that a notification is sent or queued, not that a phone shows it. The team confirms phone delivery in production with a real test enquiry after slice 1.

## Slices

1. **tesserix-k8s:**
   - Schemas, ESO entries and values.
   - ADR-0020 amendment for the bot account, and the `helivanta` entry in `config/rocketchat/products.yaml` once #1605 merges.
   - A runbook for the one-time admin steps:
     - create `helivanta-onboarding-bot` and its token;
     - apply D10;
     - invite the bot to `#onboarding-helivanta`;
     - add each person who should be notified to the `helivanta` team, after their first sign-in;
     - create a sending-only Resend key;
     - confirm the Cloudflare Email Routing rule for `sales@tesserix.app`.
2. **Helivanta, enquiry intake:**
   - Module, routes, Global API mapping and outbox consumers.
   - Header button, launcher and form.
   - Waiting state with the email fallback.
   - Contracts, tests and CSP.

   On its own this is a complete contact form that notifies the team in Rocket.Chat and by email.

3. **Helivanta, live conversation:** connected state, two-way relay, session end and purge job.

## Not covered

- Turning an enquiry into a prospect record (#807).
- Replying to visitors from Rocket.Chat by email.
- Staff support chat inside the product (#401).
- Opening hours or an automatic out-of-hours reply.
- Attachments.
