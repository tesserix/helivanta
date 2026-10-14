# Private staff documents

Related: #1105, #483, #29, #688, #6; parent #1035 / #979.

## Scope and threat boundary

Implement staff employment/qualification evidence first. Workforce owns its
metadata, authorization, API and UI; patient documents follow in patientmaster
using the same byte-storage and scanner mechanics. Protected assets are staff
documents, their ownership and scan provenance. Internet callers, another
tenant/hospital, a revoked manager and hostile content must not cross these
boundaries. The API derives tenant and actor from verified authentication,
locks the hospital-manager grant and validates the owning workforce record.
Storage prefixes never grant access.

Document intent, metadata and byte routes require `workforce.documents.manage`
independently of enrollment-form permission. Its initial system-role mapping
is tenant administrator only, matching the existing sensitive enrollment
boundary; ordinary clinical/directory roles inherit neither permission.
The current hospital's explicit manager grant is still required and locked
at every database phase. Tenant administration never implies hospital access.

Planning assumptions, not measured production demand: 20 simultaneous uploads,
10 MiB maximum each, 2 upload starts/second peak and 10 metadata reads per write.
At 10,000 files/month and 2 MiB average, original bytes total approximately
240 GiB at 12 months and 720 GiB at 36 months before retention. Temporary
quarantine is separately accounted. Target metadata p99 is 300 ms and monthly
availability is 99.9%; upload/scanning duration is visible asynchronous state,
outside that latency target. Start with bounded worker concurrency, no sharding.
Standard regional GCS storage is roughly $0.02/GiB/month before operations,
network and tax; verify regional pricing before provisioning.

## Contract and lifecycle

Use opaque intent/file/record IDs and versioned immutable objects with the
server's fixed UTC upload date. Do not put names, filenames, employee codes,
patient identifiers or bytes in events, workflow history, logs or Valkey.
Initial supported evidence formats are PNG and JPEG, bounded to 10 MiB and
20 million pixels, fully decoded and rejected for trailing/animated content.
PDF/Office support requires a separately reviewed hardened parser policy.
Archives, encrypted
documents and active/unsupported formats stay unavailable. A declared MIME
type and extension do not establish the actual type.

Uploads create only in a private quarantine bucket. Finalization pins the
stored generation and verifies bounded size/integrity before committing a
scan-pending row and an identifier-only outbox event. JetStream starts a
deterministically named Temporal execution outside a database transaction;
acknowledge only after acceptance or verification of the identical execution.
Workers validate/download that generation, record complete attributable
ClamAV provenance, promote create-only, verify the destination, then commit
availability and audit/outbox. A duplicate or crash after promotion reconciles
the exact immutable bytes; mismatching destinations never become available.

Download is authenticated, mediated, generation-pinned and subject to the
current explicit hospital and document permission. Return private/no-store,
nosniff and an attachment disposition; no browser receives a bucket listing or
an unbounded capability. Original filenames are protected domain metadata.

## Processing identity and database scope

The processor connects only as the distinct `helivanta_document_worker`
PostgreSQL login. CNPG reconciles its password; it is neither superuser,
schema owner, `CREATEROLE`, `CREATEDB` nor `BYPASSRLS`. Per-table migration
grants permit required metadata columns and processing updates only, plus
append-only audit/outbox inserts. No personnel-table, object-listing or
administrative database access is granted. Application and worker connections
fail at boot if their privilege/identity preconditions do not hold.

Worker RLS requires all four transaction-local UUIDs: tenant, origin hospital,
staff and file. An unset or different scope reveals no row. Processing
transitions additionally require that authenticated database identity and
the exact row scope; an API connection cannot mint a verdict by setting a
custom session flag. The scanner activity reads only committed immutable
ownership/content facts. It does not re-query other modules or infer a new
record binding from mutable personnel data.

Clock assertions allow at most five minutes of inter-host skew, matching the
signature provenance boundary. Exact immutable generation and checksum proofs
establish upload-before-scan causality; UTC timestamps from different hosts do
not establish a strict execution order. Larger skew blocks availability.

Only opaque scope IDs and a processing attempt enter Temporal history.
Scanning, verified create-only promotion and matching conditional release
commit execute in an owning activity outside remote-I/O database locks.
Its receipt contains no content, original filename or human-readable data.
If promotion commits and the worker loses its database receipt, the retry
rescans the exact quarantine generation and reconciles the existing released
generation before committing. Terminal rejection remains inaccessible; a
transient failure persists an observable state and retries with bounded
Temporal policies. Each explicitly requested retry has a new immutable
attempt number and deterministic workflow ID; duplicate deliveries for the
same attempt can only select the same workflow.

The activity has a six-minute execution deadline, a forty-minute total deadline,
five attempts with exponential backoff (five seconds to one minute), and a
thirty-second heartbeat deadline. Heartbeats carry no document details. Scanner
or storage failures commit `scan_error` with a bounded failure code; an identical
repeated failure does not insert another state-change event. Confirmed malware,
unsupported content and an immutable intent/checksum mismatch commit `rejected`.
The activity emits generic error types rather than persisting raw SQL, scanner
responses or storage errors in Temporal history. Both clean and infected verdicts
require unchanged scanner engine and definitions before/after the complete scan.
The VERSION probes and INSTREAM run inside one numbered ClamAV IDSESSION on
one TCP connection. Separate probes can reach a different replica and cannot
establish which engine/definitions actually scanned the bytes. Each response is
bounded and its sequence number checked; incomplete, mismatched or reloaded
provenance prevents release.

The scan consumer validates the exact versioned envelope and canonical scope
before its after-commit handoff. Workflow IDs include all four ownership IDs and
the committed attempt. Reuse is rejected even after completion. On an already
started response, the consumer reads that execution's initial history event and
checks the workflow type, task queue and exact input before acknowledging; a
collision or unavailable history is not accepted as a matching receipt.

## Failure and delivery

GCS/scanner/definitions/Temporal unavailable: the file stays unavailable and
the UI explains the pending/error state. NATS unavailable: committed outbox
work waits durably. Valkey unavailable: no correctness or access decision
changes. PostgreSQL unavailable: no intent, release or download authorization
is assumed. Stale, partial or ambiguous scan results cannot release content.

Use workload identities for separate API and worker permissions, private
quarantine/released buckets distinct from CNPG backups, current definitions,
restricted scanner network access and bounded resources. Do not introduce
static cloud keys or a public scanner endpoint.

Schema expansion is additive and preserves old binaries. Rollback disables
new upload acceptance and restores the previous workload while keeping
metadata/objects private; it does not drop confidential rows or delete objects.
Retention/legal holds and security-owned malware investigation require reviewed
policy before destructive cleanup. Restore must reconcile generations, verdicts
and current grants before serving; required RPO/RTO follow the platform's
reviewed recovery policy and are not claimed by unit tests.

Alternative rejected: a shared file module querying every domain's private
tables would break module ownership; synchronous scan-and-release inside an
HTTP/DB transaction would lose durability and hold authorization locks across
remote I/O. A generic local-disk substitute is not production GCS proof.

## Acceptance

Prove public naming/scanner/content/storage behavior before integrating it.
Run real isolated ClamAV clean and synthetic antivirus fixtures. Test source
replacement, destination conflicts, truncation, size/hash/type mismatch,
cancelled I/O, duplicate delivery and retries between GCS promotion and DB
commit. Domain tests use real PostgreSQL with forced RLS, another tenant,
sibling hospital, missing/revoked grants and forged ownership. Verify synthetic
staff browser upload/pending/available/download flows locally and live after
CI/GitOps delivery. Helpers alone do not complete #1105.
