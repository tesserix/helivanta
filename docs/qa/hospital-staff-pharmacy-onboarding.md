# Hospital staff and pharmacy onboarding acceptance

## Checkpoint: 2026-11-13

The existing hospital owner successfully signed in. The live Staff page showed
its hospital name, Main branch and an empty directory. Add staff opened the
hospital-scoped enrollment form. No Staff record, invitation, pharmacy, outlet,
login link or access grant was submitted during this checkpoint. Live form entry
paused when Chrome moved to another website.

The MFA UI candidate uses six separate digit inputs for both verification and
enrollment confirmation, retaining server verification and tenant policy. This
change does not enroll factors or change authentication policy for test staff.
Release and browser evidence must be recorded separately from unit evidence.

## Account and access prerequisites

These are distinct owner-controlled operations:

1. Workforce creates an active hospital Staff directory record.
2. The identity owner provisions an ordinary human in the hospital tenant's
   existing native organization. It must not create another customer tenant or
   grant native organization-management roles.
3. The recipient verifies their contact address and sets their own password.
4. IAM explicitly enrolls the verified account as `staff_member`; this role alone
   grants no business permissions.
5. An independently reviewed Workforce link binds the Staff record and verified
   native subject. Email equality alone is not an account binding.
6. Employment, department posting, pharmacy relationship and scoped access are
   independently checked and approved as required by their owners.
7. The effective-access result, rather than a pending proposal or graph tuple,
   determines whether operations can proceed.

The current Staff account-link UI accepts an existing verified account ID; it
has no new-account email invitation action. The identity owner's existing
`POST /orgs/:id/members` is a management-membership API: its roles are ORG_OWNER,
ORG_USER_MANAGER and ORG_OWNER_VIEWER, with ORG_OWNER as the omitted-role default.
It is unsuitable for ordinary Staff provisioning.

Existing candidate work is [identity PR #22](https://github.com/tesserix/onboarding-service/pull/22)
and [schema PR #1616](https://github.com/tesserix/tesserix-k8s/pull/1616).
Both were still draft and open at this checkpoint. The owner candidate retains
role-free creation and recovery evidence, but its own acceptance record leaves
HMS Admin → recipient → independent reviewer integration open. Do not duplicate
this owner or merge its schema as a side effect of creating test records.
The complete flow and the authorized first-reviewer bootstrap must be verified
before claiming new Staff can sign in and operate. Do not satisfy a bootstrap
gap by assigning broad tenant-admin rights to a test account or self-approving.

## Proposed test assignments

Use four clearly labeled QA directory records, one per user-authorized email.
Keep their email-to-record mapping in private test evidence; do not commit real
contact addresses, passwords, invitation codes or provider tokens. Leave
unnecessary personal and professional details blank.

These are proposed test assignments, not approved or effective grants:

| QA account | Template                                  | Exact scope             | Purpose                                                |
| ---------- | ----------------------------------------- | ----------------------- | ------------------------------------------------------ |
| A          | `pharmacy.department_binding_preparer.v1` | One hospital            | Prepare the pharmacy outlet-to-department relationship |
| B          | `pharmacy.department_binding_reviewer.v1` | The same hospital       | Independently review that relationship                 |
| C          | `supply.department_goods_receiver.v1`     | One pharmacy department | Record receiving within the governed department        |
| D          | `supply.department_goods_reader.v1`       | The same department     | Verify read access and denied receiving writes         |

All require current membership, reviewed Staff/account and employment evidence,
applicable current actor authority and an independent authorized approver.
Relationship preparers/reviewers do not acquire pharmacy operating rights.
Receiving templates do not grant dispensing, prescribing, finance or statutory
pharmacist authority. Inspect current server requirements before each proposal;
this table does not override permission catalogs, product grants or review rules.
Obtain action-time confirmation before applying security-sensitive UI grants.

## Hospital-owned pharmacy journey

1. As owner, create the hospital's pharmacy department through Organization.
2. Register a pharmacy with ownership `hospital_owned` and its first outlet,
   country IN and timezone Asia/Kolkata. Retain command IDs on uncertain writes.
3. Confirm it remains in the current tenant. Independently reviewed department
   binding must identify this hospital, department, pharmacy and exact outlet;
   an affiliation or matching name is insufficient.
4. Inspect the outlet's own access, country configuration and regulatory setup.
   Directory/setup records are not permission to trade. Use genuine customer
   evidence for licences and professional designations; never fabricate it.
5. Prepare and independently review employment, department postings and the
   pharmacy relationships required for QA C and D. Activate only the requested
   scoped assignments after confirmation and verify their effective status.
6. Recipients complete verification/password setup privately, then sign in.
   Confirm their tenant name and scope without entering a workspace ID.
7. Verify A can prepare but cannot approve its relationship. B must not approve
   its own proposal or impersonate another reviewer. C can record receiving;
   D can read the scoped records but cannot record or approve receiving.
8. Check another hospital/department/outlet and tenant are denied; missing
   permissions return 403, hidden/out-of-scope objects 404 and unavailable
   authorization 503. Never interpret missing data as successful empty reports.
9. Exercise expiry/revocation, stale pages, duplicate submission, lost responses
   and a dependency refusal. Rechecking after activation must prove the exact
   scoped authority; a success toast alone is insufficient.

## Evidence needed to close this journey

Record the deployed revisions, attributable command/reference IDs, independent
review decisions, recipient-operated verification/password completion, actual
sign-in, positive and negative scope tests, and redacted UI captures. Unit/CI
fixtures prove their own scenarios; they do not prove delivered invitations,
recipient verification, live reviewer bootstrap or hospital pharmacy acceptance.
The related [identity audit](staff-identity-provisioning-audit.md) and
[Staff tracker](staff-management-progress.md) retain broader open acceptance gates.
