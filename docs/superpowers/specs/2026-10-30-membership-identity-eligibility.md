# Membership identity eligibility

Date: 2026-10-30. Status: local implementation candidate for A01/A02/A11.
This strengthens the undeployed recovery command; it does not complete the full
Access/RBAC plan or authorize creation in an arbitrary identity organization.

## Evidence and ownership

The current recovery command calls `UserState`; an active service account or
active human without verified contact can therefore pass its identity gate.
Zitadel owns the identity and verification; IAM owns the tenant membership.
The existing presentation-profile reader intentionally drops verification and
must not be repurposed as authority. See the
[provisioning audit](../../qa/staff-identity-provisioning-audit.md).

Read-only requests using the existing local application login-client credential
on 2026-10-30 confirmed `user.userId`, `user.state`, `user.human.email.email` and
`user.human.email.isVerified` on the designated synthetic Admin and reviewer.
Both subject IDs matched and both contacts were verified. No identity, credential,
membership, delivery configuration or provider permission was changed. This is
response-shape evidence, not proof of account-creation authority or fresh contact
verification performed by a recipient.

## Contract and authority

The current authenticated tenant administrator still requires
`iam.member.manage`, current source administration, fresh credentials, matching
expected tenant/account, a retained removal receipt and the current epoch.
The candidate additionally requires a single provider observation binding the
exact requested subject to an active human with a nonempty verified email.
Membership can belong to a guest from another home organization; home-org or
email-domain equality is neither a permission grant nor a reason to expose a
profile. The eligibility reader returns only a boolean, never the email or name.

Known inactive/deleted identities, machine identities and unverified contact
return ineligible (409 through the existing command contract). Missing or
conflicting proof, unknown state, malformed response, provider refusal/outage
or an unwired reader returns unavailable (503). Never manufacture verification,
interpret unknown evidence as eligible or expose the provider payload in errors.

This checks an account and its email; it does not prove the Staff person's
identity/background, approve employment or create a reviewed Staff/login link.

The reader validates exact subject binding and rejects duplicate proof keys,
ambiguous human/machine types and renamed verification fields. Protobuf JSON may
omit a false verification boolean; omission remains ineligible. The shared HTTP
success decoder must consume one complete bounded JSON document, including EOF;
a valid prefix must not hide a second document or an oversized body.

Provider I/O remains outside database transactions. The write subsequently
rechecks actor/source/epoch after locks. This is an observation, not a distributed
transaction: protected request identity guards continue to deny later disablement.
Exact retries of an already committed recovery return the retained receipt after
current actor checks without another provider call or membership insertion.

Bootstrap explicitly wires the same identity client into renewal, presentation
and eligibility consumers. A construction test prevents an omitted setter from
silently making every new recovery unavailable.

## Delivery and limits

Use the existing membership budgets: up to 10 peak commands/s per large tenant,
4 KiB command input, 100:1 reads/writes and bounded indexed membership state.
Eligibility adds one GET only for an uncommitted recovery, within its existing
five-second command deadline; the client body bound remains 64 KiB. No new
service, datastore, migration, permission, event or paid component is required.
These are planning bounds, not measured latency/availability acceptance.

The stronger eligibility gate changes only an undeployed candidate. Production
rollout still requires exact source/migration approval and normal Admin/reviewer/
recipient acceptance. Keep source history and receipts on rollback; do not undo
verification policy by resurrecting old authority. The legacy generic enrollment
POST still permits first enrollment and privilege expansion outside this workflow
and must be migrated before A02/A11 acceptance. The
[0019 admission candidate](2026-10-31-membership-recovery-admission.md) now blocks
implicit restoration of retained ended membership; it does not close those other
legacy paths.

Test the actual recovery HTTP command against a provider response reporting an
active but unverified human, proving refusal without membership/epoch/outbox
changes. Exercise matching human verification, machines, inactive/deleted users,
unknown/ambiguous/malformed proof, provider failures, source authorization and
exact replay. Run client/IAM/bootstrap and relevant architecture, build, vet,
lint and canonical contract checks. Record browser and deployed evidence separately.

Rejected: two independent state/profile reads can mix observations; storing
copied profile data expands disclosure without proving current verification;
accepting seed-script flags replaces user verification with administrator input;
using home organization as business authority rejects legitimate external staff.
