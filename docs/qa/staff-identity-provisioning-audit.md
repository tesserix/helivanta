# Staff identity provisioning audit

Date: 2026-10-30. Scope: A01/A02 prerequisites for the full Access/RBAC plan.
Status: source and provider-contract findings; implementation and runtime
acceptance remain open. Related issues #417 and #1053 remain open.

## Current implementation

- IAM's `loginclient.Client.UserState` reads `GET /v2/users/{id}` and classifies
  known non-active states separately from unavailable/unknown provider answers.
  Active state alone is not proof of contact verification or a human identity.
- `UserProfile` reads human display name and email for presentation. Its result
  does not contain email-verification state, provider organization or a resource
  version. Do not reuse it as provisioning eligibility evidence.
- `POST /v1/iam/members` accepts a subject and any known system role, without a
  provider existence/verification check. Uniqueness preserves the current row
  on duplicate enrollment, but a new POST can recreate an ended membership.
- The Staff form constrains its request to `staff_member` and pins the tenant.
  Its request does not pin the acting subject. UI constraints cannot establish
  the backend's least-privilege, identity or recovery rules.
- `scripts/seed-dev.mjs` creates synthetic humans with `isVerified: true` and a
  fixture password. That is setup, not a user completing contact verification.
  It must not become the production onboarding implementation or acceptance
  evidence for A01.

## Primary provider evidence

The official [Zitadel v4.15.3 UserService protobuf](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/user/v2/user_service.proto)
was inspected as a versioned reference on 2026-10-30. This version matches the
version cited in existing login-client verification notes; it does not establish
the version or service-account capabilities of the current running provider.

- `CreateUser` uses `POST /v2/users/new`; the documented required capability is
  `user.write`. `AddHumanUser` at `/v2/users/human` is deprecated in this snapshot.
  Existing seed-script success on the latter proves neither endpoint support
  nor creation authority for the application's login-client credential.
- `CreateUserRequest` supports an optional immutable, instance-unique `user_id`.
  The response uses `id`; the deprecated creation response uses `user_id`.
  Do not copy seed response decoding into the new client.
- Human creation requires a profile and email. The contract describes default
  verification messages and explicitly depends on configured delivery services.
  An HTTP creation response cannot prove delivery or verified contact.
- `SendEmailCode` uses `POST /v2/users/{user_id}/email/send`; `VerifyEmail` uses
  `POST /v2/users/{user_id}/email/verify`. The provider owns verification codes
  and their effect; an Admin must not manufacture a verified state.
- A caller-chosen provider ID offers a reconciliation basis for uncertain
  creation. The actual create/conflict/read behavior still needs provider tests;
  it is not an established idempotency guarantee from a field definition alone.

## Required next evidence

Identify the supported running provider API and the authorized organization and
creation capability without displaying credentials. Establish the tenant-to-IdP
organization boundary and current Admin authority before exposing creation or
private identity lookup. An existing identity may legitimately serve multiple
tenants; email equality alone must not bind a Staff person or disclose another
tenant's private account.

The implementation design must define durable command receipts, exact uncertain
retry/reconciliation, explicit duplicate/conflict resolution, verified-contact
observation, delivery failure/retry and immutable audit. Provider network work
must remain outside owner database transactions. No success response may imply
that membership, a reviewed Staff link, employment or hospital access exists.

The enrollment migration must also remove the generic POST recovery and
privileged-grant bypasses without silently breaking existing consumers. Preserve
current-row duplicate repair, distinguish first enrollment from recovery and
require the current actor/account/tenant at the write boundary. Governance
appointment/recovery needs its accepted owner process, not ordinary enrollment.

Acceptance uses normal Admin and recipient UI, independent link/access review,
real provider verification/delivery evidence, isolation, stale-account and
source changes, provider outage, duplicates, lost responses and persistence.
No A01/A02 gate is complete from this audit.
