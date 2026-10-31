# Original credential and current identity across file I/O

Status: reproduced source defect; implementation in progress. A01–A12 remains
open. Follow ADR-0047/0049 and the current-file-permission design.

## Evidence and ownership

Real IAM/SQL/OpenFGA/Global API file journeys return private bytes with 200 when,
after the initial identity check, the credential is revoked, the upstream account
becomes inactive, or identity status becomes unavailable during storage I/O.
Expected results are 401, 401 and 503 respectively. Permissions/hospital grants
remain valid, so refreshing only authorization does not address these cases.

Authn owns signature/expiry, original principal and idle/revocation checks. IAM
owns the current identity guard, its uncached watermark check, upstream status,
inactive-account revocation/audit/outbox and current role admission. Workforce
owns files, purpose/release bindings and read/scan audit. No new identity master,
credential storage, role, endpoint, model or schema is introduced.

## Implementation boundary

Preserve existing entry authentication behavior by extracting its existing check
into one shared helper. Bind a request-local refresh callback to the original raw
credential and principal. Refresh must verify that credential again, refuse any
change to subject/tenant/auth_time/idle deadline, and retain original request
identity. Never use a replacement header/cookie to borrow a fresher account.
The raw credential stays only in the ephemeral request closure and is not logged,
serialized, exported or persisted.

IAM registers the same current-identity guard as a second request-local check
after successful entry admission. Its reusable check must not call `c.Next`;
normal middleware dispatch stays separate. Each invocation receives a new bounded
10-second context and preserves unknown/unavailable 503, inactive 401 plus durable
watermark, and the direct watermark read after upstream observation. This direct
read prevents cached negative revocation results from preserving an old token.
Sign-out keeps its existing narrow provider-independent behavior.

`authn.Refresh` fails closed without a credential refresh capability or with a
malformed registered check. It runs credential verification then any identity
check installed at entry; it adds no weaker path around an entry guard. Existing
production V1 identity wiring remains required. Private Workforce file phases
call it before refreshing permissions and before owner SQL work. No network call
is added inside an owner transaction.

## Failure and recovery

Expired/idle/revoked/changed identity returns no-store 401. Unknown/unavailable
identity or revocation source returns no-store 503, never fabricated active state.
Known inactivity preserves IAM's attributable revocation/outbox. Reactivation
cannot revive the old credential. Denial releases no bytes or filename, records
no successful read and queues no new scan after a quarantine write. Existing
quarantine cleanup, immutable retry and protected release rules remain intact.

This repeats current observations across storage I/O; it is not a distributed
lock on an external IdP or a substitute for generation-bound employment/scope
locks. All other modules and lifecycle consumers still need their own acceptance.

## Verification and operating limits

Use the real signed-session/source/graph/gateway file journeys, with deterministic
third-party identity-state observations. Add signature/idle/revocation, original
identity binding, missing/malformed refresh and identity-callback failure tests.
Preserve all existing authn and IAM identity-guard tests, including outbox failure
and reactivation. Run affected race suites, vet/build/lint and contract freshness.
The previous benefits/tax fixture route/audit corrections retain actual RLS and
all denial assertions; swapped operation permissions must still fail the tests.

Existing 5-second permission and 10-second identity budgets remain bounded by the
request context. Additional checks add latency but no new service/cache/storage
cost class; do not claim a measured file SLO. Reuse existing source workloads and
measure before release. No production rollout or rollback is authorized here.
