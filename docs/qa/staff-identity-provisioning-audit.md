# Staff identity provisioning audit

Date: 2026-10-30. Scope: A01/A02 prerequisites for the full Access/RBAC plan.
Status: source and provider-contract findings; implementation and runtime
acceptance remain open. Related issues #417 and #1053 remain open.

## Current implementation

- IAM's `loginclient.Client.UserState` reads `GET /v2/users/{id}` and classifies
  known non-active states separately from unavailable/unknown provider answers.
  Active state alone is not proof of contact verification or a human identity.
- The follow-up `MembershipEligible` reader now binds one provider response to
  the exact subject, active human type and verified email, returning no private
  profile. The candidate recovery command consumes it through explicit bootstrap
  wiring. Unknown/ambiguous proof fails unavailable. This is local implementation,
  not deployed provisioning or acceptance.
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
version cited in existing login-client verification notes. Subsequent read-only
local discovery confirmed the running `helivanta-dev-zitadel-1` image tag
`v4.15.3`, image ID
`sha256:2356d646340724b3d843c024d589314adb9b54dbd6452ebc34008b011e023eff`,
and `/app/zitadel -v` output `zitadel version v4.15.3` through Docker context
`colima-helivanta-tests`. No Docker context was changed. This establishes the
local provider version, not production version or creation authority.

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

## Local response-shape observation

Read-only GETs using the existing local application credential confirmed the
matching synthetic Admin and reviewer subjects, active state, human email and
`isVerified: true` on 2026-10-30. Only field names and boolean results were
recorded; no email, token or password was displayed. This did not create users,
verify contact, change credentials or establish provider creation permission.

The recovery regression reproduced actual 202 for an active unverified human
before the new gate, versus required 409. Its corrected test uses the real
PostgreSQL HTTP command and an HTTP provider fixture. It is distinct from a
recipient completing a real provider verification workflow.

## Enrollment consumer inventory

A fresh source inventory on 2026-10-30 distinguishes mutation callers from member
list readers. The original checkout's Graft index is stale for several moved IAM
tests; actual worktree files were verified with exhaustive path search.

| Consumer                                                         | Actual use                                                                   | Required migration consequence                                                                                                                                  |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `packages/api/src/staff-membership.ts` and Staff membership form | Generic POST with `staff_member`, tenant header only                         | Move to account-pinned, reasoned first-enrollment command with identity proof and exact uncertain retry                                                         |
| `e2e/tests/staff-access.spec.ts`                                 | Normal enrollment UI; explicitly waits for generic POST response             | Update the real UI journey alongside the successor, including verification/recovery boundaries                                                                  |
| `e2e/tests/support/opd.ts`                                       | Generic POST adds `doctor` to an existing synthetic clinician                | Privileged clinical role provisioning is a real compatibility dependency; do not silently remove it or describe a setup fixture as accepted clinical assignment |
| Supply organization grant/support editors                        | GET member-list options                                                      | Preserve list response/pagination and permissions; these are not enrollment mutations                                                                           |
| Patient pharmacy-lookup access editor                            | GET member-list options                                                      | Preserve this separate read consumer; no enrollment privilege follows from lookup authority                                                                     |
| IAM, Global API and journey Go tests                             | Ordinary and privileged enrollment/setup, current-role and projection checks | Separate attributable setup from production workflows; retain denial, projection, replay and scope assertions                                                   |

No external-consumer absence is established. A successor form alone cannot close
the generic POST's privileged-grant or historical recovery bypass. Coordinate
that contract migration and the actual privileged-assignment journey explicitly.

## Required next evidence

Verify the authorized organization and creation capability without displaying
credentials. The local version is now confirmed; production version and
configuration remain a separate release prerequisite. Establish the tenant-to-IdP
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
