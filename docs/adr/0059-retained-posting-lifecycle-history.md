# ADR-0059: Retained posting lifecycle history

Date: 2026-11-05. Status: implemented and fully checked locally; CI and normal-role browser
acceptance remain release gates. Not production accepted.

## Context and boundaries

Workforce owns Staff, proposals and immutable lifecycle approval evidence from
migration0085. IAM owns current scoped authority; Organization owns department
structure. Future departmental and Pharma consumers must preserve this evidence
without treating a historical approval as current operating authority. Current
review selectors calculate live eligibility and cannot represent an expired
approval's exact original cutoff observations.

Reuse existing unmeasured budgets:50 peak requests/s,80:20 reads/writes,
2–20KiB ordinary responses,500,000 Staff at12months and1.5million at36months,
p99<500ms and99.9% availability. Bound each history response at128KiB and32
source observations, using existing indexed owner tables and no new service,
cache, worker or deployment cost. These are planning targets, not measured SLOs.

## Decision

Extend only the existing proposal context endpoint with an explicit
`lifecycle_review=recorded` selector, requiring `purpose=review` and
`supervision_context=v1`, exclusive of current management/chain review. Pin the
exact approved proposal version/revision. Existing selectors and envelopes remain
unchanged. A new canonical envelope carries the existing protected context,
retained supervision and nullable immutable lifecycle review. Missing recorded
proof is explicit null, never a reconstruction or an assertion of current access.

The retained record exposes original actor Staff/link/generation references,
review/support assignment versions and epochs, checked time, effective deadline
and ordered source/cutoff observations. No identity files, login subject, current
Staff names or credential data are added. Sources are canonical references,
selected first then original ancestor order. Historical deadlines may have elapsed;
current source employment, department versions and newly shortened cutoffs do not
rewrite or hide the original proof. Incomplete retained proof fails closed.

Current independent posting review authority, current reviewed Staff actor and
session/object scope are rechecked on every disclosure using the existing
review/actor guard. The same contributor/self-review exclusions and tenant/
hospital/purpose RLS apply. Disabled or revoked readers lose access. The selected
Staff may have ended employment; this is history, not an operating admission.
The read and attributable identifier-only audit commit together; audit failure
withholds the entire response. No outbox event is emitted for a read. Bound the
serialized response before sending it and set `Cache-Control: no-store`.

A dedicated SDK method verifies exact selected scope/revision, approval status,
source ordering/uniqueness, original selected absence, provenance and deadline
consistency. It accepts historical time without a freshness check. It cannot
recover a command or supply a current approval examination. The reviewer UI
labels this retained history and explicitly says that current authority is not
established. All account/scope transitions clear private state.

## Failure, migration and acceptance

Malformed/mixed selectors400; missing scope403/invisible object404; stale or
non-approved selection409; unavailable auth/source owners503; incomplete proof,
serialization or audit failure500. No fallback to broad admin or current context.
Postgres/IAM/Staff actor are critical and fail closed. No schema/permission/event
migration is needed; existing immutable tables remain the single owner. Reverting
application code removes the new selector/UI without rewriting retained evidence.
Production rollout still requires the named revision's approval and acceptance.

Rejected: broad history exports, copied departmental Staff tables, adding fields
to strict legacy receipts, reconstructing earlier cutoffs from today's head, or
using approval history as operating authority. Test the real Global API path,
root/chain and legacy-null cases, changed/elapsed sources, foreign/sibling scope,
self/contributor and revoked readers, audit failure, SDK response corruption and
account abort, then normal-role browser UI when its connection is available.
