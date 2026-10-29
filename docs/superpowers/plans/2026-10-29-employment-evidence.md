# Protected employment evidence binding

Continues PR #1457 and the reviewed employment-period design. Workforce owns
both draft terms and scanned Staff documents. This slice binds available
`enrollment` / `employment_evidence` files to one immutable terms revision;
it does not submit/approve employment, verify a contract's truth, or activate
access. Existing PNG/JPEG upload, inspection/scanning and private storage remain
the file owner. PDF intake and generated letters remain separate work.

Require enrollment and document-management permissions from the same refreshed
permission set, plus the current explicit hospital manager grant. Scope every
lookup to the signed tenant, hospital, Staff, period and immutable revision.
Reject a file from another Staff/hospital, wrong purpose, unscanned/revoked file,
stale Staff source or noncurrent terms revision. New bindings freeze file ID,
record/source version, checksum, size/type/name, scan attempt and exact source
and released object generations. Clients cannot supply those verified facts.

Use create-only UUIDv7 binding/command IDs, immutable receipts and history.
An explicit reasoned withdrawal creates a new retained revision and cannot
reactivate a binding. At most 20 active files per terms revision; list and
history are cursor-paginated. Exact retries reauthorize and return the original
receipt without restoring withdrawn evidence. Protected content reads recheck
current permissions, file release and binding before and after storage I/O;
revocation/withdrawal or generation mismatch prevents bytes being released.

Reuse the period design's unmeasured 500,000/1.5 million Staff at 12/36 months,
50 peak metadata requests/sec, 99.9% availability and p99 below 500 ms planning
budgets. Twenty approximately 1 KiB binding snapshots cap an active evidence set
at about 20 KiB, excluding immutable history and already-owned file bytes.
Commands stay below 4 KiB; pages default to 50, maximum 200. No new service,
cache, queue or storage copy is needed. Plain reference text is insufficient
because it cannot identify the authorized scanned bytes.

Lock order: current hospital grant, hospital supervision advisory lock, Staff,
terms head/history, document, then binding. All snapshot/history/audit writes
commit in one owner transaction. No external storage or identity request occurs
while SQL locks are held. Download uses the existing document owner's two-phase
release, with the same document-before-binding lock order in each authorization
phase. Authorization/SQL failures deny; storage failure is unavailable, never a
successful download. Available ledger metadata is not a promise that storage is
currently reachable or that the underlying employment claim has been verified.

Additive migration 0051 retains bindings/history on application rollback; no
legacy evidence is fabricated or automatically bound. Existing API contracts
remain unchanged; new purpose-scoped routes/models are additive. Production
rollout/migration needs its exact approval. Tests must cover real owner upload
and scan, binding/withdrawal/replay, purpose/source/scope denial, immutable/RLS
history, stale/concurrent commands, and withdrawal during file retrieval. Then
run full affected Go/web gates and the local Staff Admin upload/bind/view journey.

Local implementation and automated/Staff Admin UI gates passed on 2026-10-29.
See the delivery tracker and acceptance ledger for observed results and remaining
production/independent-employment-review gates.
