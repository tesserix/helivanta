# Leave reports

Issue: #1037, slice 4 (parent epic #979). ADR: [0021](../../adr/0021-leave-management.md). Plan: [leave management](../plans/2026-10-15-leave-management.md).

## Problem

Legacy report 949 (Leave Report) shows each person's leave by month and type; report 954 (Leave Status Report) lists requests with dates, totals, requester, approver and status. Both filter by a date range, name, employee code and department; 954 also filters by leave name. Their period boundaries and counting rules are not visible, so this slice defines them over the slice 2 ledger and slice 3 requests.

## Scope of this slice

- `GET …/hr/leave-reports/monthly` (949): per staff member, per calendar month inside the period, the dates on leave and the half-days and short-leave minutes taken per leave type.
- `GET …/hr/leave-reports/status` (954): requests whose dates overlap the period, newest filing first, with dates, counted total, filer, decider and status.
- Shell page `/workforce/leave-reports` with both reports.

Not in this slice: attachment status (requests carry no attachments yet; the column is omitted, not faked), CSV/PDF export, scheduled delivery and legacy-specific columns such as "out door duty" or "COVID 19 leave", which are ordinary leave types here and appear as type columns when configured. Weekly-off, compensatory and payroll treatments stay with #1038–#1040.

## Semantics

- Period: `from` and `to` are required inclusive dates, `to` not before `from`, at most 366 days apart and within 2000–2199 (400 otherwise). Dates are hospital calendar dates; no time-zone conversion applies.
- 949 counts only approved requests. A date belongs to the period when the request day falls inside it, so a request crossing the boundary is split exactly; holidays inside a request count zero and are not listed as leave dates. Short leave contributes minutes on its date and zero half-days. For any period that covers a whole request, its half-days equal the negated `consumption` ledger entry for that request; tests assert this reconciliation.
- 954 lists requests in every status whose `start_on`–`end_on` overlaps the period; `total` is the request's counted half-days. `decided_by_subject` is shown for approved and rejected requests only, as legacy "APP. BY".
- Filters on both: `q` (case-insensitive substring of name or employee code, 1–100 characters, wildcards escaped), `staff_id`, `department_id` (HR department master). 954 adds `leave_type_id` and `status`. Unknown values return 400.
- Department and join date come from the enrollment profile, which stays behind `workforce.enrollment.manage`. Holders see `department` and `joined_on` columns; others receive neither, and filtering by `department_id` without the permission is 403.
- Both are keyset-paginated (949 by staff, 954 by filing), at most 100 rows a page. Reads need tenant membership and a hospital grant, like other leave reads; cross-hospital and cross-tenant access is 404. Reasons and notes never appear in reports. Responses are `no-store`.

## Data and security

Migration `0013_workforce_leave_reports` adds two indexes, `workforce_leave_request_days (tenant_id, hospital_id, day)` for 949 and `workforce_leave_requests (tenant_id, hospital_id, start_on, end_on)` for 954, so period scans do not read every request; because a request spans at most 366 days, 954 bounds `start_on` from below to stay on the index; no new tables and no writes, so no audit or outbox events. Forced RLS on the existing tables applies unchanged. Planning load from ADR 0021 (2,000 staff, about 60,000 leave days a year per hospital, p99 under 500 ms) holds with these indexes.

## Verification

Real-PostgreSQL tests: period splitting at month and period boundaries, holidays and short leave, only approved leave in 949, every status in 954, each filter alone and combined, wildcard escaping, empty results, pagination across pages without gaps or duplicates, ledger reconciliation, enrollment-gated columns and the 403 department filter, 400 period and filter validation, and 404 for foreign hospitals. Guards are mutation-checked. Page tests cover empty, failed, filtered and enrollment-restricted states. Full Go/web gates and `scripts/verify-leave-reports.mjs` against the synthetic tenant.
