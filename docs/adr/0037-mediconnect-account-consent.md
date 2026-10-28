# ADR-0037: Tenant-reviewed account notices and consent receipts

Status: implementation decision, 2026-10-25; legal content/policy activation is not approved by this ADR.

## Context and scope

#37 and #552 still need separate consent, proxy authority, expiry/majority handling,
rights-request routing and patient-facing disclosure. Registrar identity review
is deployed at `cccd5ff`; it creates none of these authorities. This slice completes
tenant notice publication/retirement and authenticated account receipt/withdrawal.
It does not implement clinical, guardian or proxy consent. Existing account
creation remains compatible and does not require a new receipt.

MediConnect owns both tables; all clients use Global API. Reuse the modular Go
owner, PostgreSQL/RLS, audit and transactional outbox. A separate service/cache
would add failure domains without a distinct scaling or ownership need. Valkey
remains the platform cache, never consent authority. Consult HTTP/integration/schema
contracts, IN-ID-06/07 and the identity/consent reference. No legal text, age,
retention, processing exception or consent eligibility is inferred from an issue.

Capacity assumptions, not measured demand: use the portfolio's 1,000 peak RPS,
80:20 read/write and 50 tenants at 12 months (3x by 36 months). Budget this slice
100 peak RPS and p99 <300 ms excluding auth, 99.9% monthly service availability.
At 100k accounts and three ~1KB receipts/year, raw receipts grow ~300MB/year before
indexes/audits. At 20 locales x 10 notice versions x 50 tenants x 8KB, notice text
is ~80MB. JSON bodies cap at 16KB for publication and 4KB for account commands;
lists use existing keyset pagination, max 200. No extra runtime infrastructure.

## Decision and invariants

A tenant member with `mediconnect.notice.manage` may publish an immutable UUIDv7
notice with purpose, locale, version label, title, plain text and a UUIDv7 retained
review-reference attestation. This permission has no default staff-role grants;
the existing reconciler grants tenant admins all declared permissions. Publication
is an attributable tenant action, not automated legal approval. No notices are
seeded. Published notices may be retired, never edited/reactivated/deleted.
Purpose is restricted to account_processing, communications or analytics; these
are account declarations and never treatment, clinical disclosure or parental
consent. Notices and receipts do not start processing or send messages.

The server hashes the exact canonical notice content and supplies that hash to the
client. An authenticated account owner accepts a specific notice/hash explicitly,
using a client-generated receipt UUIDv7. The immutable receipt pins all notice
identity metadata through the original retained notice. Cross-account and tenant
reads return 404. Admin notice authority confers no receipt-read authority.

Grant locks account then notice, preventing retirement racing a new acceptance.
One active receipt per account/purpose; another grant conflicts until explicit
withdrawal. Replaying the same receipt ID/body returns its current state and never
restores withdrawal, even after notice retirement. Reads expose notice status;
an active historical receipt is not proof a retired notice permits future use.
No caller may infer processing/object authority, legal capacity or guardian consent
from this receipt alone. Future processing owners must define and check current
purpose, eligibility, scope, consent and policy before acting.

Withdrawal is terminal and version-guarded, with identical retry returning the
same result and no new event. It commits with audit/outbox; no clinical record is
deleted. Notices remain readable for evidence. Retirement leaves historical
receipts intact and prevents new grants. Consumers receive only opaque receipt,
account and notice IDs plus the allowlisted account-purpose label; no notice text,
evidence reference, subject, demographics or channel destination enters events.
No downstream authority or processing completion is claimed without a consumer.

## Failure, migration and validation

Critical authentication/authorization, PostgreSQL or audit failure fails closed.
Notice absence yields 404; retired or changed hash yields 409. NATS outage leaves
committed outbox delivery pending without changing receipt authority. No network
call occurs inside a data transaction. Duplicate/mismatched keys and concurrent
grant/withdraw/retire must be tested against real PostgreSQL. Response bodies are
no-store and errors exclude raw text/SQL/evidence. A tenant publisher or another
subject must not read or alter someone else's receipt.

Additive migration 0006_mediconnect uses bounded DDL locks, immutable-history
triggers, forced RLS and scoped indexes. No destructive down migration. Previous
binaries leave these new tables intact; do not build a processing consumer that
can run without current withdrawal checks. Test prior-schema migration/repeat,
public HTTP forwarding, audit/outbox rollback, concurrency, exact text/hash/locale,
retired notice behavior and pagination; inspect large-table query plans.

Remaining work is explicit: approved notice text and eligibility policy, clinical
and proxy grants with current source checks and revocation, majority timers,
patient rights intake/officer handling, patient-facing released-record APIs, and
UI/mobile work (currently excluded by the user's API-only scope). No issue closes
until its complete acceptance criteria are met.
