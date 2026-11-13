# Protected Staff account initiation

Implementation candidate; live acceptance remains open. See
[ADR-0071](../adr/0071-protected-staff-account-initiation.md) and the
[hospital/independent-pharmacy acceptance runbook](../qa/hospital-staff-pharmacy-onboarding.md).

IAM authorizes an owner in the verified tenant. The identity owner retains
ordinary native account creation, contact reservation, dispatch and recovery.
Workforce retains Staff records and independently reviewed account links.
Creating or verifying a login grants no membership, Staff link, clinical
authority, employment, pharmacy duty or stock access.

## Global API routes

All browser business calls use the Global API and its existing IAM destination.
The following paths are relative to /v1/iam:

| Method | Path                                                     | Input                                                 | Result                                                                    |
| ------ | -------------------------------------------------------- | ----------------------------------------------------- | ------------------------------------------------------------------------- |
| PUT    | /staff-account-commands/{command}                        | Canonical email, given_name, family_name, reason only | Identity owner's creation receipt; 200 confirmed, 202 prepared/dispatched |
| GET    | /staff-account-commands/{command}                        | No body                                               | Exact initiator's retained creation receipt                               |
| POST   | /staff-account-commands/{command}/observations           | Empty object                                          | Fresh identity-owner observation and retained receipt                     |
| PUT    | /staff-account-commands/{command}/invitations/{delivery} | Empty object                                          | Bounded recipient setup delivery receipt; no proof or contact disclosure  |

Command and delivery IDs are canonical lowercase RFC4122 UUIDv7 values.
Every call requires exactly one X-Helivanta-Expected-Tenant and
X-Helivanta-Expected-Subject matching the verified session. These assertions
detect a changed workspace/account; they are not authority. The server supplies
tenant, actor and configured product. Arbitrary queries are rejected.
Contact bodies are bounded to 4096 bytes and decoded using the identity owner's
strict canonical decoder: duplicate/case-alias keys, unknown authority fields,
non-string values and trailing documents are rejected.

All four operations require iam.member.manage and current tenant_admin
membership, a fresh credential watermark and unexpired acting session.
HMS commits an attributable audit without contact payloads before external I/O,
then rechecks current authority and audits disclosure before returning a result.
Missing or failed source, audit or identity-owner dependencies fail closed.
No database lock spans HTTP. The request deadline is the earlier of the session
idle deadline and 30 seconds.

The exact tenant and initiating actor scope every identity-owner command.
A different owner or tenant cannot discover the retained receipt even if it
knows the UUID. A target account ID, email or hospital ID cannot redirect the
command. This tenant-level account workflow also supports an independent
pharmacy tenant; it does not invent a hospital or employer.

## Idempotency and incomplete results

Retain one creation ID and its frozen submitted contact/reason on every retry.
The identity owner prepares and reserves the contact before attempting native
creation. A timeout is unresolved, not permission to generate another command.
An explicit read or fresh observation reconciles the retained command.
Use a separate retained delivery ID for each deliberate setup link request.
Known delivery retries never repeat email. Native proof, tickets and passwords
are private to the recipient and are never disclosed by protected initiation.

An account is not ready for ordinary enrollment merely because creation is
confirmed. The existing IAM membership-enrollment command checks the identity
owner's current native active/verified/tenant-owned admission and creates only
staff_member. Its receipt, the reviewed Workforce link, employment/posting and
effective scoped access remain distinct.

## Errors and canonical source

Invalid contacts/bodies return 400; revoked/expired credentials 401; missing
current owner or route permission 403; inaccessible command/initiator 404;
changed workspace/account or immutable intent 409. Rate limits return 429 and
bounded Retry-After. Unavailable authorization and audit use the existing IAM
authorization_unavailable error; unavailable identity-owner calls return
503 staff_identity_unavailable. Sensitive responses are no-store.
Neither error body nor audit contains native codes, raw tickets or passwords.

Go staffcontract and staffclient, and generated Zod models from
onboarding-service are canonical. The browser contact schema omits actor_subject
from the owner creation input; HMS binds it from the verified principal. Do not
copy the identity owner's private tables or wire models into HMS. Current
dependencies are pinned in backend/go.mod and packages/api/package.json.
No new business schema or migration is required for initiation.

Tests use the actual identity-owner API and checked canonical SQL, HMS RLS,
current memberships and audit. External Zitadel/email providers are simulated
only within the isolated fixture. CI prepares the owner SQL with a pinned commit
and checksum and reuses its existing PostgreSQL test server in a separate
database. These tests do not prove live inbox delivery, recipient login,
independent review or actual pharmacy operating access.
