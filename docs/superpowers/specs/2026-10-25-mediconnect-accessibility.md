# MediConnect accessibility preferences

Issue #37 requests accessibility preferences. This API-only slice adds optional
account-owned presentation settings to the existing versioned preferences API.
It does not implement UI rendering, patient/profile grants or consent. Existing
session membership and subject-plus-tenant RLS remain the authority boundary;
there are no cross-module reads, new services, cache entries or events.

## Contract and consistency

Account responses gain `accessibility` with `text_size` (`system`, `large`,
`extra_large`), `contrast` (`system`, `high`) and `motion` (`system`, `reduced`).
All default to `system`, expressing no override of the device setting. No medical
condition or inferred disability is captured. Persisted settings are still
personal preferences: return only to the verified owner, audit without values,
and continue `no-store` responses.

The existing PATCH requires its original fields and expected_version. An optional
`accessibility` object must include all three valid fields; omission or null
preserves existing accessibility values atomically. An explicit all-system object
resets the overrides. Unknown/nested extra fields, invalid types and enum values
return 400. Stale versions return 409; retries after an ambiguous timeout must GET
and reconcile. Old callers never reset settings they did not submit.

One guarded UPDATE commits all supplied values and its audit together. Optional
columns use SQL COALESCE against the current row, not a previously read copy,
so a legacy request cannot overwrite new preferences with a stale snapshot.
Accounts still require PostgreSQL; audit/database failure rolls back. Valkey is
unchanged and does not become account authority.

## Storage, scale and rollout

Migration 0003_mediconnect adds three constrained, non-null text columns with
constant defaults, bounded lock/statement timeouts and no new index. PostgreSQL
16 constant defaults avoid a rewrite. Existing accounts are immediately readable
as all-system, and previous application binaries continue their named-column
reads/updates. Code rollback retains data; no dropping down migration is provided.

No new service, round trip or query fan-out is added. Existing 4096-byte request
limit and unique tenant/subject lookup apply. Added response payload is under 100
bytes and added setting values under 30 bytes per row. Traffic, growth forecasts
and a measured latency SLO are not established by this change; no scaling claim
is made. Reuse the current transaction instead of a separate preferences store or
endpoint, which would add independent version and recovery semantics.

## Acceptance and release

Observe a failing public API test before implementation. Test round trip,
all-system reset, legacy/null preservation, invalid nested inputs, absent account,
peer/tenant isolation, concurrent writers, audit outage and upgrade compatibility
from migration 0002. Exercise Global API → real owner HTTP → PostgreSQL. Run race
coverage, architecture/contract checks, build/vet/lint, full CI, and a migration
rehearsal before promoting the exact reviewed image through Kargo. Keep public
repository visibility and automatic promotion disabled. Public unauthorized probes
prove routing/rejection only, not production authenticated account use or UI
accessibility. #37 remains open for its other criteria.
