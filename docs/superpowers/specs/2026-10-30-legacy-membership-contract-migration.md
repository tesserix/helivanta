# Legacy membership removal contract migration

Date: 2026-10-30. Status: local implementation candidate, not approved for
production. Part of A02/A11; the full A01–A12 objective remains open.

## Reason and compatibility impact

The published `DELETE /v1/iam/members/:subject/:role` identifies a role key, not
the immutable membership incarnation. Once the original row is removed and a
replacement exists, an old retry is indistinguishable from a new removal intent.
Neither an optional tenant header nor a server-generated command ID can recover
the caller's original target. The real PostgreSQL regression demonstrated that
the old request deletes the replacement.

The candidate retains the authenticated route and its membership/permission
middleware, but refuses mutation with HTTP 409:

```json
{
  "error": "membership_command_required",
  "message": "Use PUT /v1/iam/membership-commands/:id with the current membership ID, epoch and reason"
}
```

Responses use `Cache-Control: no-store`. The optional expected-tenant assertion
still returns `409 session_changed` first when invalid. An absent target receives
the same migration response as an existing target. The refusal neither queries
the target nor deletes rows, advances epochs, creates receipts or publishes
events. Missing membership or route permission remains 403 before the handler.
This is an explicit breaking migration from the old 202 contract; retaining the
URL does not make it backward compatible.

## Successor and consumers

IAM owns both contracts. Normal Staff administration already uses the new
candidate/history UI and Global API platform destination. The caller selects the
current membership ID and epoch, freezes a UUIDv7 command ID, reason and target,
and sends `PUT /v1/iam/membership-commands/:id` with matching expected tenant and
subject headers. Exact retries return the original immutable receipt. Recovery
starts from a removal receipt and creates a new ordinary `staff_member` only.
Do not translate legacy retries automatically into fresh commands.

The source inventory found no first-party application, API-package, script or
browser E2E raw-DELETE caller. Test consumers are in IAM, Global API and the IAM
membership journey. Migrate positive removal journeys to the new command while
retaining the original replacement-survival assertion and explicit legacy-error
checks. The reviewer-governance withdrawal fixture is an attributable source
transition for a denial test; it does not demonstrate a supported governance
removal or recovery journey.

Existing member-list readers and enrollment POST consumers remain distinct and
supported in this candidate. POST's unrestricted role and historical reenrollment
behavior require coordinated correction and an identity-owner workflow. The
[0019 admission candidate](2026-10-31-membership-recovery-admission.md) now refuses
implicit restoration after retained final removal; first enrollment, privilege
expansion and external migration remain open.
This migration does not settle that gap or generation-bound employment eligibility.

## Review and sunset gate

No runtime rollout or sunset date is approved by this document. Before release,
Platform, Security and consuming-owner review must explicitly record the breaking
behavior, repository migration evidence, external-consumer inventory and rollout
communication. External consumers are not proven absent. The open #4 proposal
requests a minimum twelve-month external sunset; no accepted security exception
or external sunset agreement is evidenced here. Resolve that conflict explicitly
rather than asserting that a repository search approves immediate retirement.

The successor must pass full CI and normal Admin/reviewer/recipient browser QA,
including uncertain retries, current-actor lock waits and dependency failures.
Release only an exact reviewed source and persisted migration set after its
specific production approval. Historical production still serves the old route
until that release; local green tests are not deployed acceptance.

Rollback to the old destructive handler reintroduces the demonstrated defect.
Prefer a forward correction retaining the refusal and durable receipts; do not
erase evidence, restore old epochs or replay raw removal intent against a new
row. No new SQL is required by the refusal itself. The successor's existing
0015–0017 IAM migrations remain a separate deployment gate.
