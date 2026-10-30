# Overtime

Issue: #1038, slice 2 (parent epic #979). ADR: [0022](../../adr/0022-duty-adjustments.md). Plan: [duty adjustments](../plans/2026-10-16-duty-adjustments.md).

## Problem

Legacy Duty / OT Adjust records overtime by date and time with an approval state and attribution. Overtime feeds pay, so limits must come from a documented, versioned hospital policy, requested and approved time must be stored apart, and anything rejected, cancelled or revoked must contribute nothing to payable time.

## Scope of this slice

- A versioned, effective-dated hospital overtime policy: daily, weekly and monthly limits in minutes and a filing window.
- Overtime requests for a staff member by work date and hospital wall-clock start/end, with a reason; listing, detail and history; approval of all or fewer minutes, or rejection, by someone other than the filer; cancellation while pending; revocation of approved requests with a note.
- A monthly per-person summary of pending, approved and payable minutes for payroll (#1040).
- Shell page `/workforce/overtime`.

Not in this slice: requests filed by staff themselves (#1132), payroll period locks that would freeze revocation after a pay run (#1040), pay rates and multipliers (#1040), notifications, missing punches (slice 3, after #1039).

## Policy

`GET/PUT …/hr/overtime-settings`, `GET …/hr/overtime-settings/history`, same shape as roster settings: `daily_limit_minutes` (1–1440), `weekly_limit_minutes` (1–10080, at least daily), `monthly_limit_minutes` (1–44640, at least weekly), `filing_window_days` (0–92), `effective_on`, `reviewed: true`, `expected_version`. Writes need `workforce.overtime.configure` and a locked manager grant; an identical PUT returns the current revision; `effective_on` never moves earlier. A request is governed by the revision with the latest `effective_on` on or before its work date (ties by version); the decision records that revision.

## Requests

Hospital scope `…/workforce/hospitals/{hospital}/hr/overtime`.

| Route                                     | Access                                        | Behaviour                                                                                                                                                                                                   |
| ----------------------------------------- | --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/overtime`                          | Reader/manager grant                          | Optional `status`, `staff_id`, `from`/`to` (work date); newest filing first, keyset pages up to 100                                                                                                         |
| `PUT …/overtime/{request}`                | `workforce.overtime.request` + locked manager | Client-chosen id. Body `staff_id`, `work_date`, `start_time`, `end_time` (`HH:MM`; an end at or before the start is next day), `reason` (1–500). Replay returns the request; reuse with other values is 409 |
| `GET …/overtime/{request}`                | Reader/manager grant                          | One request                                                                                                                                                                                                 |
| `GET …/overtime/{request}/history`        | Reader/manager grant                          | Append-only transitions                                                                                                                                                                                     |
| `POST …/overtime/{request}/decision`      | `workforce.overtime.approve` + locked manager | `expected_version`, `decision` (`approve`/`reject`), `approved_minutes` (approve only, 1–requested), `note` (required to reject or approve fewer minutes)                                                   |
| `POST …/overtime/{request}/cancellation`  | `workforce.overtime.request` + locked manager | Pending only; `expected_version`, optional `note`                                                                                                                                                           |
| `POST …/overtime/{request}/revocation`    | `workforce.overtime.approve` + locked manager | Approved only; `expected_version`, `note` required                                                                                                                                                          |
| `GET …/hr/overtime-summary?month=YYYY-MM` | Reader/manager grant                          | Per person with any request in the month: pending requested, approved and payable minutes; keyset pages up to 100                                                                                           |

Statuses: `pending`, `approved`, `rejected`, `cancelled`, `revoked`. Reasons and notes are returned only to holders of either overtime request permission.

## Rules

Checked at filing and again at approval, under the hospital overtime advisory lock:

- The person is active staff of this hospital (409 `staff_inactive`).
- Start and end resolve to single instants in the hospital timezone from the roster settings in force on the work date; DST gaps and folds are refused (400). Duration is 15 to 1440 minutes.
- The work date is no more than `filing_window_days` before today and no more than 92 days after it (409 `outside_filing_window`); the policy and roster settings in force on that date exist (409 `no_overtime_policy`).
- The range overlaps none of the person's active roster allocations (409 `overlaps_duty` with ids) and the person has no approved leave of any unit on the work date (409 `on_leave`); a half day has no instants, so it refuses rather than guessing.
- The range overlaps none of the person's pending or approved overtime (409 `overtime_overlap`; GiST exclusion is the authority).
- Counted minutes are approved minutes of approved requests plus requested minutes of pending ones. With this request counted (requested at filing, the approved figure at decision), the person's work date, week (hospital week start) and calendar month stay within the policy limits (409 `limit_exceeded` naming the period).
- The decider and revoker differ from the filer (403 and a table check).

Payable minutes are the approved minutes of requests whose status is `approved`; every other status contributes zero.

## Data

Migration `0015_workforce_overtime`:

- `workforce_overtime_settings` and `_revisions`: head plus immutable revisions, deferred current-revision key, advancing trigger and limit-order checks.
- `workforce_overtime_requests`: head with work date, instants, `requested_minutes` generated from the instants, `approved_minutes` (set exactly when approved or revoked, never above requested), `policy_revision_id` set at decision, guarded transitions, separation check, exclusion over live ranges, forced RLS.
- `workforce_overtime_events`: append-only transitions with a deferred "complete" check that every version has its event.

Every write commits the change, its event, a `workforce_audit` row and an outbox event (`overtime_changed.v1`, `overtime_settings_changed.v1`) together. Audit rows and events carry identifiers, statuses and minutes, never reasons or notes.

## Verification

Real-PostgreSQL tests: policy versioning and effective dating; filing, approval of all and fewer minutes; daily, weekly and monthly limits counting pending and approved minutes and excluding rejected, cancelled and revoked; overlap with duty, leave and other overtime; filing window; replay and conflicting reuse; stale version; self-decision refused; concurrent approvals that together exceed a limit (one wins); summary totals; cross-hospital and cross-tenant 404, permission 403, reason redaction; event and audit payloads carry no reasons. Guards are mutation-checked. Page tests cover policy, filing, decision, summary, empty, failed and restricted states. Full Go/web gates and `scripts/verify-overtime.mjs` against the synthetic tenant.
