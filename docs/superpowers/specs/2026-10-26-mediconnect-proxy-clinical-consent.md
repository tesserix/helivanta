# Signed guardian clinical-sharing declarations

Related stories #552, #60 and #556 remain open until their full criteria pass.
Patientmaster owns authority, notices and consent; MediConnect owns live account
claims. Global API is the public entry. This adds guardian declarations to the
existing single clinical-consent ledger without changing retained self v1 bytes.

Use separate explicit proxy capture at `/v1/patients/my/proxy-clinical-consents/:consent`;
read, history, withdrawal, integrity quarantine and recipient decision remain the
canonical clinical-consent APIs. Proxy declarations use signed schema version 2
and pin parental/appointed-guardian capacity, authority revision, identity decision,
registration version, purpose, notice and exact approved recipient. They authorize
no record release on their own. Existing self capture accepts only self authority.

Notices gain an optional immutable `authority_kind`: omitted means self; parental
and appointed_guardian require separate explicitly reviewed wording. Preserve old
notice hashes with omitted JSON fields. No real notice or legal policy is created
or approved by code. Qualified review remains required before real activation.

Add `0010_patientmaster` with nullable separate proxy-authority FK and an exactly-
one-authority constraint. Retain existing self FK/history, common consent IDs,
unique active scopes, audit, integrity incidents and outbox publication. Extend
RLS narrowly for selected live proxy validation and exact recipient evaluation;
preserve every existing registrar/self/recipient branch. Parental acceptance,
capture and decision recheck exact non-estimated DOB and the reviewed calendar
boundary. Unknown, changed, expired or revoked authority denies new use.

The private owner claim-check request gains optional exact relationship (self by
default, child or ward explicitly). Workload authentication, tenant/object binding,
audit and bounded no-cache lookup remain mandatory. This observes claim intake,
not consent. Capture and decisions check it freshly; withdrawal needs neither
signer nor claim service. Outages deny or return unavailable, never allow.

Sign outside transactions, then re-read all local source versions before atomic
insert. Exact retries preserve terminal state; duplicates publish one transition.
Quarantine invalid signatures and retain history; corrections do not erase facts.
No new cache, worker or runtime service is needed. Portfolio peak 1,000/3,000 RPS
and 2–20 KB JSON assumptions apply; declarations remain capped at 16 KiB. Existing
12-second operation/3-second claim budgets apply; these are not measured SLOs.
Writer rollback retains a reader-compatible binary and additive schema/history;
do not drop records or roll back to a self-v1-only reader after proxy capture.

Alternative separate proxy ledger rejected: it would duplicate withdrawal,
quarantine, audit and active-scope logic. Reinterpreting self v1 as guardianship
is rejected because its signed meaning must remain stable. Adult delegation,
clinical record release, booking/reminders, offline/device capture and real policy
approval are separate remaining work, not implicit consequences of this change.

Verify real PostgreSQL/NATS/OpenBao and Global API journeys: signed parental and
appointed-guardian capture, exact recipient allow/wrong resource deny, direct
patient read denial, withdrawal/replay, live claim withdrawal, authority revocation,
majority/registration changes, wrong notice capacity, cross-tenant/hospital/actor,
signer/claim/audit/outbox failures, integrity quarantine, concurrency and complete
self-consent regression. Apply IN-ID-02/04/06/07; metadata is not legal approval.

## Release gates still to resolve

The writer control `PATIENTMASTER_PROXY_CLINICAL_CAPTURE_ENABLED` now defaults to
false; it blocks new proxy capture while leaving canonical retained reads,
withdrawal and exact replay available. Empty relationship remains omitted from
self owner requests so rolling upgrades preserve the old private request shape.
Enable proxy writes only after all owner/reader replicas support the new contract.

The deployed runner is transactional. The local implementation now adds typed
concurrent index migration `0011_patientmaster`, including restart/invalid-artifact
recovery and conflict checks. Its 100k capacity-skew plan test passes with guards
enabled. Full regression/CI and deployment of this runner remain required; do not
substitute a blocking index build. See ADR-0042.

Older binaries cannot interpret schema-v2 proxy receipts and could quarantine
otherwise valid history. Before activating proxy writes, ship a reader-compatible
baseline and a disabled-by-default writer control, with tenant activation requiring
an explicitly approved capacity-specific notice. Rollback must disable writes and
retain a reader-compatible binary. Rolling back to the self-v1-only binary after
proxy capture is unsafe; retained rows must not be erased to enable rollback.
These are release blockers for this worktree, not completed capabilities.

## Concurrent-index migration decision

The actual callers are the new clinical-consent FK index and capacity catalog
keyset index on populated tables. They require ordinary non-unique B-tree keys,
including descending keys; this is not a general nontransactional SQL executor.
Retain transactional migrations and their atomic marker behavior. A migration
contains either SQL or typed concurrent indexes, never both. Use a dedicated admin
connection and a database-wide session advisory lock so processes cannot race
across transactional/concurrent steps. Bound lock waits to 5 seconds and index
statements to 10 minutes, restoring prior settings before returning the connection.
No runtime tenant pool or privilege changes are permitted.

Only mark an index migration applied after every specified index is valid/ready.
An existing equivalent valid index is restart-safe. An invalid index with the exact
expected table, simple keys, ordering, default collation/operator classes and no
extra uniqueness/include/predicate/expression semantics can be rebuilt using
`REINDEX INDEX CONCURRENTLY`. A conflicting name/definition fails without altering
it. No drop or arbitrary SQL recovery is provided. Interrupted creation/rebuild
retains evidence and is retried by the next migration invocation, not a background
unbounded retry loop. Cleanup must release the lock even after cancellation; a
connection whose cleanup fails must be discarded rather than pooled.

The inherited portfolio assumes peak 1,000 RPS/3,000 burst and 2–20 KB JSON; consent
history grows rather than being erased. Validate 100k-row plans and design for
millions of retained rows. This migration path introduces no HTTP latency budget;
it prevents DDL from blocking serving writers and fails rollout on ambiguity.
Rollback retains additive indexes/ledger; this is intentionally forward-only.
General expression/partial/unique index APIs are deferred until an actual caller
needs their additional semantics.

The cross-process test exposed a PostgreSQL deadlock when a blocking advisory-lock
waiter retained the snapshot needed by `CREATE INDEX CONCURRENTLY` validation.
Acquire the session lock with `pg_try_advisory_lock` and bounded jittered waits
outside SQL; never park a database snapshot waiting for the migrator lock.
