# ADR-0056: Canonical posting supervisor discovery

Date:2026-11-04. Status: implemented candidate; final acceptance and rollout pending.

## Context and capacity

Workforce owns Staff and reviewed postings. Pharma and future departments must
reference those identities instead of maintaining employee copies. The approved
posting inbox has no retained supervision kind and includes ordinary members;
it cannot directly populate a supervisor picker. Private ancestor decision
context is inappropriate for discovery and contains unrelated rationale.

Inherit ADR0053's unmeasured50 peak requests/s,80:20 read/write,99.9% availability,
p99 below500ms,500,000/1.5million Staff at12/36months and5/15million revisions.
This discovery is read-only, at most8 choices plus one continuation probe and
20KiB escaped JSON per page. The query budget is4096bytes, cursor2048bytes.
Managerial intents are a subset of2.5/7.5million placements; a tenant/hospital/
kind/time/proposal B-tree supports two fixed keyset probes. These are planning
bounds, not measured SLO acceptance. No new service, cache or standing cost is
introduced; existing Postgres storage/index growth is the marginal cost.

## Decision

Add GET `/v1/workforce/hospitals/:hospital/posting-supervisor-choices` through
Global API. Require explicit `purpose=submission|review`; only limit/cursor are
additional query fields. Cursor identity includes tenant, account, hospital,
route and purpose. Require current posting.read, posting.submit or posting.review
for that purpose, and Staff actor use, held together through the read/audit
commit. Broad administration cannot substitute. Existing templates are unchanged.

A new canonical StaffPostingSupervisorChoice contains approved proposal ID and
revision, hospital, canonical Staff label, retained hospital_management or
department_supervisor kind, reviewed department reference, nominal interval,
retained source cutoff and intent creation time. It contains no rationale,
identity evidence, login data, grant or assertion of present eligibility. ID is
the canonical proposal ID; created_at is immutable intent creation time and forms
the cursor with ID. A candidate is a reference hint; selected-chain context and
approval must still revalidate every source and Organization ancestry.

Extend the single existing permissive read policy under a closed scoped read
context for proposals, current approved revisions and managerial intents. Preserve
its exact write check and add restrictive update/delete guards using the prior
read predicate, so discovery cannot widen write visibility. Reuse the existing guarded Staff directory purpose solely within
this three-capability callback. Two fixed kind-specific keyset probes use a new
concurrent index on intents(tenant_id,hospital_id,kind,created_at DESC,proposal_id
DESC); merge at most18 rows to the final9-row probe. No cross-owner table query,
HR supervisor reinterpretation, per-record external call or unbounded scan loop.
Audit supervisor_choices_read with purpose and bounded row count in the same
transaction; audit failure withholds the page and rolls back actor-use evidence.

IAM, Staff actor eligibility and Postgres are critical: unavailability returns503
with no page. Missing current capability returns403; mismatched expected account
returns409. Incorrect/hidden tenant or hospital never returns a foreign choice.
Malformed/repeated filters or foreign-purpose/account cursors return400. Responses
are no-store. Historical approved choices may be stale or ended: the API does not
call them eligible or independently authorize their use.

## Delivery and recovery

Canonical JSON generates Go/Zod models; add a strict SDK getter and normal picker
using Staff names, kind and interval. Preserve all existing v1 receipts, inboxes
and protected context shapes. Department names remain Organization-owned; this
first discovery projection supplies only reviewed department references. Protected
chain review and operating lifecycle enforcement remain separate required work.

Append0082 read-policy migration and0083 concurrent index; historical SQL remains
unchanged. Test migrations on disposable Postgres, keyset continuation across both
kinds, wrong account/scope, omitted read/purpose grant, source failure, audit
rollback and a bounded representative EXPLAIN. Rollback removes route exposure
while retaining immutable data, guarded policies and index. Destructive schema
down-migration is intentionally excluded; a later forward migration may retire
unused read policies after consumers are removed. Production rollout/migrations
require approval for the exact merged revision.

Reusing the inbox would conflate members and supervisor references. Copying Staff
or Organization data would break ownership. A new service/cache adds an unrelated
consistency boundary; the indexed owner projection is sufficient for this slice.


## Current validation evidence

The route first returned404 against the unavailable-owner test; registered routing
fails closed503 (owner test3.430s / package4.571s). Real Global API root/reviewer/chain scenarios
reproduced expected200/actual503 before query implementation, then passed72.479s.
Expanded real pagination/cursor/filter/audit-rollback scenarios passed72.941s.
The first full API run exposed the repository's one-permissive-policy invariant;
separate SELECT policies were replaced by the read extension/restrictive guards
above. The three failing RLS smoke cases pass37.613s, and the full native Global
API race suite passes526.154s at the current application source.

SDK:667 tests pass, including20 new reference/ordering/scope cases. Focused UI:
42 tests pass, including both non-root responsibilities, exact retry preservation,
self exclusion, selection reset, dependency failure and disclosure cutoff. Existing
root receipt recovery remains covered. The root checkbox became the shared
responsibility selector; requests and retained exact-payload assertions remain
intact. Production Webpack build and current types/lint pass. Full shell passes
1,519 tests /182 files in353.80s; full Workforce native race passes646 tests and
subtests in587.931s. Architecture passes128 tests in157.635s after the fixture
switched to the approved tenant database pool.

The actual GORM query and concurrent index descriptor pass EXPLAIN ANALYZE/BUFFERS
on100,000 synthetic postings across two tenants/hospitals with manager/member and
pending/approved mixtures, and the actual scoped context/read-policy functions.
First and continuation probes each return9 rows using the intended index, with
108.630ms and150.569ms execution on the final6.535s fixture run. Relation shapes are reduced and
Staff actor eligibility is represented by explicit scoped session context; actual
IAM/actor authorization and audit are verified separately by Global API tests.
These two executions do not establish production throughput or a p99 SLO.
Discovery-context UPDATE/DELETE attempts affect zero rows. Removing the DELETE
guard deliberately deletes1,298 synthetic rows, so the regression detects the
missing guard. SQL is restored byte-for-byte (SHA256
`41d3b6129c10192477fb45f7d895a7b93f45a494a8cd0b94947f69bf10933d0a`).
The final real migration/purpose/policy run passes75.136s, including unchanged
write checks, one permissive policy per table, cursor binding and audit rollback.
Normal independent-account browser acceptance remains open: reconnection still
fails with the browser tool's timeoutMs error. Protected chain context and
operating/lifecycle enforcement remain required before full acceptance.

The later fixture run measures10,526,720 index bytes for100,000 intents. Linear
planning extrapolation to7.5million intents is about0.74GiB before growth/fill-factor
variation; no new database tier or production throughput claim follows from this.
