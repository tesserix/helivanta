# ADR 0045: Staff file classification beside evidence purpose

Status: implementation candidate, 2026-10-28. Local route/schema, component and
owner/reviewer UI acceptance are recorded in the Staff ledger. Full local web
and backend race/coverage gates passed. Review, release and deployed acceptance
remain open. Nothing in this
slice is deployed. Related: #1035, #1105, #1399;
[Staff acceptance](../qa/staff-management-acceptance.md).

## Context and capacity

Workforce owns staff file metadata and versioned HR masters. The fixed
`category` in `contracts/workforce-documents.json` is an evidence purpose bound
to enrollment, a credential or a qualification. `documentCategoryBindings`,
`documentGate`, scanner state and release/download authorization use this
purpose. An arbitrary HR master must never redefine it. Benefits and tax evidence
share `registerDocument`, with their own gates and contracts; they are outside
this classification slice.

For design sizing only, reuse the provisional 2,000 staff/hospital envelope.
At 20 new classified files/staff/year this is 40,000 classifications at 12 months
and 120,000 at 36 months. At two revisions/record and a conservative 1 KiB per
indexed revision, history is about 80/240 MiB per hospital, excluding file bytes.
Plan for 5 peak metadata requests/second/hospital, 9:1 reads:writes, at most
1 KiB write bodies and 4 KiB single-record responses. A provisional 99.9% monthly
availability and 300 ms p99 budget through Global API require measurement before
adoption as an SLO. Threefold growth still fits indexed owner PostgreSQL tables.
No cache, queue, new service or additional cloud resource is justified.

## Options and decision

1. Replace the purpose enum with HR document categories: rejected because it
   would change record binding and security meaning.
2. Add fields to every existing upload/list/retry response: rejected because
   strict existing clients and the shared benefit/tax pipeline would be affected.
3. Add a separately versioned classification resource to an existing staff file:
   proposed. It reuses Workforce storage, authorization and master revisions,
   leaving file bytes, evidence purpose and the existing wire shape intact.

Proposed owner routes, through Global API:

- `GET /workforce/hospitals/{hospital}/staff/{staff}/documents/{file}/classification`
- `PUT /workforce/hospitals/{hospital}/staff/{staff}/documents/{file}/classification`
- `GET .../classification/history` with the standard cursor and limit maximum.

Read/write/history use the existing `workforce.documents.manage` permission,
verified tenant membership and a locked explicit hospital manager grant. The
owner must verify the exact file belongs to the scoped staff member and is one
of the three ordinary staff evidence kinds. Benefit/tax files are inaccessible
through these routes even when their IDs are known. No new role, clinical
privilege, approval stage or self-service disclosure is implied.

PUT accepts only `expected_version` and `classification_revision_id`. Empty
reference means unclassified; null, malformed UUIDs, unknown fields and supplied
labels/tenants/actors are rejected. The owner derives the label from the same
tenant/hospital `document_category` revision. A new selection requires the active
current revision; a retained historical selection remains valid after retirement.
No bulk matching of legacy names or automatic classification is permitted.

GET of an authorized file with no classification returns an explicit version 0,
empty reference/name. A successful meaningful amendment creates an immutable
revision with actor/time and advances the current version. An unchanged request
is a no-op. Exact retry against its immediately preceding expected version returns
the same result; other stale writes return 409. Clearing preserves the earlier
classification revision in history. Bound history reads and audit sensitive
metadata disclosure; never return original filenames or staff demographics in
new events. Changing classification never changes the file purpose, scanner
state, expiry, immutable bytes, checksum or released generation.

## Canonical contract and UI

Extend the canonical `workforce-documents.json` with separate classification input,
current and history definitions; regenerate their Go/Zod models. Existing document
input/output definitions retain their exact shapes. Route registration must name
its HTTP error, permission, scope and pagination contracts. Keep the current
registered product destination and use no direct client-to-owner bypass.

A Classify action on one staff-file row opens a single scoped metadata editor.
Do not fetch classification for every listed file through an unbounded fan-out.
Its cache key includes tenant, hospital, staff and file; tenant transitions clear
the editor and pending mutations. Require successful owner metadata loading before
showing Save, paginate master choices, show the recorded historical label, and
explain that classification does not change evidence purpose or review status.
Support edit, explicit clear, history, accessible validation, stale reload and
exact uncertain retry. This changes metadata only; authorization to download or
review the evidence is evaluated independently.

## Persistence, failure and rollback

Use owner-scoped current/history tables with forced tenant/hospital RLS, a typed
composite FK to immutable HR revisions and a scoped FK to the owning staff file.
Serialize first classification and subsequent amendments on the parent file lock;
lock order follows authorization, staff/file and master revision. Current state,
history, attributed audit and identifier-only outbox commit atomically. Do not
write the existing shared document row merely to change classification.

PostgreSQL, membership and current authorization are critical: failures deny the
operation, with no invented empty classification. Master lookup failure denies a
new assignment. File storage, scanning and Temporal are outside this metadata
operation; their availability does not grant or remove metadata authority, and a
classification never makes an unavailable file downloadable. Preserve the file
owner's existing revoked/rejected evidence history and authorization boundaries.

Migrate additively with bounded lock/statement timeouts; index any referencing
columns and keep history append-only. Old APIs do not touch the sidecar tables.
Application rollback hides this feature while retaining its data. No live schema
change is authorized by this design. Migration registration, route registry,
canonical Go/Zod generation, contractdocs and consumer examples must be updated
with implementation. There is no per-file cloud cost beyond existing PostgreSQL
storage/backup; validate table/index sizes against the provisional envelope.

## Threat model and acceptance

Assets are staff-file association, confidential metadata and classification
history. Threats include another tenant, a sibling hospital manager, a permitted
user substituting another staff/file ID, an insider forging an HR label and an old
client erasing new data. Every lookup includes verified tenant/hospital/staff/file
scope; unavailable authorization fails closed, and invisible objects return 404.
Existing file content/download policies remain authoritative.

Before release: real-DB cross-tenant/hospital/staff/purpose denial, forced RLS,
immutable-history attempts, master category/scope substitution, active/stale/
retired revision cases, null/empty/unknown input, no-op/exact retry/stale conflict,
concurrent first write, parent access revocation, audit/outbox rollback, bounded
history and prior-client compatibility. UI QA must create a synthetic category,
classify an existing synthetic file, save/reload, edit/clear, retire and retain its
recorded revision, then sign in independently and verify denial. Verify that
reclassification cannot change evidence purpose or make rejected/pending/revoked
content available. Test actual scanned file upload/release separately; metadata
acceptance alone does not complete #1105, identity review or #1399.
