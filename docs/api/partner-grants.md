# Supplier access grant preparation — unreleased

Shared IAM records immutable **requested intent**, not an effective grant.
Canonical models are in [`contracts/partner-grants.json`](../../contracts/partner-grants.json).
Run `node scripts/generate-partner-grants.mjs` and its `--check` mode to generate
Go and Zod inputs. Current registered routes enter through Global API.

| Path under `/v1/iam`        | Methods  | Authority                                                                            |
| --------------------------- | -------- | ------------------------------------------------------------------------------------ |
| `/partner-grant-drafts`     | GET      | Current access-management governance authority; signed bounded cursor pagination     |
| `/partner-grant-drafts/:id` | PUT, GET | Same current authority; UUIDv7 identifies one immutable proposal and recovery record |

The verified session supplies the owner tenant and actor. Expected tenant and
subject preconditions bind the browser workspace. The server independently checks
current governance membership, credential revocation and account interval under
locks; route permission alone does not authorize the operation. Ordinary Staff
and other tenants cannot read these private proposals. These routes do not grant
clinical, pharmacy, distributor, financial or hospital operating authority.

Input selects exactly one source owner (`pharmacy` or `supply`), organization,
outlet/premise, relationship and revision, one named hospital account or published
hospital group, a bounded read-only template, both requested jurisdictions,
validity and reason. Initial jurisdictions are `IN-MH` and `IN-OD`; this is rollout
scope, not a legal applicability decision. Group and source references are UUIDv7;
hospital/tenant references preserve their existing UUID identities. Recipient and
relationship references remain **unverified** at this preparation stage.

Each result explicitly contains:

```json
{
  "status": "pending_verification",
  "operating_access_enabled": false
}
```

Validity is positive and at most 366 days. A new proposal must expire in the
future. Historical GET and exact PUT recovery remain authorized independently
of expiry. Recovery binds the original actor, canonical intent and membership
incarnation; rejoining an administrator account cannot replay an old incarnation
as a new preparation. Different intent under the same identifier is `409`.

Audit and creation commit atomically; audit failure withholds both writes and
read disclosure. This metadata-only preparation emits no external offer, graph
tuple, stock movement, invoice or message that a trade consumer could treat as
authority. The new table has forced tenant/purpose RLS and rejects updates,
deletes and truncation. Its additive migration retains immutable evidence on
application rollback; no destructive down migration is supported.

The SDK checks owner tenant, proposal identity, actor, exact input and timestamp
precision before confirming recovery. After an ambiguous save, the UI retains
the identifier and input, freezes editing, and offers GET recovery or the same
PUT. An inaccessible owner record is `404`, absent governance authority `403`,
revoked credentials `401`, changed workspace/intent `409` and unavailable
authority/audit `503 partner_grant_draft_unavailable`. Responses are `no-store`.

UUID-valued intent compares by UUID identity, including the owner's canonical
lowercase representation. Account subjects and free text remain case-sensitive.
The canonical generator supplies the UUID field set used by SDK recovery.

The [implementation specification](../superpowers/specs/2026-11-07-independent-supplier-grants.md)
and [delivery ledger](../design/staff-and-rbac/independent-collaboration-delivery.md)
retain the remaining bilateral, recipient, disclosure, stock-owner, financial and
release requirements. No draft is consumed as trade authority. Production still
needs canonical product/machine onboarding and actual participant/evidence review.
