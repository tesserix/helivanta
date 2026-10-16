# ADR 0028: Attendance punches, derived attendance and finalised timesheets

Status: accepted for implementation. Parent: #1039 / #979. Extends ADR-0020 (rosters), ADR-0021 (leave) and ADR-0022 (duty adjustments); spec: [attendance](../superpowers/specs/2026-10-16-attendance-design.md); plan: [attendance](../superpowers/plans/2026-10-16-attendance.md).

## Requirements and evidence

Legacy Attendance screens (read-only review, 2026-10-12) show entry and exit date and time per shift, work hours, pending and overtime indicators, present, absent and late, and counters for short duty, leave without pay, missing punches and early departure, with recalculate and finalise actions. MIS report 929 lists each punch with person, department, biometric id, date, time, who updated it and when, status, active flag and source. No source values are copied. Planning assumptions: up to 2,000 staff per hospital, four punches per person per shift, about 250,000 punches a month, peak 20 reads/s and 5 writes/s, p99 under 500 ms for one day's sheet, one indexed PostgreSQL and no new service.

Biometric device integration is shelved by the product owner (2026-10-16). Punches are recorded by authorised managers with a reason; the source column leaves room for `import` and `device` later.

## Decision

Four layers that never write into each other:

1. **Raw punches**, append-only. A punch is a person, an instant, a direction (`in`/`out`), a source and a reason. A recorded punch is never edited or deleted; a mistake is answered by a separate, reasoned void row. Clients choose the punch id, so retries replay instead of duplicating.
2. **Derived attendance**, a pure function of punches, frozen-roster allocations, approved leave, approved overtime and the attendance policy in force on the roster date. It is computed on read and never stored, so a policy change or a voided punch can never leave a stale derived row behind.
3. **Corrections** (#1038 slice 3), separately approved rows that add or void punches; they reuse layer 1 and add an approval trail.
4. **Finalised timesheets**: a finalisation snapshots the derived rows for a closed period with the calculation inputs' revision ids, versions each recalculation, and locks punches and voids inside the period. Reports read finalised rows and reconcile to the individual records.

This ADR's first slice delivers layers 1 and 2 plus the policy. Corrections and finalisation follow as separate slices against the same tables.

Derivation rules:

- A day is the roster date of an allocation. Only allocations in frozen rosters are evaluated; draft allocations show as `unpublished` rather than producing lateness against an unpublished plan.
- A punch belongs to the allocation whose window (start minus the policy's early-arrival allowance, end plus the late-departure allowance) contains it; ties go to the nearest start for `in` and nearest end for `out`. Instants come from the allocation, so overnight shifts and DST transitions need no wall-clock arithmetic.
- Same-direction punches within the duplicate window collapse to the earliest `in` and the latest `out`, and are reported as `duplicate_punch`.
- First `in` and last `out` bound worked time. Worked minutes are their overlap with the shift; payable minutes are worked minus the shift's break, never negative. Time outside the shift is payable only through approved overtime.
- Late beyond the shift's grace minutes, early departure beyond the policy grace, and short duty (payable short of expected by more than the policy tolerance) are separate exceptions. Approved half-day leave excuses the matching half; approved short leave reduces expected minutes.
- Before the shift starts a day is `upcoming`; inside its window without an out punch it is `in_progress`; never `absent` early.
- No punches and no leave is `absent` and counts as leave without pay. An `in` without an `out` (or the reverse) after the window closes is `missing_out`/`missing_in` and pays nothing until corrected.
- No attendance policy in force on the date yields `no_policy` and no figures; the system never guesses thresholds.

Each derived shift carries the identifiers that explain it: allocation, shift revision, policy revision, the punches used and the ones ignored.

Rejected: materialising derived rows on each punch. It would need invalidation on every leave, overtime, roster and policy change and would drift on the first missed event. Rejected: editing punches in place, which loses the observation the correction replaced.

## Concurrency and storage

Punch writes take the hospital attendance advisory lock, so the duplicate check sees concurrent writes. Punches are indexed by `(tenant, hospital, staff, punched_at)`; a day's sheet reads one keyset page of people, then their allocations, punches, leave and overtime in four indexed queries. Valkey is not used: derived rows are cheap, must reflect the latest void at once, and a cache would be a second source of truth. NATS carries identifier-only change events through the outbox for downstream payroll (#1040). Temporal is not used: every write is one transaction with no long-running step.

## Security and compliance

Threats: a foreign tenant or sibling hospital, an unauthorised person fabricating presence, silent edits to worked time, replayed writes, and punch reasons revealing health details. Reads need tenant membership and a hospital grant; writes need the permission, a hospital manager grant locked `FOR SHARE`, and the `app.workforce_attendance` flag in RLS `WITH CHECK`. Permissions: `workforce.attendance.configure` sets the policy; `workforce.attendance.record` records and voids punches. Cross-tenant and foreign-hospital requests are 404, a missing permission 403, database errors fail closed.

SOC 2 (CC6.1, CC7.2, CC8.1) and HIPAA (§164.312(b) audit controls, §164.312(c)(1) integrity): punches and voids are append-only at the database (statement triggers reject UPDATE, DELETE and TRUNCATE), each write adds an identifier-only `workforce_audit` row and outbox event in the same transaction, and every row records its actor. Reasons may mention health, so they are returned only to holders of `workforce.attendance.record`, never placed in events or audit rows, and capped at 500 characters. Punches cannot be in the future or older than the policy's back-dating window, which the finalisation lock will tighten.

## Consequences

- Payroll (#1040) reads derived payable minutes and approved overtime per day, and later the finalised snapshot.
- Device and import sources add a source value and an ingestion path, not a new model.
- A day's figures can change until finalisation; finalisation is the point where they stop changing, by design.
