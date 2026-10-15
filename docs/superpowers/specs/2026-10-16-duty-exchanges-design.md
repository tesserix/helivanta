# Duty exchanges

Issue: #1038, slice 1 (parent epic #979). ADR: [0022](../../adr/0022-duty-adjustments.md). Plan: [duty adjustments](../plans/2026-10-16-duty-adjustments.md).

## Problem

Legacy Duty / OT Adjust records a requester and an adjuster with their dates and shifts and an approval state. Published (frozen) rosters cannot change today except by unfreezing, which needs `workforce.roster.correct` and reopens the whole roster. A swap or cover between two people needs its own consented, separately approved and attributable path that leaves the rest of the roster published.

## Scope of this slice

- `swap`: requester allocation A and counterpart allocation B change hands. `cover`: the counterpart takes A and the requester keeps no duty in its place.
- Filing with attested counterpart consent and a reason; listing, detail and history; approval or rejection by someone other than the filer; cancellation while pending; revocation of approved exchanges before the duty starts.
- Approval and revocation write `exchanged` roster revisions; the roster grid marks those allocations.
- Shell page `/workforce/duty-exchanges`.

Not in this slice: consent given by staff themselves (#1132), credential conflicts (#1108), notifications, overtime (slice 2) and missing punches (slice 3, after #1039).

## API

Hospital scope `…/workforce/hospitals/{hospital}/hr/duty-exchanges`.

| Route                                     | Access                                    | Behaviour                                                                                                                                                                                                                                          |
| ----------------------------------------- | ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/duty-exchanges`                    | Reader/manager grant                      | Optional `status`, `staff_id` (either side), `from`/`to` (duty date overlap); newest filing first, keyset pages up to 100                                                                                                                          |
| `PUT …/duty-exchanges/{exchange}`         | `workforce.duty.request` + locked manager | Client-chosen id. Body `kind`, `requester_allocation_id`, `counterpart_staff_id`, `counterpart_allocation_id` (swap only), `counterpart_consent` (must be `true`), `reason` (1–500). A replay returns the exchange; reuse with other values is 409 |
| `GET …/duty-exchanges/{exchange}`         | Reader/manager grant                      | Exchange with both allocations (roster, date, shift, area, instants) and, after approval, the replacement allocation ids                                                                                                                           |
| `GET …/duty-exchanges/{exchange}/history` | Reader/manager grant                      | Append-only transitions                                                                                                                                                                                                                            |
| `POST …/{exchange}/decision`              | `workforce.duty.approve` + locked manager | `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject)                                                                                                                                                                   |
| `POST …/{exchange}/cancellation`          | `workforce.duty.request` + locked manager | Pending only; `expected_version`, optional `note`                                                                                                                                                                                                  |
| `POST …/{exchange}/revocation`            | `workforce.duty.approve` + locked manager | Approved only, before every exchanged duty starts; `expected_version`, `note` required                                                                                                                                                             |

Statuses: `pending`, `approved`, `rejected`, `cancelled`, `revoked`. Reasons and notes are returned only to holders of either duty permission.

## Rules

Checked at filing, again at approval and at revocation, under the hospital roster advisory lock:

- Allocations are active, primary, in frozen rosters of this hospital, belong to the named people (requester ≠ counterpart), and start after now. A swap's two allocations differ.
- Each allocation is in at most one pending or approved exchange (partial unique index; 409 `allocation_in_exchange`).
- After the change, no receiver overlaps their other duty (409 `allocation_overlap` with conflicting ids; the GiST exclusion remains the authority), already holds a primary on that date (409 `primary_taken`), or has approved leave on that date (409 `on_leave`); no giver keeps additional allocations on that date without a primary (409 `orphan_additional`).
- Both people have active employment status (409 `staff_inactive`).
- The decider differs from the filer (403 and a table check).
- Revocation needs the replacement allocations still active in frozen rosters and all starting after now (409 `exchange_started` or `roster_changed`).
- Unfreeze and discard of a roster with pending exchanges return 409 `pending_exchanges`.

## Data

Migration `0014_workforce_duty_exchanges`:

- `workforce_duty_exchanges`: head with guarded status transitions and version; `decided_by_subject <> requested_by_subject`; forced RLS.
- The head carries the original allocation ids, the replacement ids set on approval and the restored ids set on revocation; composite foreign keys tie each id to the person who holds it, and a deferred check requires replacements to copy date, shift, area and instants from the original removed in the same revision.
- `workforce_duty_exchange_holds`: one row per original allocation with a `held` flag kept true only while the exchange is pending or approved; a partial unique index lets an allocation be held by one exchange at a time.
- `workforce_duty_exchange_events`: append-only transitions.
- Roster revisions gain action `exchanged` with an `exchange_id`; the head trigger lets a frozen roster advance its version only for that action, and the allocation trigger admits inserts and removals in a frozen roster only within an `exchanged` revision of the current version.

Every write commits the exchange change, its event, any roster revisions, a `workforce_audit` row and outbox events (`duty_exchange_changed.v1`, `roster_changed.v1`) together.

## Verification

Real-PostgreSQL tests: swap and cover approval moving duty without changing shift, area or instants; roster stays frozen and its history shows the `exchanged` revision; each refusal rule above; replay and conflicting reuse; stale `expected_version`; self-approval refused; concurrent approvals of exchanges sharing an allocation (one wins); revocation restoring duty and refusing after start; unfreeze refused while pending; cross-hospital and cross-tenant 404, permission 403, reason redaction; event and audit payloads carry no reasons. Guards are mutation-checked. Page tests cover filing, decision, empty, failed and restricted states. Full Go/web gates and `scripts/verify-duty-exchanges.mjs` against the synthetic tenant.
