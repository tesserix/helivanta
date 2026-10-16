# Independent document-worker outbox policies

Refs #1135, epic #1133, parent #979. Production acceptance reproduced SQLSTATE
42501 when Supply completed a clean scan: the shared permissive outbox policy
contains an HR subquery. PostgreSQL checks that subquery's privileges even when
its CASE branch is not selected. Supply-only migration tests missed the full
registry composition.

Keep the common permissive policy limited to tenant visibility. Move the existing
HR insert restrictions unchanged into a role-specific RESTRICTIVE INSERT policy
for `helivanta_document_worker`. Preserve the independent restrictive Supply
policy. Add append-only `0018_workforce_worker_outbox_scope`, applied after the
existing HR migration; do not modify applied migration SQL or grant clinical
access to Supply. No new endpoint, privilege, identity, service or data model is
required. Existing capacity, SLO and cost assumptions remain unchanged.

Protected assets are private HR and Supply evidence and their event provenance.
An incorrectly scoped worker or another tenant must neither read the other's
records nor publish a fabricated completion event. Both workers retain scoped
column permissions, forced RLS, exact event checks and independent credentials.
Failure keeps the evidence unavailable and Temporal retries the same attempt.
Repeated processing must produce one released object and one completion event.

Prove valid Supply processing with the entire production migration registry,
cross-tenant denial, no HR table access, and forged HR event rejection. Run
existing HR/Supply lifecycle tests and backend race/coverage/lint/build checks.
Verify the approved production release through actual synthetic upload,
download and revocation. This policy-only migration is forward-only: retain
tables/objects, repair policies forward, and do not remove worker restrictions
as a rollback. Its named production migration and rollout require approval.

Rejected: granting Supply SELECT on workforce_documents broadens the trust
boundary; a CASE-only guard still triggers privilege checks; weakening event
checks makes forged outbox publication possible.

## Upload timestamp ordering: #1167

Local full-suite runs also reproduced `workforce_documents_check2` at the first
upload timestamp update: the sampled database wall clock preceded immutable
creation time. Reproduce deterministically by setting the isolated fixture's
creation default ahead of its current clock before creating an intent. Preserve
the chronology constraint and set the first upload timestamp to the later of
database wall time and creation time, for both HR and Supply. Exact replay keeps
the stored timestamp. No production row rewrite, new migration, clock change or
new privilege is required. Existing expiry and scanner-provenance checks remain.
