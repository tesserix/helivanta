# Finalised timesheets, recalculation versions and period locks

Issue: #1039 slice 3 (parent epic #979). ADR: [0028](../../adr/0028-attendance.md) layer 4. Plan: [attendance timesheets](../plans/2026-10-16-attendance-timesheets.md).

## Problem

Derived attendance (slice 1) changes whenever a punch, correction, leave, overtime or roster input changes. Payroll and month-end reports need figures that stop changing at an agreed point. They also need every later change to be explicit, attributable and visible as a new version. The legacy screens offer "recalculate" and "finalise" controls. MIS report 929 lists every punch with who recorded it, when, its source and whether it is still active.

## Scope of this slice

- **Timesheet periods.** A hospital closes a date range of up to 31 days. A closed period is stored with a full snapshot of the derived days, per-person totals and hospital totals.
- **Recalculation versions.** Recalculating a period writes a new numbered timesheet and keeps every earlier one. Each day row is marked `added`, `changed`, `unchanged` or `removed` against the previous version.
- **Period locks.** While a period is finalised, punches, voids and corrections dated inside it are refused by the database. So is an attendance policy revision that would take effect on or before a finalised day. Reopening a period with a reason lifts the lock. Finalising it again writes the next version.
- **Punch report** (MIS 929): a hospital-wide punch listing over a date range, newest punch first, with job category, source, recorder, and void state and actor. Reasons are never included.
- **Shell:** a Timesheets section on `/workforce/attendance` and a punch report view.

Not in this slice:

- Department and roster-area filters, because staff have no department attribute yet (#1035).
- CSV/PDF export (#1066 report catalogue).
- Automatic drift alerts between a finalised version and the live sheet. A recalculation shows the difference explicitly.
- Payroll consumption of the snapshot (#1040).
- The legacy day-off report 934, which is covered by the roster day-off allocation views.

## Permissions

`workforce.attendance.finalise` finalises, recalculates and reopens periods. It also needs a locked manager grant and the hospital attendance advisory lock. Reading periods, timesheets and the punch report needs a hospital grant. Notes on periods are returned only to holders of `finalise`, `record`, `correct` or `approve`, because a reopening reason can name a person's circumstances.

## Routes

All routes are under `/workforce/hospitals/{hospital}/hr/attendance`.

| Route                                                  | Access     | Behaviour                                                                                                                                                                                                                                                     |
| ------------------------------------------------------ | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/periods`                                        | Reader     | Newest period first, keyset pages up to 100.                                                                                                                                                                                                                  |
| `PUT …/periods/{period}`                               | `finalise` | Client-chosen id. `from`, `to` (hospital dates, `to` before today in the hospital timezone, at most 31 days) and `note` (0–500). Creates the period and timesheet 1. A replay with the same values returns the period; other values are 409 `request_reused`. |
| `GET …/periods/{period}`                               | Reader     | The period and its current timesheet's totals.                                                                                                                                                                                                                |
| `POST …/periods/{period}/finalisation`                 | `finalise` | `expected_version` and `note` (1–500). Writes the next timesheet. On a finalised period this is a recalculation; on a reopened period it locks the period again.                                                                                              |
| `POST …/periods/{period}/reopening`                    | `finalise` | `expected_version` and `note` (1–500). Only from `finalised`.                                                                                                                                                                                                 |
| `GET …/periods/{period}/history`                       | Reader     | Transition trail, newest first.                                                                                                                                                                                                                               |
| `GET …/periods/{period}/timesheets`                    | Reader     | Every version with totals and change counts, newest first.                                                                                                                                                                                                    |
| `GET …/periods/{period}/timesheets/{timesheet}/people` | Reader     | Per-person totals in staff-list order, keyset pages.                                                                                                                                                                                                          |
| `GET …/periods/{period}/timesheets/{timesheet}/days`   | Reader     | Day rows, newest day first, with the derived day as finalised. Filters: `staff_id`, `change`.                                                                                                                                                                 |
| `GET …/punch-report`                                   | Reader     | `from`/`to` (at most 31 days), optional `staff_id`. Newest punch first.                                                                                                                                                                                       |

Transitions follow the corrections pattern:

- A stale `expected_version` is 409 `stale_version`.
- Reopening a period that is not finalised is 409 `request_state`.
- Replaying the step that produced the current version, by the same actor with the same note, returns the period unchanged.

## Finalisation rules

Checked under the attendance lock, in this order:

1. The range is valid: `from` ≤ `to`, at most 31 days, inside 2000–2199. Otherwise 400.
2. A roster timezone is in force on `to`, and `to` is before today in that timezone. Otherwise 409 `period_not_closed`. A day still in progress can never be locked.
3. No other period covers any of the days. Otherwise 409 `period_overlap`, naming the period. A per-day table with a primary key enforces this.
4. No pending correction affects a day in the range, by its own date or by its target's date. Otherwise 409 `pending_corrections` with the count.
5. Every derived shift in the range is settled. A shift that is `upcoming`, `in_progress`, `unpublished` or `no_policy` blocks finalisation with 409 `period_not_ready` and the count per status. Thresholds and unpublished plans are never guessed into a locked record.

The snapshot holds every person who has a derived day in the range: a rostered shift, approved overtime or a stray punch. For each day it stores the derived day exactly as the live sheet returns it, plus its scalar figures. It stores per-person totals and hospital totals, including status and exception counts in contract order with zeros included.

Reconciliation is structural. A deferred constraint trigger refuses the commit unless:

- the timesheet totals equal the sum of its people rows;
- each person row equals the sum of that person's day rows;
- each day row's figures equal those inside its stored day.

Change marking compares each day with the previous version's row for the same person and date. Days that existed before but have no derived content now are written as `removed` rows with zero figures.

## Lock rules

A day is locked when a `finalised` period covers it. Statement triggers refuse, with constraint name `workforce_attendance_period_locked` (409 `period_locked`):

- inserting a punch whose local date is locked;
- inserting a void of such a punch;
- filing a correction whose date or target date is locked;
- inserting an attendance policy revision effective on or before any finalised day.

Approving a correction writes through the punch and void tables, so a late approval into a locked day fails the same way. The request stays pending until the period is reopened.

Leave, overtime and roster changes are not locked. They belong to their own approval workflows, and the snapshot already records the figures that were finalised. They are adopted only when someone with `finalise` recalculates, which records the changed days in a new version.

Attendance-owned writes (punches, voids, corrections and policy) take the same advisory lock as finalisation, so they are serialised with it. Leave, overtime and roster inputs are read as committed at the moment each is read. A change that races a finalisation is adopted by the next recalculation.

## Data

Migration `0019_workforce_attendance_timesheets`:

- **`workforce_attendance_periods`**: range, status (`finalised`/`reopened`), version, current timesheet, creator. Updates go through a trigger that only lets the version advance by one.
- **`workforce_attendance_period_days`**: one row per day. Its primary key on `(tenant, hospital, day)` refuses overlap.
- **`workforce_attendance_period_events`**: append-only transitions with actor and note.
- **`workforce_attendance_timesheets`**: numbered versions with totals and policy revisions.
- **`workforce_attendance_timesheet_people`** and **`workforce_attendance_timesheet_days`**: append-only.

All tables use forced RLS with the attendance write flag. Every write adds an identifier-only `workforce_audit` row and the outbox event `helivanta.in.workforce.attendance_period_changed.v1` (`hospital_id`, `period_id`, `status`, `version`, `timesheet_id`).

Size: 2,000 staff over 31 days is about 62,000 day rows of about 1 KB each per version, written in batches of 1,000 inside the finalisation transaction. Reads are keyset pages on `(timesheet, sort key, id)`.

## Tests

Each test must be able to fail:

- Finalise, then list people and days, and check that the totals reconcile row by row.
- A punch, void, correction filing and policy revision inside the locked period each return `period_locked`, and punch and void inserts through raw SQL fail at the trigger. Approval cannot reach a locked day through the API, because pending corrections block finalisation; the trigger on the punch and void tables covers it. After reopening they succeed. Finalising again marks the changed day `changed`, and an emptied day `removed`.
- Overlap, an unclosed period, pending corrections and unpublished or in-progress shifts each block finalisation.
- A policy revision effective inside a finalised period is refused.
- Stale version, wrong state and replay behaviour.
- A foreign hospital returns 404, and a missing permission returns 403.
- The reconciliation trigger refuses mismatched totals.
- Punch report filters, ordering, paging and the absence of reasons.
