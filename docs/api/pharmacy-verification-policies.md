# Administrative pharmacist-verification policy drafts

Refs [#169](https://github.com/tesserix/helivanta/issues/169) and accepted
[ADR-0077](../adr/0077-pharmacist-verification-policy-authority.md).

This candidate implements administrative preparation only. It is compiled
`unreleased`; it does not activate a clinical policy, grant clinical authority,
permit dispensing or enable emergency supply. The complete pharmacist-verification
workflow remains unfinished.

PharmaConnect owns this append-only ledger. Its only consumers in this change are
the account-bound SDK and the unreleased outlet prescription workspace. There is
no cross-product write, patient projection, stock movement or financial effect.

## Scope and routes

All routes use the Global API and require `pharmacy.access.manage`, verified tenant
membership and a current grant to the named outlet under the named pharmacy.
Writes require outlet editor access; reads require outlet access. The transaction
locks and rechecks that grant. A client-supplied pharmacy, outlet or header never
grants authority. Identity/authorization failures fail closed.

The root is
`/v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/verification-policy-drafts`:

| Method | Suffix                         | Behavior                                                |
| ------ | ------------------------------ | ------------------------------------------------------- |
| PUT    | `/:policy/revisions/:revision` | Retain a new revision, or recover the identical command |
| GET    | `/:policy/revisions/:revision` | Read one exact revision                                 |
| GET    | `/:policy/revisions`           | Cursor-paginated revision history for one policy        |
| GET    | (root)                         | Cursor-paginated saved revisions across outlet policies |

Both IDs are canonical UUIDv7. The root collection contains **revisions**, not one
latest row per policy. Collections use the platform keyset/cursor contract and
require explicit loading of subsequent pages. The SDK requests at most 50 rows.

The SDK sends `X-Helivanta-Expected-Tenant` and
`X-Helivanta-Expected-Subject`, aborts on account transitions and rejects late
responses. If supplied, empty, duplicate, malformed or mismatched preconditions
return `409 session_changed` before storage. Headers constrain the authenticated
principal; they do not select a principal. Responses are `Cache-Control: no-store`.

## Canonical intent and receipt

`contracts/pharmacy-verification.json` owns the generated Go/Zod inputs, receipts
and SQL validation predicate. Run `node scripts/generate-pharmacy-verification.mjs`
and its `--check` mode after changing the source. The SQL function uses UTF-16 text
lengths so supplementary characters have the same bounds as JavaScript and Go.

A synthetic input is:

```json
{
  "expected_version": 0,
  "label": "Synthetic proposal for qualified review",
  "second_check_required": true,
  "review_valid_for_seconds": 600,
  "query_response_target_seconds": 1200,
  "reason": "Synthetic administrative proposal"
}
```

These intervals are **test data, not clinical recommendations**. New drafts have
no default clinical choices. Both intervals must be positive integral seconds
representable as a Go duration. The expected version and second-check choice must
be explicit. Labels and reasons are trimmed and bounded. Unknown fields, including
approvers, verified-person IDs, effective state and emergency/authority assertions,
are rejected. Server-supplied receipts always say `state: draft` and
`clinical_authority: not_evaluated`.

Each successful revision increments the policy version atomically. A revision ID
identifies one command within a tenant/outlet. Its normalized input, policy and
preparing subject are retained. Identical retry returns the original receipt even
after later revisions. Changing actor, policy or intent for the same command, or
submitting a stale expected version, returns `409 verification_policy_draft_conflict`.
The client must recover an uncertain outcome using the same revision ID; it must
not silently generate a new command to retry. A new revision returns 201; exact
replay returns 200.

The ledger has composite tenant/outlet ownership, forced RLS, database-enforced
sequential versions and immutable update/delete/truncate guards. Writes append
attributable audit records in the transaction. Reads, history and replay recovery
must record audit before disclosure. Audit failure returns 503 without revealing
the stored draft. Missing or inaccessible resources return 404; missing declared
permission returns 403; unavailable dependencies return 503. The SDK validates
all returned rows and binds saved receipts to the exact normalized intent.

## Delivery and remaining gates

Migration `0076_pharmacy_verification_policy_preparation` is additive and
transactional, with bounded lock/statement timeouts. Its registration causes the
next release to run it even while HTTP routes remain unreleased. Keep its PR draft
until hosted PostgreSQL tests, required checks and the specifically named live
migration approval are complete. GitOps/Kargo deployment and actual acceptance
are separate evidence; local unit tests do not establish database correctness.

The UI offers named drafts, revision history, explicit choices and stable-command
retry/recovery. Account IDs are not display labels. It stays behind the compiled
unreleased boundary. No effective-policy consumer is introduced.

Still required for the full issue: authoritative physical-person/account binding,
current registration and local privileges, qualified independent clinical-policy
approval, separate publication by a different verified person, review decisions,
independent second check, prescriber resolution, source/fulfilment concurrency
checks and enforcement at the active stock/financial commit. Owner, staff or
registration status cannot substitute for these controls. Emergency supply remains
disabled without its separately qualified policy.
