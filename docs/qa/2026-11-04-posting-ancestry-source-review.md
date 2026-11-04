# Posting ancestry source review — 2026-11-04

The operational source ledger correctly refused the changed Organization guard.
Review of both actual Workforce callers (`putPostingDraft` and posting proposal
submission) confirms they construct `DepartmentReference` without the optional
ancestor pair. The direct path retains verified tenant/hospital, exact active
revision, active guarded-command/database checks, the hospital structural gate
and row SHARE lock. Stable sorting has one element for this path. No route,
permission, registration, clinical authority or operating posting changes.

The new paired internal constraint rejects partial/invalid inputs and requires
both current active revisions and a same-hospital active root-reaching path.
Sibling/upward/retired/stale/unknown sources deny. Owner depth remains eight.
An intermediate public department move remains fenced after callback return
until the outer consumer commits; afterwards the old ancestry is denied even
with an unchanged leaf revision. Missing/hidden structure cannot imply a root.

Full restored-source race suites passed: Organization 125.467s, authorization
11.035s and architecture 103.945s. Removing the ancestry denial, row SHARE lock
or hospital structural gate each failed its corresponding real regression test;
original source bytes were restored and SHA256 verified each time. The real
5,000-node plan fixture uses keyed seed/parent probes after normal auto-analysis;
this is not throughput/SLO acceptance. No elevated application role was added.
Vet, build, scoped lint and contractdocs check also passed.

Only the matching guard hash and rationale are updated in the review ledger.
Regeneration preserves classification and every remaining runtime obligation.
This source review does not activate a root management capability, positive
posting approval, lifecycle enforcement or normal-role browser acceptance.
See [ADR-0053](../adr/0053-reviewed-managerial-posting-supervision.md) and the
[Staff tracker](staff-management-progress.md).
