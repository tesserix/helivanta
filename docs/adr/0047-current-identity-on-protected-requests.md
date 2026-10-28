# ADR-0047: Current identity on protected requests

Status: implementation decision, 2026-10-28. Related: #417, #1053 and the
[Staff lifecycle research](../superpowers/specs/2026-10-27-staff-lifecycle-rnd.md).

## Problem and decision

Renewal checks Zitadel state, but an existing signed session can call ordinary
business routes without renewing. A disabled account therefore remains usable
until its cookie expires. This crosses the boundary from a previously verified
identity into current staff, patient and financial data access.

Every protected `/v1` request must pass a mandatory IAM identity guard after the
existing principal/tenant rate limiter and before permission resolution. IAM
owns the subject-global credential watermark; Zitadel owns current identity
state. The guard reads the exact subject's watermark fresh from IAM and checks
the existing login client's current user state. Tenant, membership, object and
employment authorization remain separate checks.

Only POST to the registered self sign-out route skips the upstream identity
read. It can only end the caller's own credentials and must remain available
during an identity outage. It still requires a fresh IAM watermark check and
its existing authentication. Administrator credential revocation does NOT skip
the live identity check: a disabled admin must not change another user's access.
GET, another route or caller-supplied URL/header cannot add an exemption.

Known non-active states follow the [Zitadel v4.15.3 protocol enum](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/user.proto):
inactive, locked, suspend, deleted and initial. Source retrieved 2026-10-28,
SHA-256 `d942c4c093bd31daf27c526a91007edd5dc5989073de5b212444f2ab5a0437bb`.
Unspecified/missing/unknown values remain unavailable, not a known disablement.

A known inactive/deleted subject receives 401 only after the existing IAM
watermark and minimal invalidation event commit together. The watermark uses
the start of the identity observation, so a delayed observation cannot revoke a
later authentication. Reactivating the IdP account does not restore credentials
already invalidated this way. A fresh sign-in is required. An unreadable or
unrecognized identity response, missing dependency or failed revocation commit
returns 503 without executing the business handler. It must not permanently
revoke a credential merely because an external service is unavailable.

## Bounds and failure behavior

One admitted active request adds one indexed IAM read and one bounded identity
GET; inactive observations add one owner transaction and one small outbox event.
No provider response or credential is persisted in a new store. Watermarks
remain O(subjects ever revoked), not O(requests). The existing principal default
is 120 requests/minute with a 20-request burst; tenant limits also apply before
this external call. A 1,000-active-user deployment at that ceiling would imply
2,000 identity GETs/second: provider capacity and actual request latency must be
measured before such a rollout. This is a bound, not observed production load.

The existing client bounds responses to 64 KiB and calls to ten seconds. The
guard gives its entire lookup/revocation sequence a ten-second request budget.
No new latency/availability SLO is asserted. Provider unavailability now blocks
ordinary protected traffic immediately, rather than only renewal; operators must
account for this deliberate fail-closed dependency. Existing authenticated rate
limiting protects the provider. Public login/health routes and worker identities
do not enter this guard.

## Alternatives and limits

Renewal-only enforcement fails against direct cookie replay. A new background
poller or webhook-only solution leaves missed-delivery and idle-user gaps.
Caching active identity state would introduce another authorization window;
it is deferred until a measured load justifies a separately reviewed freshness
contract. Fresh IAM reads prevent missed broadcasts or stale negative cache
entries from restoring a revoked cookie on another replica.

The check governs request admission, not an atomic transaction with the IdP.
An in-flight action admitted before disablement is not retroactively cancelled.
Existing sensitive command guards remain necessary. No rule here disables a
global login merely because employment at one hospital ends. Employment periods,
suspension, legacy grants, cards and cross-owner offboarding remain required.

Migration `0013_iam_identity_guard` widens the existing watermark reason CHECK
to include `identity_inactive`; it adds no table or copied identity data. No
endpoint, role or event payload changes. Rollout is a migration and API image
change through GitOps after validation and its named production approval.
Rollback removes this admission protection and therefore also needs explicit
approval; it does not erase durable watermark history.
