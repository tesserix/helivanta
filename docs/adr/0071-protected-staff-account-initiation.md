# ADR-0071: Protected Staff account initiation

Date: 2026-11-13. Status: implementation candidate; runtime acceptance open.

## Context and boundary

An owner needs to create an ordinary account before that account can be enrolled
and independently linked to Staff. The identity owner already retains immutable
creation, contact reservation, dispatch, native identity binding and recovery
evidence. IAM owns current membership and credentials; Workforce owns Staff and
reviewed links. A matching email is never a Staff/account binding.

Use ORG-AC-03 and the accepted ORG-D3/D4/D7 defaults in ADR-0044. The initiating
actor has governance authority through current tenant_admin membership and
iam.member.manage. Its exact scope is the verified tenant, independently of
hospital or pharmacy employer. New accounts receive no membership or product
roles. Reviewing a Staff link and granting operating duties remain separate
authorized actions. This does not waive their independent-review requirements.

Planning assumptions reuse the identity-owner envelope: 50/150 tenants at
12/36 months, 100 accounts/tenant, 10 peak requests/second, 90:10 reads:writes,
and a 4096-byte contact body. There is no new service, table or standing runtime
cost. Target availability is 99.9%; lookup p99 target is 2 seconds, with bounded
30-second creation/delivery requests because the owner has a 25-second deadline.
These are design targets, not measured production results.

Threats include an unauthenticated caller, another tenant's owner and a revoked
or stale session. Protect contacts, identity receipts and dedicated machine
credentials at both the Global API/IAM boundary and the identity-owner boundary.

## Decision

Expose protected IAM creation, exact-command read, fresh observation and
invitation delivery. Require the normal Global API business route, verified
tenant/current identity, iam.member.manage, current tenant-admin membership,
current credential watermark, and exact expected-tenant/subject assertions.
The server supplies the tenant, product and actor. Contact input cannot carry
any of those authority fields. Reads outside the frozen tenant/initiator return
404; missing authority 403; revoked credentials 401; uncertain authorization 503.

The browser generates a UUIDv7 creation command once, freezes its submitted
contact/reason and retains that command across errors. The identity owner
persists the immutable intent before native dispatch and reserves the contact.
It is the sole durable command owner. HMS does not create a second identity
ledger or regenerate a command after timeout. An explicit delivery has its own
UUIDv7, retained on uncertain retry. Only the recipient receives the proof link.

HMS records an attributable command attempt before external I/O, without contact
payloads. It rechecks current owner/credential authority and records disclosure
before returning a receipt. No database transaction or row lock spans HTTP.
Authority loss during an already-admitted native command may leave a role-free
identity recorded by the identity owner; HMS denies disclosure/access and retains
that command for recovery. It must not erase history or repeat native creation.
An unavailable owner, failed audit, expired session or incomplete binding fails
closed. Native credential setup never grants IAM membership automatically.

The existing role-free creation, private recipient setup and explicit ordinary
membership APIs compose with independently reviewed Staff links and scoped
assignments. Account initiation works for hospital and independent pharmacy
tenants without inventing a hospital for the latter.

Rejected: using the native management-membership API (its default is ORG_OWNER),
copying identity-owner tables into HMS, holding a local transaction during HTTP,
and combining account creation, membership, Staff linking and grants into an
unrecoverable multi-owner command. There is no saga or new waiting worker:
each explicit command commits in its existing owner, and recipient verification
is a separate capability operation.

## Contract and recovery

The identity-owner staffcontract Go models and generated Zod contracts are
canonical. Browser contact input omits actor_subject; unknown fields, duplicate
keys and case aliases are refused. Source-native validation remains authoritative.
Read, observation and delivery accept no arbitrary query or body data.

A confirmed creation receipt is distinct from prepared/dispatched, verified
contact, configured password, ordinary membership, reviewed Staff linkage and
effective operating access. Unknown results stay visibly unresolved. Recovery
uses the retained command; no email lookup adopts an existing identity.

No new application schema is needed. Rolling back the consumer retains owner
evidence and stops new initiation; it never deletes native users or restores
revoked memberships. Dedicated product credentials come from the existing
OpenBao namespace-bound reader and exchange only for the onboarding audience.

## Required acceptance

Use the actual identity-owner API/ledger in CI with the checked canonical SQL,
plus real HMS RLS/membership/audit storage. Simulate only external Zitadel/email.
Verify duplicates, changed intent, lost response, foreign tenant/initiator,
stale workspace/account, revoked/removed owner, invalid contact and dependency
or audit failures. Assert no native management role or IAM membership is created.

UI tests must prove retained immutable retry IDs, explicit recipient delivery,
no exposed proofs and no account-ID adoption by email. Live acceptance must use
the deployed Global API, authorized QA contacts, recipient-operated private
setup, explicit ordinary membership, independent Staff linking and actual
positive/negative pharmacy access. Passing initiation tests do not close those
remaining gates.
