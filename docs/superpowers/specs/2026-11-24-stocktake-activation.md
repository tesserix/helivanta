# Stocktake activation continuation

Status: implementation plan for #1686; this document enables no feature.

## Bounds and owners

Reuse ADR-0073: 10 stocktake commands/second across tenants, 10,000 frozen
batch/location buckets, 100 count entries/command and 500 adjustments/approval.
New department-choice pages contain at most 20 records (approximately 4 KiB),
read-heavy with selection only when the worker changes location. Department
ancestry/subtree bounds remain the Organization owner's existing limits. Page
p99 remains below 300 ms and start/post below five seconds. One active count per
outlet bounds source-change recovery; no additional store or service is needed.

IAM owns effective assignment snapshots and membership/credential cutoffs.
Workforce owns current reviewed employment/account links and historical actor
identity. Organization owns the current department hierarchy and revisions.
Pharmacy owns outlet bindings, stocktakes and append-only stock movements.
Verified tenant/account and narrower hospital/department/outlet scope apply at
every boundary. Client selections never confer authority.

## Department descendants

Add a bounded private Global API collection at
`/pharmacy/hospitals/:hospital/departments/:department/stocktake-department-choices`.
The route department is the selected assignment's root. The response contains
only named current departments within that root and the held stocktake read and
Staff-source grants. Organization supplies names/revision references through a
narrow owner projection under the held root proof and structure lock. Pharmacy
never reads Organization tables or fabricates its hierarchy.

Reuse the existing Organization `CurrentSubtree` owner primitive and its limits,
not a new hierarchy store. Cursor authority binds account, hospital and root;
query keys also retain the chosen assignment version/epoch. Department/outlet
selection remains explicit and named. Selecting a child changes subsequent
stocktake command coordinates to that child; commands revalidate actual held
ancestor grants, Staff, complete current ancestry and the child's binding.

Unknown IAM/Workforce/Organization, failed disclosure audit or malformed source
returns unavailable without names. Foreign or no-longer-current structure is
hidden. Empty pages retain cursor continuation; a source withdrawal removes work
from view. No governance/Organization-management permission is added to stocktake
roles. Baseline department/outlet discovery supports retained recovery while new
stocktake work remains release gated.

## Source changes

Before activation, test binding replacement/end, department revision/retirement,
and the actual ownership mutation contract. An old count cannot be silently
adopted by a new binding or department revision. Revoke operational access
immediately; preserve original quantities, actors, commands and adjustments.
Changing a source must not leave an active count blocking the outlet forever.
Recovery of an invalidated unfinished count must never post stock movements
or confer approval authority. The source recovery decision below uses append-only
invalidation metadata and a fresh start; it invents no cancellation command. A completed
count and its ledger are never erased or undone by source cleanup.

## Delivery and activation

First record actual hospital maximum-capacity evidence and inherited child
selection tests, then source-change/recovery evidence. Keep the canonical
`pharmacy_stocktake` eligibility false until those checks pass. Generate the
canonical release outputs and contract/access inventories together. Merge only
after focused hosted checks and protected CI pass; no local app/database,
container/Colima or local build is required by this workflow.

Deploy through the owning GitOps/Kargo stage, verify the exact running images
and migrations, then enable the reviewed Console flag and its prerequisites.
Verify UI and Global API for actual hospital and independent QA accounts,
including wrong tenant/scope, counter versus reviewer, uncertain commands and
retained recovery on flag withdrawal. Record release/flag commands and restore
any temporary test state. Provider desired=true alone is not app acceptance.

## Source recovery decision

Recovery remains within Pharmacy and its existing guarded command transaction.
A new start first has to establish current IAM counter/source grants, reviewed
Staff, current Organization ancestry/revision, the current hospital-owned outlet
and independently approved binding. An existing active count with the same
binding snapshot continues to block a second count.

When a different current approved binding replaces the original source, the
unique approved-binding-per-outlet index proves the original binding cannot
still be approved. Append a source-invalidation record for the old unfinished
count in the same transaction as a successful new start. Pin the index shape,
validity and uniqueness in the recovery integrity check. Do not invalidate from
an unknown source, an unapproved replacement, a Staff departure, a release flag
change or a submitted/posted ledger action. A completed count is never invalidated.
Invalidating metadata posts no movement and invents no counter or reviewer
command. It retains original quantities, versions, actors and receipts.

The old header/list reports `source_invalidated: true` while preserving its
original command state/version. The UI labels the retained source change and
withholds counting, submission, approval and cancellation on that old record.
New writes must match the original binding/version/revision even before another
start records invalidation; original command recovery retains its existing
current-account, Staff and source restrictions. A retired department does not
transfer its old command-recovery authority into a replacement department.

Department revision/retirement first makes the old binding unusable through the
existing Organization reference guard. The independently reviewed replacement
binding must reference a current revision; then a valid start can invalidate the
old unfinished count and take a fresh snapshot. End-and-recreate and atomic
binding replacement follow the same path. There is no event delivery window in
which an old source authorizes new work, and no asynchronous cleanup dependency.

Only the original start's internal source references are needed for this
recovery. They remain inside the Pharmacy owner and are not an API projection of
Staff identity or a reusable permission. The new append-only table uses forced
RLS, bounded exact outlet coordinates, held current counter/source evidence,
original/replacement foreign keys, disclosure/write audit and existing private
choice-purpose restrictions. An invalid or oversized new start rolls back the
invalidation with it. Duplicate/replayed starts cannot duplicate invalidation.
