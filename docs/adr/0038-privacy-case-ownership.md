# ADR-0038: Privacy case ownership and officer authority

Status: implementation in progress for #66/#556; API-only. No officer or policy is seeded.

Privacy owns cases, officer appointments and retained decisions; MediConnect
consumes them through Global API. Scope is verified tenant and subject, never
body-supplied tenant or proxy authority. IN-ID-04/06/07 apply. Clinical consent,
clinical records and erasure remain with their owners.

Sizing assumptions: 10 peak requests/s, 16 KiB body cap, 80:20 read/write, 100k
cases at 12 months and 300k at 36 months, 4 KiB descriptions and responses.
Design targets are p99 <300ms and 99.9% availability, not measured promises.
Indexed keyset reads cap at 100 rows. Existing Postgres/audit suffice; no new
cache/service infrastructure. DB/audit/authorization failure denies or rolls
back. Valkey is not case authority. Cost is existing DB storage/API capacity.

An administrator nominates an officer with reviewed public contact, evidence
reference and validity period. The nominated authenticated member must accept.
Admin status alone grants no case access. Replacement, revocation and expiry
remove officer authority at each operation, including old cases. Appointment
and case decisions are audited, immutable history is retained. Transactions lock
the current appointment before case state to serialize authority changes.

Subject-owned requests/grievances use UUIDv7 idempotency IDs, immutable original
intent and explicit-version commands with retained command history. One commit
covers state/history and response audit. Patient withdrawal/replies and officer
review/response actions are distinct. Case response is not proof of rights
fulfilment or permission to disclose/delete records; only the data owner can
provide those effects and evidence. No clinical/file payload enters events.

This slice implements authenticated tenant-local casework. Owner-confirmed
fulfilment, reviewed SLA policy/Temporal escalation, notification receipts and
verified representative/cross-tenant/public intake remain subsequent work;
#66/#556 stay open. No OTP/SMS/UI, legal text, age or statutory deadline is guessed.
Existing accounts stay usable when casework is unconfigured. Migration is additive
and forward-only; older binaries ignore these tables. Rollback disables routes,
never erases evidence. A mailbox-only intake was rejected because it cannot
enforce ownership, transitions and attributable officer decisions.
