# Beds, admissions, transfers and discharge

[Contract index](../README.md) · **proposal for domain review; slice 1 implemented (below)**

## Implemented: slice 1 (#106, 2026-10-29)

The [slice 1 design](../../superpowers/specs/2026-10-29-bed-occupancy-slice-1-design.md) is in MediCore, migration `0007_medicore`. Everything else on this page is still a proposal.

- **Hierarchy:** ward → room → bed, with immutable codes per hospital, and the bed classes `general`, `semi_private`, `private`, `icu`, `hdu`, `isolation`, `neonatal` and `day_care`. Setup routes need `medicore.bed.configure`, which only tenant administrators hold.
- **Bed state:** `available`, `occupied`, `cleaning` or `out_of_service`. A state moves only with an append-only `medicore_bed_transitions` row for exactly the next version, and the database holds the legal moves. A released or vacated bed always goes to `cleaning` first.
- **Admissions:** direct, into an available bed, with no reservations yet. Intent is `elective` or `emergency`, and the database allows one active admission per patient per hospital.
- **Occupancy intervals:** gist exclusions keep stays from overlapping, per bed and per admission. A transfer closes one stay and opens the next at the same instant.
- **A stay ends** only by release, with a reason: `discharged`, `transferred_out`, `died`, `left_against_advice`, or `other` with a note. The reason records the departure; it is not clinical discharge.
- **Commands:** admit, transfer, release and the bed moves need `medicore.admission.manage` (receptionist and nurse) plus the caller's MediCore hospital grant. All are idempotent by client UUIDv7. Refusals are `409` with `bed_not_available`, `stale_bed`, `already_admitted` or `illegal_move`.
- **Who is in a bed:** `GET …/beds/:bed/occupant` and `…/rooms/:room/occupant` answer `404 no_occupant` for an empty bed or room, and `409 ambiguous` for a room with several occupied beds, never a guess.
- **Events:** `bed_occupancy_started.v1`, `bed_occupancy_ended.v1` and `admission_released.v1`, carrying ids and times; a start also names the bed's ward, room and bed codes and its class (`bed_class`), from which [billing](billing.md#bed-day-charges-197-slice-2) prices the stay.
- **Row security:** the bed tables require the session subject's MediCore hospital grant.

Sources: [#106 bed management](https://github.com/tesserix/helivanta/issues/106), [#107 wards](https://github.com/tesserix/helivanta/issues/107), [#983 inpatient worklists](https://github.com/tesserix/helivanta/issues/983), [#1043 ward hierarchy and discharge gates](https://github.com/tesserix/helivanta/issues/1043), [#96 discharge](https://github.com/tesserix/helivanta/issues/96), [#197 admission billing](https://github.com/tesserix/helivanta/issues/197). Their acceptance criteria are requirements, not evidence of existing APIs.

## Ownership and model

MediCore owns clinical admission, bed allocation and occupancy intervals. FacilityConnect owns maintenance work; housekeeping owns cleaning completion evidence; billing owns rate calculation and ledger posting. The bed owner decides operational availability after consuming their facts. Other products may not edit bed state or occupancy tables.

Proposed records, all scoped to verified tenant and owning hospital:

| Record              | Required contract facts                                                                                        | Invariants                                                                                       |
| ------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Bed                 | `id`, location hierarchy reference, code, class, supported attributes, operational state, version              | Code unique in its agreed scope; physical identity retained when renamed or taken out of service |
| Reservation         | `id`, bed/encounter references, owner, hold expiry, status, version                                            | At most one active hold/occupancy per bed; expired hold cannot authorize occupancy               |
| Occupancy interval  | `id`, bed/encounter, start/end instants, admission/transfer/release reason                                     | No overlapping active bed occupancy; no encounter simultaneously active in two beds              |
| Admission           | `id`, patient registration/encounter, state, admission/discharge instants, current location reference          | Pre-registration is not admission; discharge-ready is not discharged                             |
| Discharge clearance | `id`, encounter, department owner, requirement-definition version, status, source version, decision actor/time | A department decides its own clearance; missing evidence is not approval                         |
| Transition evidence | command ID, expected aggregate version, actor, action, reason, recorded/occurred time                          | Append-only attribution; corrections preserve the original evidence                              |

Hierarchy is facility → building → floor → ward → room → bed. Confirm which hierarchy nodes already belong to the platform facility master before creating tables. Bed classes and physical capabilities follow #106; they are not inferred from a room name. Clinical placement restrictions require explicit reviewed rules, including isolation, neonatal, ICU capability and gender policy where applicable.

## Proposed lifecycle

| From                       | Command/fact               | To             | Gate                                                                                |
| -------------------------- | -------------------------- | -------------- | ----------------------------------------------------------------------------------- |
| available                  | reserve                    | reserved       | Authorized admissions user, compatible placement, future bounded hold expiry        |
| reserved                   | occupy                     | occupied       | Same active reservation/encounter and expected version; clinical admission accepted |
| reserved                   | cancel/expire              | available      | No completed occupancy; expiry recorded as a transition                             |
| occupied                   | release/transfer out       | cleaning       | End the occupancy interval once; do not make the physical bed immediately available |
| cleaning                   | accept cleaning completion | available      | Current task evidence and authorized ward/housekeeping confirmation                 |
| available                  | block                      | blocked        | Reason and authorized ward/clinical actor                                           |
| blocked                    | unblock                    | available      | Original restriction resolved; readiness rechecked                                  |
| available/blocked/cleaning | take out of service        | out_of_service | Safety/maintenance authority; preserve outstanding work and reason                  |
| out_of_service             | return to service          | cleaning       | Maintenance clearance; cleaning/readiness still required                            |

An occupied-bed maintenance concern requires a safe relocation/escalation workflow; it must not remove an occupied patient from capacity by changing a status. Direct admission without a reservation, hold duration/extension, emergency overrides, transfer handover timing and blocked-bed reopening are explicit review decisions. The draft does not authorize automatic clinical placement.

Transfers should be one local MediCore transaction when both beds share its authority: lock source/target in a consistent order, check versions and compatibility, close the source interval and open the target interval at the same effective instant, then append audit and outbox facts. A cross-hospital transfer is a different acceptance/sharing workflow; do not extend this transaction across services.

## Proposed HTTP surface

Names below are candidates under `/v1/medicore/hospitals/{hospital}`; they must not appear in a current-client SDK until approved and implemented.

| Operation                     | Candidate path                                       | Contract                                                                                           |
| ----------------------------- | ---------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Capacity query                | `GET /beds`                                          | Cursor page; ward/class/capability/state filters; source version and freshness                     |
| Bed detail/history            | `GET /beds/{bed}`; `GET /beds/{bed}/transitions`     | Authoritative current state and attributable history under a separate history permission           |
| Bed master                    | `PUT /beds/{bed}`                                    | Reviewed field schema, expected version and durable command identity; no arbitrary state overwrite |
| Reserve/cancel/occupy/release | `PUT /bed-commands/{command}`                        | Explicit operation, bed, encounter, expected version, reason and hold expiry when applicable       |
| Transfer                      | `PUT /transfers/{transfer}`                          | Source/target beds, encounter, both expected versions and effective instant                        |
| Admission/worklist            | `GET /admissions`; `GET /admissions/{admission}`     | Separate admission/discharge date filters and lifecycle states                                     |
| Department clearance          | `PUT /admissions/{admission}/clearances/{clearance}` | Source department, source version, definition version, decision and evidence reference             |
| Final discharge               | `PUT /discharge-commands/{command}`                  | Expected admission version; all applicable clinical/operational gates explained                    |

Proposed command success: durable accepted result with command/resource IDs and resulting version; synchronous completion may use 200/201, asynchronous orchestration 202 with an operation lookup. Review the exact replay status/body before freezing the OpenAPI. Duplicate command with identical canonical input returns its recorded result; different input returns a stable conflict code. Scope deduplication by tenant, hospital, operation and command ID. No cache-only deduplication.

Return 404 for invisible scope/resource; 400 for malformed input; 409 with stable reason codes for stale version, occupied target, expired hold, incompatible placement or incomplete clearance. Error details may identify permitted blockers but must not disclose another patient's identity. A capacity query is advisory; the write transaction is the final allocation authority.

## Integration and failure behavior

Proposed events from #106: `BedAllocated`, `BedOccupied`, `BedReleased`, `BedBlocked`, `PatientTransferred`. Subject names/payload schemas are not yet registered. Before publication, agree metadata-only fields: tenant/hospital, event ID/time, aggregate ID/version, bed and encounter references, interval ID, transition reason code and effective time. Billing needs exact occupancy intervals, tariff version references and corrections; it must not derive charges from a dashboard count.

PostgreSQL availability is critical: allocation fails closed when correctness cannot be committed. A cache outage falls back to a bounded database read. NATS/consumer delay leaves the local bed state correct and downstream views visibly stale. Housekeeping failure leaves a released bed in cleaning with escalation. A billing outage creates charge-pending work; it does not reverse a physical transfer or automatically block clinically required emergency care. SafetyConnect must show stale location and reconcile before relying on a projection for evacuation.

## Acceptance and review decisions

Before implementation, clinical operations, nursing, housekeeping, finance and security owners must review the transition matrix, emergency override policy, pricing/occupancy boundaries, retention and error codes. Select the existing platform hospital/facility master and define service objectives; the platform sizing envelope is not a measured bed-board refresh guarantee.

Required synthetic cases: two concurrent allocations yield one winner; expiry racing occupancy; repeated transfer after timeout; stale source/target versions; incompatible isolation placement; release followed by unfinished cleaning; one tenant/hospital cannot discover another's capacity/patients; revoked actor cannot finish a command; delayed/duplicate events do not double-charge; corrected interval preserves billing reconciliation; discharge blockers retain progress; disaster recovery reconstructs occupancy and pending handoffs.

Roll out additively within MediCore first. Shadow/reconcile any capacity projection against the owner before use. Migrating an existing live bed board requires approved mapping, cutover, recovery and named single-writer ownership; routing back to stale occupancy is unsafe. No migration or rollout is authorized here.
