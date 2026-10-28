# MediConnect account API — #37

API-only work requested by the user. Existing authentication is a prerequisite;
OTP work is paused. This slice owns an account and its preferences, not clinical
identity, dependent verification, consent authority, booking or report release.
The rest of #37 and the MediConnect stories remain open.

Owner: MediConnect. Consumers: patient clients through the Global API. Scope:
verified session tenant and subject; no caller-selected account identity. Routes
require current tenant membership. A hospital registration is not linked by
creating an account, sharing a phone, or accepting an account notice. IN-ID-04,
06 and 07 apply. No demographic or clinical data is copied from patientmaster.

POST /v1/mediconnect/account creates once per (tenant, subject), returning 201;
replays return the same account with 200 and never reset preferences. GET on the
same path returns only the caller's account or 404. PATCH /account/preferences
requires expected_version and replaces all preference fields, with a guarded
update returning 409 on stale writes. Locale is a supported BCP47 application
locale; channel is none/email/whatsapp and is a preference, never send consent.
No SMS channel. Initial channel is none: account creation does not enroll anyone
in outreach. All responses are no-store. Input is bounded, strict JSON, no
identity/tenant fields, no trailing values. No provider calls or credentials.

PostgreSQL owns account state; no cache authority. Forced RLS restricts tenant
AND subject; transactions set the subject GUC. Account UUIDv7, unique subject,
version, timestamps and preference constraints are database-enforced. Append-only
audit records and ID-only account-created outbox event commit atomically with the
account. Concurrent creates resolve the unique key without duplicate events.
Sensitive reads are audited; audit failure aborts the response. The existing
transactional outbox handles broker outage; no live remote call inside a tx.

Assets: account settings and future authorization linkage. Attackers include
another tenant, another subject in the same tenant and a compromised client.
Session verification/membership and SQL owner predicates/RLS are independent
boundaries. Staff permission does not grant access to somebody else's account.
No arbitrary-account admin route or clinical access is introduced.

Reference: ICTHospital e506dfb553f7d146a6d0cddc136ffd5b6c9bdb09, reviewed for
workflow lessons only; no GPL source copied. It lacks this patient-account API.

Local tests must cover actual HTTP via Global API to owner and PostgreSQL,
unauthenticated/nonmember/dependency failures, same-tenant and cross-tenant
isolation, duplicate/concurrent creation, stale updates, strict input, audit and
outbox atomicity. Migration is additive, forward-only; rollback application code
leaves stored accounts intact. No production migration or deployment authorized
by this specification. Patient onboarding without tenant membership still needs
a separate reviewed session/tenant binding; this API must not bypass membership.

## Capacity and deployment choice

Reuse the platform planning envelope in docs/architecture/01-system-design.md:
50/150 tenants at months 12/36, 1000/3000 peak RPS, 80:20 reads:writes and a
30% hot-tenant case. These are planning assumptions, not measured account load.
Account requests are capped at 4 KiB; responses contain one row. Storage grows
one row per (tenant, subject) plus retained audit history. Account-count forecasts,
retention and route-specific latency SLO remain owner review items; no new
capacity or latency claim is made. Keep the existing module runtime and indexed
Postgres lookup rather than create a separate deployment or cache to serve one
row. No new production infrastructure cost is introduced by this source change.
