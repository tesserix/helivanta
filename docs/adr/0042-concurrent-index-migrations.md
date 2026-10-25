# ADR-0042: Concurrent index migrations on the schema-owner connection

Status: implemented locally; release validation pending. Related: #552, #60, #556.

## Context

Signed proxy consent adds an FK and capacity-specific notice catalog keys on
populated tables. Existing portfolio assumptions are 1,000 peak/3,000 burst RPS
and 2–20 KB API payloads. Test plans cover 100k notices; retained consent history
can grow to millions of rows. Blocking index builds are unsuitable for serving
hospital writers. PostgreSQL forbids concurrent builds inside a transaction.

## Decision

A `tenantdb.Migration` contains either transactional SQL or typed concurrent
non-unique B-tree indexes. Typed indexes support only public-schema tables, simple
columns, optional descending order, default collation and default operator class.
Identifiers are bounded/validated and quoted with pgx. Runtime tenant/system
privileges do not change; only the existing schema-owner pool is used.

Use one dedicated, session-affine admin connection. The schema URL must connect
directly to PostgreSQL or a session-pooled endpoint, never a transaction-pooled
PgBouncer endpoint. A database advisory lock coordinates migration processes.
Acquisition uses nonblocking attempts with bounded jittered waits outside SQL:
blocking advisory-lock waiters can retain snapshots and deadlock concurrent-index
validation, reproduced by the independent-pool test. Bound lock acquisition to
5 seconds and statements to 10 minutes. Restore prior settings/release the lock;
discard the connection if cleanup fails.

Transactional DDL and its marker retain one transaction. Concurrent markers commit
only after every index is structurally equivalent, valid and ready. Equivalent
valid artifacts survive interruption before marker creation. Equivalent invalid
artifacts use `REINDEX INDEX CONCURRENTLY`; conflicting names/definitions fail
without changing them. No arbitrary outside-transaction SQL, index dropping,
uniqueness, expression or partial-index API is added.

## Evidence and consequences

Real PostgreSQL tests cover cancellation with an invalid artifact, serving writes
during a build, retry, conflicting metadata, independent admin pools, partial
progress, settings cleanup and transactional rollback. A 100k notice test retains
RLS/audit/state guards and checks the actual catalog plus plan scan/buffer bounds.
These tests are not production throughput or availability measurements.

Migration `0010_patientmaster` adds compatible schema; `0011_patientmaster` builds
the proxy-authority FK and capacity catalog indexes. Reader-compatible code ships
with new proxy writes disabled, then activation follows verified reader rollout
and approved tenant/hospital notice governance. Do not roll back to a self-v1-only
reader after creating proxy receipts. Schema/index rollback retains evidence and
is forward-only.

A generic nontransactional SQL flag was rejected: it permits unbounded statements
without index postcondition/recovery checks. Plain index creation was rejected
because it blocks writers. A separate proxy consent ledger was rejected because
it duplicates history, active-scope, withdrawal and integrity behavior.
