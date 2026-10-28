# ADR 0046: Recover a staff file whose processing execution expired

Status: implementation candidate, 2026-10-28. Related: #1105, #1399.

Actual local browser evidence showed a file indefinitely marked Processing after
its matching Temporal execution timed out before any worker ran. The scanner and
worker were subsequently ready; the UI could not recover the file because manual
retry accepted only `scan_error`. The 45-minute workflow execution deadline can
expire before an activity writes an error verdict.

Workforce remains the data owner. Existing document-management permission and
explicit tenant/hospital/staff/file gates apply, including the tax/benefit parent
and frozen-evidence checks. A new attempt grants no download or evidence approval.
The assets are private staff evidence and trusted scan provenance; foreign users,
unprivileged insiders and stale workers must not obtain or release those bytes.
Global API remains the only browser business API destination.

Allow an authorized exact next-attempt command for `scan_pending` only after its
45-minute processing window has elapsed on the database clock. Preserve immediate
retry for `scan_error`. Use the existing file row lock, immutable source generation,
monotonic attempt, transaction/outbox and idempotent receipt. An old worker must
return superseded and cannot release against the new attempt. Restarting or
repairing Temporal executions by hand, backdating business data and treating a
queue receipt as successful processing are rejected recovery paths.

The database guards enforce the deadline and attempt transition. Existing tax
and benefit frozen-evidence guards also reject attempt changes on a frozen
pending row, preserving their `not_pending` error reasons and the current
settlement exception. Queue-entry
and retry timestamps are server-owned; an unchanged pending attempt cannot rewrite
its waiting clock. No new API field, permission, table, public object URL or verdict
is introduced. The UI uses the existing timestamp to offer recovery; client clock
skew can only cause a server-rejected early command. Canonical document constants
supply the matching Go workflow and TypeScript presentation duration; migration
0047 pins the same 45-minute database rule for this schema version.

Storage/scanner/Temporal failure leaves the file protected. A failed transaction
rolls back attempt, audit and outbox. When the workflow service is unavailable,
committed outbox delivery retries; no file becomes available from the retry itself.
Frozen/decided proposals remain immutable. Ordinary file retention and provenance
are preserved. Revoked, rejected and available files cannot be restarted.

This is a row-local command with no additional network call under lock. Its target
is the existing 5-second request budget, one bounded primary-key row lookup, and
no new steady-state service or cloud cost. Existing workflow concurrency and scan
limits remain. Schema rollout is a forward migration replacing the three guard functions;
code rollback retains monotonic attempts and history. Reverting the guard to the
previous canonical guard functions would disable stalled recovery, not delete evidence.
Production migration/rollout needs its own named approval. No production change
is authorized or claimed here.

Acceptance requires a failing real-DB reproduction, duplicate and concurrent
commands, early/foreign/unprivileged/frozen denial, old-worker supersession and
outbox rollback, plus actual Admin UI retry through Global API to a real local
ClamAV/Temporal worker, matching-byte download and revoke/relogin denial. Component
checks cover recent versus expired pending files and uncertain same-command retry.
All broader identity/background/photo/card and deployed Staff gates remain open.
