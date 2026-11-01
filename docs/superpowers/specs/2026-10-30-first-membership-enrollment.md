# First membership enrollment

Date: 2026-10-30. Status: implementation candidate for A01/A02/A11, not deployed.
The full Access/RBAC plan remains open. This command enrolls an existing verified
identity; identity creation and contact-verification delivery remain separate.

## Owner, authority and consumers

IAM owns tenant memberships, continuity and enrollment receipts. Zitadel owns
the global account and email verification; Workforce owns Staff identity,
employment and reviewed login links. The Staff form and its API helper consume
this command through the Global API platform destination. Supply and patient
member-list readers retain their existing GET contract. The
[enrollment consumer inventory](../../qa/staff-identity-provisioning-audit.md)
also identifies OPD's legacy doctor-role setup and the unresolved generic POST
migration. No UI-only change closes that bypass.

A current tenant administrator with `iam.member.manage` can enroll an existing
active human with provider-verified email. The acting account and tenant come
from the verified session; exactly one expected-subject and expected-tenant
header must match. No hospital, clinical, payroll or privileged role follows.
An HR title or successful enrollment cannot approve employment or Staff linking.
The recipient can belong to another home identity organization; a known subject
does not authorize disclosure of that person's provider profile.

## Command and history

`PUT /v1/iam/membership-enrollments/:id` takes a UUIDv7 command ID and canonical
`MembershipEnrollmentInput` containing only subject and mandatory reason.
Unknown fields, including caller-supplied role or acting identity, are rejected.
The receipt identifies command, subject, reason, actor, new membership ID,
`staff_member`, continuity epoch 1 and creation instant. Canonical models live
in `contracts/staff-access.json` and generate Go/Zod outputs.

Only a subject with no previous continuity record can first-enroll. Existing
membership is not a new enrollment. Ended membership requires the retained
removal/recovery journey; this command must not reopen it, even if an old grant
event or another role is present. Exact retries return the original immutable
receipt after current actor checks, including after subsequent removal or IdP
disablement. Reuse with another body/actor conflicts. A different command for a
previously enrolled subject conflicts and cannot create duplicate membership.

`GET /v1/iam/membership-enrollments` returns bounded cursor-paginated history;
`GET /v1/iam/membership-enrollments/:id` reauthorizes an exact receipt. Both
require current source administration, matching workspace/account and audited
reads. Another tenant's receipt returns 404. Responses are no-store.

## Transaction and failure boundaries

First check current actor and an exact existing receipt in a tenant transaction.
If uncommitted, obtain one provider eligibility observation outside database
locks. Then serialize the command, recheck the actor's credential watermark and
source administrator membership after lock waits, recheck source continuity and
insert a fresh UUIDv7 ordinary membership. Commit receipt, audit and outbox in
the same transaction. Provider failures are unavailable, not evidence of identity
absence; ineligible identity refuses without a write. A later identity disable
continues to deny protected requests through the current identity guard.

An append-only IAM migration adds forced-RLS, indexed, immutable receipts. A
membership INSERT trigger creates the receipt only for the actual new row after
continuity is established. It requires epoch 1 and exactly one membership;
concurrent legacy/source enrollment cannot fabricate a first-enrollment result.
A direct receipt INSERT cannot manufacture transition evidence. Receipt history
survives membership removal; continuity remains the durable foreign-key parent.
No graph call occurs within the transaction. The existing outbox reconciles the
new incarnation; 202 proves persistence, not graph readiness or hospital access.

Status codes retain the membership command conventions: 400 invalid input,
401 stale/revoked acting credentials, 403 missing current authority, 404 invisible
receipt, 409 stale account/workspace, command conflict, previous enrollment or
ineligible identity, 503 unavailable authority/provider and 500 persistence failure.
An uncertain result is reconciled by retrying the frozen ID/body; never generate
a fresh ID automatically. After reload, review attributable enrollment history
and current membership before another action. Unique first-enrollment evidence
also prevents a new command from duplicating an earlier uncertain success.

## UI and acceptance

The existing-account form requires subject, reason and an explicit ordinary-
membership consequence acknowledgement. Freeze its command/body on submission;
keep input frozen after an uncertain response and retry the same command.
Account/workspace changes remount scoped state; the server rechecks both headers.
Show the receipt's membership ID and distinguish persistence from projection and
operational access. Provide protected enrollment history with loading, failure,
empty and pagination states so a reload can reconcile committed work.

Use existing membership planning bounds: 10 peak commands/s per large tenant,
4 KiB input, 100:1 reads/writes, 100,000 subjects and 1 million receipts at month
12; 300,000 subjects and 3 million receipts at month 36. History uses ordered
tenant/time/ID keysets; source lookup uses tenant/subject continuity indexes.
The command has the existing five-second deadline including provider I/O and
lock waits. These are planning bounds, not measured SLO evidence.

Tests must cover verified first enrollment, required fields, privileged-input
refusal, independent actor/account/tenant boundaries, existing/ended continuity,
exact and conflicting retries, concurrency, immutable evidence, provider outage,
actor changes after provider/lock waits, outbox rollback, protected history and
ordinary-membership-only consequences. Exercise normal Admin/reviewer/recipient
UI and deployed behavior separately from component/server tests. Run generators,
contractdocs, schema replay, affected race/coverage, lint, builds and full CI.

No production migration or rollout is approved. Apply the exact append-only
migration through the owner release process, retaining receipts on rollback;
forward-correct rather than deleting evidence. Rejected: raw POST with no durable
intent, using enrollment as recovery, accepting an arbitrary system role, doing
provider I/O under database locks, or copying provider personal data into receipts.
