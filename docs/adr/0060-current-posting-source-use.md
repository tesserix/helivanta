# ADR-0060: Current posting source use across department and product owners

Date: 2026-11-05. Status: owner implementation locally verified; consumer and
production acceptance remain open.

Workforce owns Staff, employment and approved postings; Organization owns active
structural references; IAM owns current independent grants. Pharma owns outlet
bindings, professional review/designations and operations. The current Pharma
consumer needs live source checks through its own command, not another Staff
master or a reusable authority token. Standalone employer work remains gated by
ADR0054 acceptance and is not implemented by this hospital-source boundary.

Use the existing unmeasured50 peak requests/s,80:20 read/write,2–20KiB normal
responses,500,000/1.5million Staff at12/36months, p99<500ms and99.9% availability.
Each source is one exact approved posting, with at most32 ordered source nodes.
Reuse the guarded Postgres command and existing owner ports; no service, queue,
cache, deployment resource or extra compound-IAM check is introduced.

The new Workforce-owned `workforce.posting_source.use` capability grants only
identifier/source observations inside a guarded caller command. It grants no
private HR rationale/directory/file read or operating authority. Product callers
still require their independently approved action permission and current reviewed
Staff actor. These three requirements fit the existing maximum3 compound checks.
No legacy role replaces the required independently approved scoped assignment.
The platform retains its existing coarse tenant-administrator permission default;
that coarse permission alone cannot satisfy this guarded source boundary.

The scope pins tenant, hospital, Staff, approved posting/revision and current
structural department/revision. The owner requires genuine IAM command evidence,
validates the active caller, locks supervision before actor/source rows, validates
the exact approval and original root/supervision proof, and holds every current
employment/department/cutoff source through the product commit. Duplicate/cyclic
or incomplete chains fail closed. Owner context is restored before product code.
No current caller permission or source availability is inferred from IDs, shared
ownership, affiliation, Staff category or a stored approval/history receipt.

Current use differs from approval: a scheduled earlier employment end truncates
usable lifetime; it does not require the old posting interval to fit the newly
shortened future term. It never extends nominal posting eligibility. Use requires
an effective generation and an already-started placement. The minimum includes
current employment endings, nominal placement ends, all ancestor bounds and all
current retained cutoffs. The guarded-command deadline and database final check
prevent resuming after expiry. Immediate source ending/revocation denies.

Source-use audit and caller work commit atomically. The owner retains an
identifier-only bounded observation record and returns an attributable reference,
not permission. Audit/dependency failure withholds the callback; callback or
outbox failure rolls back its source-use audit as well. Source contexts remain
read-only at the database boundary and cannot approve, revise or end postings.
Missing current IAM/owner state is503; a known inactive/stale source is denied.

The first product integration is hospital-employed Pharma staff professional
registration/designation, followed by unit-bound operations. Each command must
also verify its outlet/qualified policy and match source Staff to acting Staff
where operating action requires that. The source port alone does not complete or
activate that consumer. Existing tenant-only dispense rows require trusted
hospital/outlet lineage; no hospital/outlet is fabricated to make them pass.

Tests cover actual IAM/OpenFGA/Postgres, self-operation versus independent review,
root/32-node bounds, future starts, shortened future terms, immediate/ancestor
cutoffs, source changes, department retirement, sibling/foreign scope, missing
proof, audit/outbox/callback faults and both source-writer race orderings. Keep
legacy approval behavior unchanged and verify its existing suites. New persisted
migration/template numbers are allocated from the current registry. No production
migration or rollout is authorized here. Rollback retains additive evidence and
removes consumer mounting; it does not delete historical approvals.
