# Attendance corrections

Issue: #1038 slice 3 (missing-punch corrections), #1039 slice 2 (parent epic #979). ADR: [0028](../../adr/0028-attendance.md) layer 3. Plan: [attendance corrections](../plans/2026-10-16-attendance-corrections.md).

## Problem

A forgotten, mistimed or wrong punch changes payable hours. Recording or voiding it directly (slice 1) is one person's say-so. Payroll needs a request that a second person approves, which keeps the original punch intact and links the change to its before and after values, its reason and both actors. Rejected and cancelled requests must never change attendance.

## Scope of this slice

- Correction requests of three kinds: `add` (a missing punch), `void` (a punch that should not count) and `replace` (a mistimed or wrong-direction punch: void it and add the corrected one).
- Filing, approval, rejection and cancellation, with an append-only event trail per request.
- On approval, the corrective punch and/or void is written in the same transaction, using the existing punch and void tables, linked to the request.
- Shell: a corrections board on `/workforce/attendance` plus "Request correction" entry points on the punch ledger.

Not in this slice: revoking an approved correction (file a new correction against the corrective punch instead, which keeps every row append-only), finalisation, recalculation versions and period locks (slice 3), self-service requests by the staff member (#1132), and linking staff records to login subjects, so the person corrected may still approve their own correction when they hold the permission. Separation of duties covers the filer and the decider only.

## Permissions

- `workforce.attendance.correct` files and cancels requests.
- `workforce.attendance.approve` decides them, and never on a request the holder filed. This is enforced by a `CHECK` on the row as well as in the handler, and returns 403.

Both also need a locked manager grant on the hospital. Reasons and notes are returned only to holders of `record`, `correct` or `approve`.

## Routes

| Route                                                        | Access    | Behaviour                                                                                                                                                                                                  |
| ------------------------------------------------------------ | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/hr/attendance/corrections`                            | Reader    | Filters: `status`, `staff_id`, `from`/`to` (affected local date). Newest filing first, keyset pages up to 100.                                                                                             |
| `PUT …/hr/attendance/corrections/{correction}`               | `correct` | Client-chosen id. `staff_id`, `kind`, `direction` and `punched_at` (add/replace), `target_punch_id` (void/replace), `reason` (1–500). A replay returns the request; other values are 409 `request_reused`. |
| `GET …/hr/attendance/corrections/{correction}`               | Reader    | The request with before (target) and after values and the applied punch id.                                                                                                                                |
| `GET …/hr/attendance/corrections/{correction}/history`       | Reader    | Event trail, newest first.                                                                                                                                                                                 |
| `POST …/hr/attendance/corrections/{correction}/decision`     | `approve` | `expected_version`, `decision` (`approve`/`reject`), `note` (required on reject).                                                                                                                          |
| `POST …/hr/attendance/corrections/{correction}/cancellation` | `correct` | `expected_version`, optional `note`.                                                                                                                                                                       |

Transitions follow the overtime pattern. A stale `expected_version` returns 409 `stale_version`, and a request that is no longer pending returns 409 `request_state`. Replaying the step that produced the current version, by the same actor with the same note, returns the request unchanged.

## Rules

Filing:

- `add` needs direction and time and no target. `void` needs a target and no direction or time. `replace` needs all three. A shape mismatch returns 400.
- The target is a live punch of the same person. A voided target returns 409 `punch_voided`. A target that already has a pending or approved correction returns 409 `correction_exists`, enforced by a partial unique index.
- A new time is a whole minute, no more than five minutes in the future, and pins the roster timezone and attendance policy of its local date (the same rule as punches). No policy in force returns 409 `no_attendance_policy`.
- The affected dates (the new punch's and the target's) are inside the back-dating window of their own policy on the filing day. Otherwise the request returns 409 `outside_backdate_window`.
- A live punch in the same direction within the duplicate window returns 409 `duplicate_punch`; a `replace` ignores its own target. A pending add or replace in the same direction within the window returns 409 `duplicate_correction`.
- Corrections fix past facts, so unlike direct punches they do not require the person to still be active.

Approval re-checks, under the hospital attendance lock:

- The target is still live.
- There is no duplicate.
- A policy is in force.
- The back-dating window is measured from the filing time, not the approval time, so a request filed in time can still be decided. Period locks (slice 3) will bound late approvals.

Approval then inserts:

- For `add` and `replace`: a punch with `source = 'correction'`, `correction_id`, the corrected direction and time, the request's reason, and the approver as `recorded_by_subject`.
- For `void` and `replace`: a void of the target with `correction_id`, the request's reason, and the approver as `voided_by_subject`.

Derived attendance needs no change. Corrective punches are ordinary live punches, and voided targets drop out. Rejected and cancelled requests never write a punch or void, so they cannot reach payable hours.

## Structural enforcement (migration `0017_workforce_attendance_corrections`)

- `workforce_attendance_corrections`:
  - CHECKs for kind shape, status/decision consistency and separation of duties.
  - An opening trigger enforces pending at version 1, the pins, the target's ownership and liveness, and the window.
  - An advance trigger allows one step from `pending`; filing facts are immutable.
  - A partial unique index on the target.
  - `workforce_prevent_removal`.
- `workforce_attendance_correction_events`: append-only. An event-matches trigger and a deferred completeness constraint make every version need its event.
- The deferred completeness constraint also requires an approved request to have its punch and/or void.
- `workforce_attendance_punches` and `workforce_attendance_punch_voids` gain a nullable unique `correction_id` foreign key:
  - `source IN ('manual','correction')`, with `correction_id` present exactly for corrections.
  - Their insert triggers are replaced: a correction-linked row must match an approved request of the right kind (same person, direction, time and target), and its window is measured from the request's filing time.
- Forced RLS on both new tables, writable only with `app.workforce_attendance = 'authorized'`.

## Events and audit

- `helivanta.in.workforce.attendance_correction_changed.v1` carries `{hospital_id, correction_id, status, version}`.
- Approval also publishes `attendance_punch_changed` for each corrective punch and void.
- Audit rows carry identifiers, kind, direction, status and version only. Reasons and notes stay out of audit rows and events.

## Tests

Real PostgreSQL module tests:

- Each kind's filing rules and every 409.
- Replay and id reuse.
- Approval writes exactly the linked rows, and derived attendance reflects them.
- Reject and cancel write none.
- Separation of duties at both handler and `CHECK` level.
- Stale version.
- Cross-tenant and foreign-hospital 404.
- Redaction.
- Direct SQL attempts against each trigger, under RLS: forged correction punch, update, delete, wrong target, out-of-window.
- Event payload allow-list.

Frontend:

- Vitest for the board, form and actions.
- A contract generator check.

Synthetic verify script: `scripts/verify-attendance-corrections.mjs`.
