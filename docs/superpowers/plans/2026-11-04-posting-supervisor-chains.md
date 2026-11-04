# Canonical supervisor-chain approval increment

Builds on root candidate #1565 and ADR-0053; not deployed or accepted. The parent
Staff objective, standalone employers, department-scoped IAM assignments and all
operating/Pharma lifecycle consumers remain required.

Workforce owns chain intent, decisions and retained evidence. IAM/OpenFGA retains
current hospital-scoped ordinary posting review and Staff actor use; this increment
adds no role or permission. Organization alone authorizes current department paths.
An approved posting confers no product access. No profile supervisor link, job
category, client UUID or management flag can substitute for a reviewed appointment.

Retain the existing unmeasured 50 peak requests/s, 80:20 reads/writes, 2–20 KiB normal
response, p99 <500 ms and 99.9% availability budgets. A chain is at most 32 postings
including the child. The worst command performs at most 31 ancestor source reads
and 32 bounded Organization callbacks under the existing command deadline. At
2.5/7.5 million historical placements and average four retained ancestor rows of
about 512 bytes, evidence payload is roughly 5/15 GB before indexes; the 31-hop
worst case is about 40/120 GB. These are sizing assumptions, not measured SLOs.
There is no service, worker, cache or new standing infrastructure cost.

## Source admission and locking

1. Reserve existing ordinary review and actor-use grants before Workforce locks.
   Keep the existing hospital supervision gate through the outer command commit.
2. Verify the pending child, independence and target generation exactly as root
   approval does. A root still requires the separate management capability and
   attestation. Non-root approval must reject a root-only management attestation.
3. Resolve each exact same-tenant/hospital Staff/proposal/revision under the
   owner's selected-Staff RLS context, restoring that context before child writes.
   Never query Organization or IAM tables from Workforce. Read only necessary
   proposal/source fields, never ancestor private reasons.
4. Each ancestor must be its exact current approved head and have typed retained
   intent. Members cannot supervise. Every chain reaches an independently approved
   hospital-management root, whose retained management evidence must exist.
   Repeated Staff/proposals, self-links, missing links and >32 postings deny.
5. Each target/ancestor Staff version, employment epoch, generation and reviewed
   eligibility cutoff must still match. Nominal supervisor intervals contain the
   direct child's entire interval. Preserve ongoing nominal terms; retain the
   intersection of source cutoffs separately. Scheduled sources can support a
   child only when they cover the child's start. Constrain outer commit to the
   earliest current cutoff; source unavailability returns 503 and writes nothing.
6. Hold Organization's exact current-reference callback for every posting through
   commit. Hospital-management roots cover only their own hospital. For each
   departmental supervisor, additionally hold the direct child's current ancestry
   callback against that supervisor's department. Sibling/upward/retired/stale or
   cross-hospital references deny. Staff locks remain ordered by the hospital gate;
   existing source writers must retain their pre-row-lock gate ordering.

## Persistence and API boundary

Add a forward-only Workforce migration after 0080 for immutable per-hop approval
source evidence. Retain the child approval revision, hop, exact ancestor
Staff/proposal/revision/generation/source version/epoch/department reference and
cutoff. Force tenant/selected-child RLS, composite references and immutable history.
The decision, all required evidence, audit and canonical event commit together.
Deferred database checks reject an approved child without complete matching chain
evidence. Failed source/evidence/audit/outbox writes leave no head or receipt.
Root evidence and historical root receipts remain compatible.

Public receipts/events keep their existing strict fields and `grants_access=false`.
Existing default and v1 supervision contexts stay compatible. No new route is
introduced by the first backend behavior. Canonical supervisor choices and a
protected reviewed-chain context must be designed, generated and tested before
normal submitter/reviewer UI exposes this approval workflow. No manual UUID-only
form or fabricated choice source qualifies as UI acceptance.

An immutable receipt retry reauthorizes and returns the original outcome; it does
not rebase the stored supervisor or create duplicate evidence/audit/event rows.
Rollback retains additive schema and historical evidence. Production migration,
release and any rollback require separately named approval.

## Verification sequence

The first real Global API test creates a distinct canonical parent Staff/root,
independently approves it, then submits another Staff member's department posting
with that exact approved supervisor reference. It fails at the intended approval
with 409 `posting_supervision_required` (76.820s). Implement that path, then add
negative/source/concurrency cases one behavior at a time: missing/pending/stale or
member parent, self/repeats/depth, interval/scheduled/expiry, sibling/upward/foreign
scope, target/supervisor/structure changes in both orderings, atomic failure and
exact recovery. Catch critical predicate removal, restore source and run full
Workforce/Global API race suites, architecture, canonical generation, lint/build,
SDK/shell checks and actual independent-account browser acceptance. Keep the PR
draft until that evidence exists; do not close full Staff stories from this slice.

## Backend checkpoint

Public Global API first RED: 76.820s, expected child approval 200 and actual
409 supervision-required. First implemented root/child path: 76.240s. Injected
header/source evidence-write failures, rollback and exact retry: 96.935s. Repeated
Staff under a valid earlier root refusal: 79.476s. Three canonical Staff through
root/department supervisor/descendant sub-unit member: 73.639s, with immutable
recovery and no access grant. A full Workforce process reached its original
10-minute package cap while the current tax test was only 2 seconds old and no
earlier failure/race was reported. A 20-minute process-envelope rerun is active;
no business deadline or assertion was relaxed. Build/vet, scoped lint and canonical
contract/inventory checks pass. Further mutation/direction/depth/source/interval
proof, full suites and normal UI remain required before this increment is ready.
