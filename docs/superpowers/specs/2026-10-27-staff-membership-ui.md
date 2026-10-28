# Staff membership enrollment

Implements the membership-entry part of #1053 and #417. Staff records, login
identities, membership, reviewed account links and scoped assignments remain
distinct. The current reviewer QA identity has no IAM membership; there is no
Staff enrollment UI for IAM membership.

IAM owns membership and its outbox. Add an enrollment form to `/workforce/access`
for callers with `iam.member.manage`. It accepts an existing identity subject and
always requests `staff_member`, which has no declared business permissions. It
does not create an identity, link a staff record, or grant hospital access. Keep
the existing assignment controls available under their own permissions.

Use the existing Global API platform route `POST /v1/iam/members`. Move its request
and accepted-ID response into the existing staff-access canonical generator;
preserve the general endpoint's existing role validation and wire contract.
Add the shared optional expected-tenant precondition before membership grants.
This detects a workspace change; verified session context remains authority.

Threat boundary: an administrator supplies an identity identifier to IAM, not
evidence of hospital authority. The server retains membership and
`iam.member.manage` checks and tenant RLS. The UI cannot select a privileged role.
Do not reuse proposal-administration permission as membership authority. A hidden
form is not a server authorization control.

The existing unique tenant/subject/role key makes identical enrollment retries
safe. An ambiguous response retains the exact subject and tenant for retry; a
session conflict requires refresh. A 202 confirms the membership row/outbox
commit, not graph convergence or completed login. The result must say this.
Do not automatically remove membership when closing a form.

Rejected alternatives: broad tenant-admin membership for QA violates least
privilege; direct SQL/API QA provisioning bypasses the requested UI journey;
identity-provider administration from this form duplicates its owner; a second
membership endpoint duplicates IAM's existing command.

Validation: demonstrate a stale expected tenant currently succeeds then fails
without writing; preserve headerless clients and identical replay IDs. Client
tests prove the fixed role, same-origin Global API path, private response handling
and schema validation. Component tests prove permission gates, validation,
successful enrollment and immutable retry. Hosted browser testing covers the
rendered enrollment flow. Live credential changes remain user handoff.

This slice does not complete scoped leave authorization, target-bound RLS,
reviewer queues, supervision, postings or the broader Staff Management backlog.
ORG-AC-01/02 lifecycle and authority separation and ADR-0044 apply; open ORG-D2/D5/D6 decisions remain
open. No migration, new permission, clinical authority or legacy grant conversion.

Browser validation also exposed cached permission data arriving before page
hydration. Keep the initial loading render stable through hydration before
rendering permission-dependent enrollment or assignment controls. Navigation
accepts membership manage, assignment manage or assignment review independently.
