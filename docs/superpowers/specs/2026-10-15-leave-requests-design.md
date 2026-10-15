# Leave requests and approvals

Issue: #1037, slice 3 (parent epic #979). ADR: [0021](../../adr/0021-leave-management.md). Plan: [leave management](../plans/2026-10-15-leave-management.md).

## Problem

Slices 1 and 2 give every staff member a reviewed structure and a ledger balance, but nothing consumes it. HR cannot record that a nurse is on leave, an approver cannot see whether the leave is affordable or clashes with a published roster, and roster population still books people who are away. Legacy Leave Requests shows type, dates, half-day/short-leave choice, status and approver; its counting and overlap rules are not visible and are defined here.

## Scope of this slice

- HR files a request on a staff member's behalf under `workforce.leave.request`; a different principal approves or rejects under `workforce.leave.approve`.
- Units: full days over a date range, a single first- or second-half day, or a single-day short leave in minutes.
- Counting excludes regular and government holidays from the pinned calendar; optional holidays count as working days.
- Overlap exclusion, clubbing, probation eligibility and balance checks, all re-checked at approval.
- Approval writes consumption to the ledger; cancelling approved leave writes its reversal. Both are idempotent.
- Roster population skips staff-days covered by approved full-day leave; request detail lists roster allocations the leave clashes with.
- Shell page `/workforce/leave-requests`.

Not in this slice: staff filing their own requests (#1132, needs a staff-to-login link that does not exist), multi-level approval chains, approver notifications (#416), supporting attachments, sandwich rules for weekly offs, clubbing across intervening holidays, short-leave monthly caps, compensatory leave, and reports (slice 4). Each is out of scope, not half-built: the API rejects nothing it should accept and accepts nothing it cannot account for.

## Semantics

- `PUT …/staff/:staff/leave-requests/:id` files a request with a client-chosen id. An identical replay returns the stored request; reusing the id with different content returns 409.
- A request lies inside one open leave year. It pins the leave-type revision and the staff assignment revision in force on its start date, and the current holiday-calendar revision for each calendar year it touches. A missing calendar fails closed (409).
- Each date is stored with the half-days it counts: 2 for a working full day, 1 for a half day, 0 for holidays and short leave. A request whose every date is a holiday is rejected (400). Weekly offs count; sandwich rules are out of scope.
- Overlap is a PostgreSQL exclusion over half-day slots per staff member for pending and approved requests, so morning and afternoon halves of one date can coexist; full days and short leave occupy both halves.
- The type must be active, in the staff member's structure, allow the unit (`half_day_allowed`, `short_leave_minutes`), and, when the entry is not probation-eligible, the staff member's enrollment profile must show probation ended before the start date. The profile stays behind `workforce.enrollment.manage`: filing or approving such a type without that permission is 403, and a missing probation end date fails closed (409).
- A non-clubbable type cannot sit on the day before or after live leave of a different type, in either direction.
- For balance-tracked types, the ledger sum for the year minus other pending requests must cover a new filing; at approval the ledger sum alone must cover it, so when holds were over-committed by later corrections the first approval wins and later ones are refused. Every request write takes the hospital's leave-ledger lock that postings and corrections already hold, so balance checks serialise with every ledger writer.
- Transitions: pending → approved | rejected | cancelled; approved → cancelled. Each decision carries `expected_version` (stale is 409). The approver must differ from the filer, enforced in the API (403) and by a table check. Cancelling approved leave needs `workforce.leave.approve`.
- A leave year cannot close while it has pending requests (409).

## Data and security

Migration `0012_workforce_leave_requests` adds `workforce_leave_requests` (head, guarded status/version), `workforce_leave_request_days` (one row per date, inside the request dates, checked against the head at commit) and `workforce_leave_request_events` (append-only transitions), and extends the ledger with `consumption` and `reversal` kinds tied to a request id. Forced RLS uses the hospital policy and the `app.workforce_leave_ledger` write flag on every table. Request reasons may hold health information: they are returned only to holders of the request or approve permission, never written to events, logs or audit payloads, and responses are `no-store`. Each transition commits the head, event, ledger entry, audit row and an identifier-only `leave_request_changed.v1` outbox event together.

## Verification

Real-PostgreSQL module tests: filing, replay and id reuse; every rejection above (overlap including concurrent filings, clubbing both directions, probation, unit rules, insufficient balance with pending holds, missing calendar, cross-year range, closed year, inactive staff); half-day pairing on one date; approval consumption and its idempotent replay; concurrent approvals that would overdraw; self-approval; reject and cancel paths with reversal; permission, grant and tenant denial (403/404/401); reason redaction; year close blocked by pending requests; roster population skipping approved leave; and database guards below the API (exclusion, self-approval check, append-only events, days outside the request dates). Every guard is mutation-checked. Page tests cover empty, failed, read-only, filer and approver states. Full Go/web gates, `scripts/verify-leave-requests.mjs` against the synthetic tenant and production evidence before the slice is reported done.
