# MediConnect and patient API workspace precondition

## Evidence and decision

Live synthetic QA reproduced a 200 account read with a valid tenant-A session and
`X-Helivanta-Expected-Tenant` naming a different membership. The verified tenant
was still enforced; this is an ignored stale-workspace precondition, not evidence
of an unauthenticated or cross-tenant authorization bypass. Pharmacy already
supports this optional precondition. Before proxy/profile work, apply its semantics
to MediConnect and patientmaster's authenticated public request paths.

The shared authn package will expose an opt-in principal extractor. It first uses
existing verified-principal extraction, then accepts an absent header or exactly
one nonzero UUID equal to the verified tenant. Empty, malformed, repeated,
comma-joined, zero and mismatched values return 409 `session_changed`, with
`no-store`, before domain reads or writes. This assertion never selects a tenant,
replaces membership/object checks, or authorizes a hospital. Do not alter global
TenantPrincipal behavior or unrelated modules as part of this correction.

MediConnect owns accounts/claims/notices; patientmaster owns registration and
consent. Their current Go HTTP models remain canonical. No route, payload,
permission, event, migration, cache or deployment topology changes are needed.
Existing callers omitting the header remain compatible. The Global API must
preserve the assertion to the owner. Workload-only current-claim routes are not
user-session operations and retain their exact existing contract.

## Acceptance and failure behavior

Reproduce the ignored header before implementation using real PostgreSQL/NATS
handlers. Assert every registered domain route refuses a mismatch without
producing domain audit/outbox/data changes. Verify a valid create is blocked on a
mismatch, succeeds on a matching assertion and replays without creating duplicates.
Test absent/matching/UUID-equivalent headers, malformed/empty/duplicate/zero
values, unauthenticated callers, and same-tenant peer invisibility. Include actual
Global API forwarding. An unavailable membership or object authority remains
fail-closed; this precondition grants no authority or freshness guarantee beyond
the current request's verified session. A later session switch cannot change an
already admitted transaction's verified tenant.

This adds constant-time UUID parsing and no I/O to existing 2–20 KB requests;
reuse portfolio capacity assumptions (1,000/3,000 peak RPS at 12/36 months), with
no new runtime cost or SLO claim. No persistence change is required; code rollback
restores old header behavior but does not undo accepted writes. Report actual
regressions and verify the live mismatch becomes 409 after GitOps deployment.
