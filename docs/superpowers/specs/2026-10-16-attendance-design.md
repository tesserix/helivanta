# Attendance

Issue: #1039 (parent epic #979). ADR: [0028](../../adr/0028-attendance.md). Plan: [attendance](../plans/2026-10-16-attendance.md).

## Problem

Hospitals need to know who worked their rostered duty, when they arrived and left, and how much of it is payable, with every exception explained. Raw observations, derived attendance, corrections and finalised timesheets must stay separate so that a figure can always be traced to the punches and rules that produced it.

## Scope of this slice (slice 1)

- A versioned, effective-dated hospital attendance policy.
- Manually recorded, append-only punches with reasoned voids.
- Derived attendance per shift: a hospital day sheet, totals for a day, and one person's range.
- Shell page `/workforce/attendance`.

Not in this slice: biometric devices and file imports (shelved), missing-punch corrections with approval (slice 2, closes #1038 slice 3), finalisation, recalculation versions, period locks and the MIS punch report (slice 3), self-service punching (#1132), payroll rates (#1040).

## Policy

`GET/PUT …/hr/attendance-settings`, `GET …/hr/attendance-settings/history`, same shape as overtime settings: `early_arrival_minutes` (0–240), `late_departure_minutes` (0–720), `early_departure_grace_minutes` (0–240), `short_duty_minutes` (0–720), `duplicate_window_minutes` (1–60), `backdate_days` (0–92), `effective_on`, `reviewed: true`, `expected_version`. Writes need `workforce.attendance.configure` and a locked manager grant; an identical PUT returns the current revision; `effective_on` never moves earlier. A roster date is governed by the revision with the latest `effective_on` on or before it. Late grace comes from the shift revision.

## Punches

| Route                                       | Access                                          | Behaviour                                                                                                                         |
| ------------------------------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/hr/attendance/punches`               | Reader/manager grant                            | Required `staff_id`, `from`/`to` (hospital dates, at most 42 days); newest punch first, keyset pages up to 100; voids included     |
| `PUT …/hr/attendance/punches/{punch}`       | `workforce.attendance.record` + locked manager  | Client-chosen id. `staff_id`, `direction` (`in`/`out`), `punched_at` (RFC 3339), `reason` (1–500). Replay returns the punch; other values 409 `request_reused` |
| `GET …/hr/attendance/punches/{punch}`       | Reader/manager grant                            | One punch with its void, if any                                                                                                   |
| `POST …/hr/attendance/punches/{punch}/void` | `workforce.attendance.record` + locked manager  | `reason` (1–500). A repeat with the same reason returns the punch; a different reason is 409 `punch_voided`                         |

Recording rules: the person is active staff; the instant is at most five minutes in the future and no older than the policy's `backdate_days` before today in the hospital timezone (409 `outside_backdate_window`); a live punch in the same direction within the duplicate window is 409 `duplicate_punch` naming it; no policy in force is 409 `no_attendance_policy`. Reasons are returned only to holders of `workforce.attendance.record`.

## Derived attendance

| Route                                        | Behaviour                                                                                         |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `GET …/hr/attendance?date=YYYY-MM-DD`        | People with a roster allocation, punch or approved overtime that day; keyset pages up to 100       |
| `GET …/hr/attendance-totals?date=YYYY-MM-DD` | Counts of each status and exception for the whole hospital day                                     |
| `GET …/staff/{id}/attendance?from=&to=`      | One person's days, at most 42                                                                      |

A person-day lists its shifts, punches not tied to any shift, leave coverage and approved overtime minutes. A shift reports `status` (`present`, `absent`, `on_leave`, `missing_in`, `missing_out`, `in_progress`, `upcoming`, `unpublished`, `no_policy`), scheduled instants, first in and last out, late, early-departure, worked, expected and payable minutes, `unpaid_half_days`, exceptions (`late`, `early_departure`, `short_duty`, `missing_punch`, `absent`, `duplicate_punch`, `punched_on_leave`, `stray_punch`) and the ids of the allocation, shift revision, policy revision and punches used or ignored. Derivation rules are in ADR-0028.

All three routes accept reader or manager hospital grants and return 400 for invalid dates or ranges, 404 for foreign hospitals and people.

## Events and audit

`helivanta.in.workforce.attendance_settings_changed.v1` (hospital, revision, version) and `helivanta.in.workforce.attendance_punch_changed.v1` (hospital, punch, `recorded`/`voided`). Audit actions `workforce.attendance_settings.changed`, `workforce.attendance.punch_recorded`, `workforce.attendance.punch_voided`, with identifiers and direction only.

## Shell

`/workforce/attendance`: a day sheet with date filter, status totals and per-shift explanation; a punch panel to record and void punches for a person; the policy panel. Empty, loading, failed, invalid-filter and restricted states are explicit and accessible.

## Verification

Go tests against PostgreSQL cover derivation (overnight, DST-free instants, duplicates, missing punches, leave, upcoming, unpublished, no policy), recording rules, replay, RLS and immutability. `scripts/verify-attendance.mjs` walks a synthetic tenant.
