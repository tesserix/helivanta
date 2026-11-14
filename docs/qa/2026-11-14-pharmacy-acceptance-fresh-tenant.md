# Pharmacy production acceptance on a fresh signup tenant

- **Issue:** #1672 (unblocks the production journeys of #1391 and #1467)
- **Date:** 2026-11-14, written against main `0a1fad14`
- **Decision:** the user chose a replacement acceptance tenant created through normal signup (option B). There are no real customers yet, so no legacy-tenant migration is needed now.

## Why the retained tenant cannot be used

Every protected request asks the identity owner (onboarding-service) for the caller's current status. The owner resolves a tenant only when its registry row is active, was created by Helivanta and names the Zitadel organisation `helivanta-<tenant UUID>` (`internal/onboarding/login_target.go:52`, pinned `d91aa99f102c`). Anything else is not found (404). Helivanta treats any non-200 as unavailable (`backend/internal/customeronboarding/identity_projection.go:54`), so the API answers `503 identity_unavailable` and does not revoke the credential. Sign-in has the same rule: an organisation scope must be exactly `helivanta-<UUID>` (`backend/internal/modules/iam/loginclient/tenant_session.go:37-38`).

The retained synthetic tenant predates ADR-0070, and its accounts belong to a numeric native organisation. ADR-0070 says identities like this are not silently migrated: that needs a separately reviewed migration. Its tenant, membership, file, stock and financial history are left untouched and unused.

## Rules

- Synthetic data only: no real patients, prescribers, licences or payments. Licence numbers and files are clearly fake, for example `SYN-DL-20B-0001` and a PNG stamped `SYNTHETIC`.
- Every step is a normal product flow, run by the user. No database edits, no relabelled organisations, no rewritten IDs, no identity bypass and no broad tenant-admin shortcut.
- Never paste passwords, codes, cookies or tokens into the run sheet or an issue. Record IDs and outcomes only.
- If a step fails, stop and record it on #1672 with the time, the build SHA and the visible error. Do not work around it.

## Accounts in the new tenant T1

| Key  | Created by                                                        | Roles                                                 |
| ---- | ----------------------------------------------------------------- | ----------------------------------------------------- |
| OWN1 | Public signup (the tenant owner)                                  | Tenant owner, plus `pharmacy_owner` granted in step 4 |
| OWN2 | "New staff account" in Workforce > Staff access, by OWN1 (step 3) | `staff_member`, then `pharmacy_owner` (step 4)        |

The licence review is refused when the reviewer entered the licence (`four_eyes`), so OWN1 and OWN2 must be different people's accounts with different email addresses.

## Steps

### 1. Sign up the pharmacy

1. Open `/signup` in a private window and create an **independent pharmacy** with a synthetic business name such as `SYN Pharmacy 2026-11-14`.
2. Verify the email, set a password and enrol MFA through the normal pages.
3. Open the workspace and save its workspace-specific sign-in link.

Record: the tenant ID shown after sign-in. Expected: the dashboard loads, and `GET /api/v1/iam/me/permissions` returns 200, not 503. This alone proves #1672's acceptance for a supported tenant.

### 2. Confirm isolation

Using OWN1's session, request a resource ID from the retained legacy tenant. Expected: 404, with nothing revealed.

### 3. Create OWN2

1. As OWN1, open Workforce > Staff access (`/workforce/access`) and use **New staff account**: first name, last name, a second work email and a reason. Expected: "Account created. No access has been granted."
2. Use **Send private setup email**. The recipient verifies the address, sets a password and enrols MFA.
3. As OWN1, enrol the account as a staff member with the membership form on the same page.

Expected: the account belongs to T1's `helivanta-<tenant>` organisation, and it signs in with no business permissions yet. The staff-account flow (#1667, #1668) has not been accepted in production before, so a failure here is a finding against that chain. Record it on #1672 and stop.

### 4. Grant the pharmacy owner role

The access UI has no pharmacy-owner grant yet (F-8), so this is an API call from OWN1's signed-in tab:

```
POST /api/v1/iam/members   {"subject": "<OWN1 subject>", "role_key": "pharmacy_owner"}
POST /api/v1/iam/members   {"subject": "<OWN2 subject>", "role_key": "pharmacy_owner"}
```

Expected: 202 each, with a member ID. Before granting, the API asks the identity owner whether the subject belongs to T1's organisation; a subject from another organisation gets 409 and nothing is granted. A 409 `employment_scope_required` means the account already has an employment record and needs scoped assignments instead: stop and record it.

### 5. Set up the pharmacy and outlet

Run SET-01, SET-02, SET-05 and SET-07 from the [counter acceptance plan](2026-11-09-pharmacy-counter-acceptance-plan.md#6-setup-and-access-set) as OWN1. Then grant OWN2 `editor` on outlet A through the API (SET-08).

### 6. Licence journey (#1391)

Run SET-19 to SET-23 with OWN1 as the person who enters the licence and OWN2 as the reviewer:

1. OWN1 records the licence (SET-19), then tries to review it and gets `four_eyes` (SET-20).
2. OWN1 uploads the synthetic PNG and waits for upload_pending, then scan_pending, then available (SET-23).
3. OWN2 opens the licence, downloads the evidence (audited) and approves it with a reason (SET-21).

When these pass, record them on #1391 and close it.

### 7. Prescription-sale enforcement (#1467)

Only after step 6 passes: merge and deploy #1477 through the normal release. Then re-run sections 8 and 9 of the counter plan in T1.

## Not covered

- Migrating any tenant created before ADR-0070. If a real customer ever exists on one, it needs its own issue and a reviewed migration owned by the identity owner.
- Tidying up the retained tenant. It stays as it is.
