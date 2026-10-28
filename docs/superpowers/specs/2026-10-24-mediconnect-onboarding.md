# MediConnect account and OTP boundary

Status: proposed implementation contract; not an implemented API.
Tracking: #551, #37, #552, #556 and #75. See the
[delivery evidence](../../delivery/mediconnect.md) for source findings, assignments
and prerequisites. Domain references: IN-ID-01/04/05/06/07/08.

## Authentication and authorization decisions

Retain Zitadel as credential authority. A separate patient client/audience and
patient session boundary must prevent a patient OTP from authorizing staff routes,
including when one identity also has a staff role. Staff sessions retain their
existing password/MFA policy. The Global API forwards credentials to the owning
API and never grants patient access from headers, URL selectors or phone numbers.

Email and WhatsApp are supported delivery channels in the proposed release; SMS
is disabled. UI capability discovery reflects configured, operational channels.
A disabled/misconfigured channel cannot dispatch, enqueue or silently fall back.
Reject client attempts to override the provider, recipient, template, callback
URL, verification purpose or tenant binding.

For existing identities, read provider state using an exact server-resolved
identity, generate a provider challenge and verify its current factor, subject,
purpose, expiry and patient application before issuing a patient session. Do not
assume `CheckUser.login_name` resolves an email or phone. Do not link multiple
provider identities or patient registrations on matching contact details.

For first registration, prove the native provider's contact-verification and
account-creation sequence in a disposable instance before implementing it. The
existing session OTP contract presupposes a checked user; it alone does not prove
safe new-user enrolment. An account created before verification has no patient
session, hospital grant, clinical link or discoverable record. Duplicate signup,
timeout after creation and provider retries require reconciliation by a stable
opaque registration command ID rather than creating another identity.

Phone and email verification prove control of a contact destination, not identity
of a hospital patient. New accounts begin unlinked. Facility-reviewed binding is
a separate attributed operation with current reviewer authority and evidence.
Shared contacts, recycled numbers and probabilistic matches cannot create a link.

## API ownership and proposed surface

All browser/native requests use the Global API `/v1/mediconnect/*` namespace
(`/api/v1/mediconnect/*` at the web origin). Only the named onboarding methods are
unauthenticated; each has origin/client/purpose binding and dedicated abuse limits.
No wildcard bypass of the authenticated chain or arbitrary upstream URL is allowed.

| Method and path suffix                                              | Behavior and owner                                                                                                                                    |
| ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /auth/capabilities`                                            | Configured channel and locale availability only; no account lookup                                                                                    |
| `POST /auth/otp/challenges`                                         | Opaque challenge for an allowed destination/channel and server-selected tenant/patient app; generic response independent of account existence         |
| `POST /auth/otp/challenges/{id}/verify`                             | Consume current challenge once, check provider proof, then issue only the appropriate patient onboarding/session state                                |
| `POST /auth/otp/challenges/{id}/resend`                             | New generation invalidates prior code; enforce cooldown and rolling destination/device/IP budgets                                                     |
| `GET /me` and `PATCH /me/preferences`                               | Current account only; versioned locale/accessibility/channel preferences                                                                              |
| `GET /me/profiles`                                                  | Current authorized hospital registration links; no candidate demographics disclosed                                                                   |
| `POST /me/link-requests`                                            | Request review; never grant access on self-assertion                                                                                                  |
| `POST /hospitals/{hospital}/link-requests/{id}/decisions`           | Authorized identity reviewer approves/rejects evidence; owner checks hospital/patient reference, version and independent review where policy requires |
| `POST /me/profile-grants` and `POST /me/profile-grants/{id}/revoke` | Explicit action/purpose/expiry delegation; caregiver is not guardian; revocation preserves history                                                    |
| `GET /me/consents` and `POST /me/consents/{id}/withdraw`            | Compose the single consent authority; deny future affected access immediately, expose downstream acknowledgement separately                           |
| `POST /me/rights-requests` and `GET /me/rights-requests/{id}`       | Access/correction/erasure/grievance intake and status under assigned accountable owner; never delete clinical data directly                           |

Clinical composition follows only after owner contracts exist. Booking commands
must reach MediCore scheduling; reports must reach the release-owning service;
prescriptions and bills remain their owners' records. Missing integrations return
explicit unavailable status, not empty fabricated records, dummy slots or success.

## Storage and migration contract

One canonical contract input generates Go/Zod payload validation; migrations own
storage. New table IDs use UUIDv7 with version CHECKs. Every tenant table has forced
RLS with USING and WITH CHECK, composite local foreign keys including tenant,
bounded indexes and attributable append-only audit. External owner references are
validated through published owner contracts, never foreign-table reads.

| Record                     | Required state and constraints                                                                                                                                                                                                                             |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Onboarding challenge       | Opaque ID; server-selected application/tenant; HMAC destination key; channel; purpose; generation; hashed browser binding; issued/expiry times; attempt budget; encrypted provider session reference; state/version; no raw code or contact in logs/events |
| Challenge delivery attempt | Challenge/generation; provider message ID; queued/accepted/delivered/failed/unknown status; last attempt; expiry; deduplication key; callback identity; encrypted short-lived payload only if durable delivery is necessary                                |
| Patient account            | Unique tenant/provider issuer/subject; preferred locale and accessibility settings; unresolved/held/linked/suspended state; optimistic version; no duplicated clinical demographics master                                                                 |
| Registration link request  | Account; owning hospital; opaque candidate reference; requested/reviewed/declined state; evidence reference; reviewer/decision time/version; patient details visible only to an authorized reviewer                                                        |
| Profile grant              | Account and owner registration reference; self/guardian/caregiver relationship; explicit action scopes; policy/evidence version; start/expiry/revocation; grantor; no implied rights from relationship label                                               |
| Consent reference          | Reference to the platform authority's versioned notice/receipt and current decision; no independently writable consent master in MediConnect                                                                                                               |
| Rights request             | Subject and actor profile; kind; assigned owner; state; policy-derived due date; immutable transitions and resolution evidence; no automatic erasure                                                                                                       |
| Command receipt            | Unique tenant/subject/operation/command ID; canonical input fingerprint; recorded response and source version; immutable changed-payload conflict                                                                                                          |
| Consumer inbox/outbox      | Stable event ID and source aggregate version; duplicate/out-of-order guards; transactional writes; no OTPs, contact details or clinical payload in general events                                                                                          |

Challenge/provider secrets require application-layer encryption through the
existing secret/encryption boundary. Do not duplicate provider-owned OTP hashes
as a second verifier. Provider tokens and returned codes never reach a client or
an unencrypted general-purpose event bus. Contact-destination keys use keyed HMAC,
not dictionary-enumerable unsalted hashes.

Use a lock or atomic version transition to consume a challenge; concurrent correct
submissions cannot mint two independent completions. A resend replaces its prior
generation, including queued deliveries. Expired challenges fail on read even if
the cleanup worker is delayed. Provider outage cannot consume an attempt as a bad
code or mint a session. An ambiguous provider response requires status
reconciliation; blind regeneration risks multiple valid challenges.

The authenticated subject and verified tenant drive account lookups. Profile reads
also require a current hospital-scoped grant and source release authority. Check
authority before replaying stored sensitive command responses. Revocation commits
before acknowledgement and serializes against sensitive disclosures. Events/FGA
projections cannot temporarily restore access after a locally committed revocation.

## Delivery configuration

Email initially reuses the reviewed Zitadel SMTP path; do not change shared sender
credentials or instance policy as a side effect of patient onboarding. Confirm the
approved sender and test actual inbox delivery before activation.

For WhatsApp, select the configured provider and approved authentication template.
The native phone challenge's server-return mode is a candidate mechanism, subject
to integration verification; never invoke native SMS send mode while SMS is
disabled. Deliver only to the provider-verified challenge recipient, not a second
client-supplied address. Signed callbacks need replay protection and configured
connection/tenant mapping. A provider acceptance response is not proof of delivery.

A failed WhatsApp delivery does not automatically switch to a newly entered email.
Offer a separately verified account contact or restart that contact's onboarding.
Never send live test messages without an explicitly supplied test recipient.

## Scope, capacity and operational behavior

The platform planning envelope remains 50/150 tenants, 1,000/3,000 peak RPS at
12/36 months, 80:20 reads/writes and typical 2–20 KB JSON. Those are aggregate
planning assumptions, not measured MediConnect demand. Patient-volume, concurrent
OTP demand, provider cost/rate ceilings and retention are unknown and must be
measured/approved before capacity or cost claims. Proposed own-service target:
99.9% availability and p99 below 500 ms for account reads excluding provider
latency; OTP delivery has a separately measured provider SLO. Limit list pages,
body sizes, queue attempts and provider concurrency from the first release.

When identity/authorization/consent is unavailable, deny protected access. When a
clinical owner is unavailable, mark only its section unavailable. When delivery is
unknown, expose pending/retry status without pretending an OTP arrived. Expired
challenges are not resent. Revoked grants do not become temporarily valid during
queue outages. No automated retry of bookings/payments without owner idempotency.

Deployment uses GitOps, two replicas where required by the approved SLO, private
product ingress, mesh mTLS, dedicated workload identity and secret references.
Review an independent product deployment against the simpler registered-module
option after the authentication/owner contracts are proven; neither can share
writable patient/clinical tables or bypass the existing migration hold. Database
expansion is additive; post-write recovery preserves audit and accepted commands.

## Acceptance tests before any issue closure

- Existing and first-time email/WhatsApp identities complete the actual provider
  flow in an isolated environment, including address verification and required
  policy/method enrollment. No staff role or staff session is obtained.
- SMS absent from capabilities and refused by direct API calls; no SMS network
  call on request, resend, fallback, replay or queued worker recovery.
- Wrong, expired, reused and superseded OTPs; simultaneous verification; attempt
  exhaustion; resend limits; provider timeout before/after acceptance; late and
  forged callbacks; no account enumeration through response or timing.
- One phone shared by relatives; reassigned phone; two simultaneous claims;
  wrong tenant/hospital; missing evidence; revoked reviewer; caller-supplied
  subject/patient ID; no clinical access before verified link.
- Self/guardian/caregiver action differences, expiry and policy-defined majority
  transition, subject withdrawal, stale version and changed idempotency payload.
- Consent withdrawal succeeds locally with propagation pending during outage,
  and affected disclosures stop immediately; receipt/timeline remains truthful.
- Client uses only Global API; no private owner bypass; real database RLS and
  object grants; audit failures fail protected reads; logs contain no secrets/PHI.
- Owner report release/amendment/withdrawal and profile cache purge; browser and
  native device isolation; stale offline status remains explicit.
- Generated-contract parity, migrations on disposable Postgres, race/coverage,
  lint/typechecks/builds, production-build browser tests, delivery replay tests,
  vulnerability scans, and post-deployment synthetic probes.

No automated test substitutes for approval of notice text, guardian evidence,
clinical release rules, provider templates, legal applicability or real-user
accessibility/comprehension criteria in the linked issues.
