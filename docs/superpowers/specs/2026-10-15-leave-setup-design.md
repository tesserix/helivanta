# Leave setup: types, holiday calendars and structures

Issue: #1037 (parent epic #979). ADR: [0021](../../adr/0021-leave-management.md). Plan: [leave management](../plans/2026-10-15-leave-management.md).

## Problem

Balances, requests and approvals all depend on reviewed leave policy: which leave types exist and how they behave, which days are holidays, and what each group of staff is entitled to. Legacy Leave Setup holds these as leave, holiday and structure tabs. Without versioned policy, a later balance or decision cannot say which rule produced it.

## Scope of this slice

- **Leave types**: code, name, description, active, effective date, and rules: `paid`, `balance_tracked`, `half_day_allowed`, `short_leave_minutes` (0 = not allowed, otherwise 15–480 in 15-minute steps), `clubbable`.
- **Holiday calendars**: one per hospital per calendar year (`calendar_year`, immutable). Up to 100 dated holidays, each `regular`, `government` or `optional`, unique per date, all inside the calendar year.
- **Leave structures**: up to 30 entries, one per leave type revision, each with `annual_half_days` (0–730), `accrual` (`upfront` or `monthly`), `carry_forward_half_days` (0–730), `proration` (`none` or `join_month`) and `probation_eligible`. An entry for a type that is not balance-tracked must have zero allowance and zero carry-forward. Referenced types must be current and active, or unchanged from the previous revision, and effective on or before the structure.
- Versioned writes (`expected_version`, mandatory review, idempotent replay), history, events, audit, generated Go/TS contract, `/workforce/leave-setup` page, verifier and docs.

Not in this slice, each tracked on #1037: leave-year settings and staff structure assignments, the balance ledger (allocation, accrual, proration, carry-forward, expiry, corrections), requests and approvals with overlap and roster conflicts, and reports 949/954. Structure fields are stored and validated now; nothing is credited from them until the ledger slice ships, and the page says so.

## Semantics

- `PUT /workforce/hospitals/:hospital/hr/leave-configs/:id` creates or revises one config. Replaying identical content returns the current record unchanged. Changing kind, code or calendar year returns 409.
- `effective_on` cannot move earlier than the current revision's. A holiday calendar's effective date must fall on or before 1 January of its year, so the whole year is covered by one reviewed revision.
- Reading a structure returns each entry's pinned type revision (code, name, rules), so a later type edit never silently changes what a structure meant.
- `GET …/leave-configs?kind=` lists current configs; `GET …/leave-configs/:id/history` lists revisions newest first.

## Data and security

Tables: `workforce_leave_configs`, `workforce_leave_config_revisions`, `workforce_leave_type_rules`, `workforce_leave_holidays`, `workforce_leave_structure_entries`. Forced RLS on the hospital policy plus the `app.workforce_leave` write flag. A deferred constraint trigger checks each revision has exactly the children its kind requires (one rule row for a type, at least one entry for a structure, holidays only in the calendar year). Writes need `workforce.leave.configure` and a manager grant; reads need any hospital grant. Each write commits config, revision, children, audit and an identifier-only outbox event together.

## Verification

Real-PostgreSQL module tests: create and concurrent idempotent create, replay, stale version, identity changes, permission/grant/tenant denial (403/404/401), invalid input (unknown fields, NUL, impossible dates, out-of-range or misaligned values, duplicate holidays or entries, holidays outside the year), references to retired, foreign-kind and foreign-hospital types, untracked-type allowances, history, audit/outbox counts, and database guards below the API (append-only revisions, children outside the creating transaction, RLS without hospital scope). Zod schema tests mirror the API rules. Page tests cover empty, failed, read-only and manager states, inline validation and conflict handling. Full Go/web gates, synthetic verifier and production evidence before the slice is reported done.
