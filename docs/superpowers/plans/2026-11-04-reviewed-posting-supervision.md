# Reviewed managerial posting implementation plan

Implements the proposed [contract](../specs/2026-11-04-reviewed-posting-supervision.md)
and ADR-0053 on top of the independently reviewed #1551 ancestry source guard.
This plan is not implementation or production acceptance.

1. Write a failing SDK contract test for a valid explicit root intent, preserving
   omitted-intent compatibility. Add the canonical nested model and generate Go
   and Zod; then alternate red/green for complete non-root Staff/posting references, invalid roots and IDs,
   strict unknown/null rejection, and decision attestation constraints.
2. Add real PostgreSQL tests through the Global API for retained root intent and
   immutable exact retries. Implement append-only typed owner storage, guarded
   RLS/purpose and immutable source linkage. Preserve all legacy public receipts.
3. Test the separate closed root-management IAM catalog and projection before
   adding its generated migration. Preserve every old catalog/snapshot and test
   grant expiry, withdrawal, same-hospital actor proof and independent decisions.
4. Test a successful independent root approval, then missing/wrong/expired
   capability, self/contributor/retained associate and target-source failures.
   Implement compound grant admission before owner locks, exact transition,
   source epoch, audit/outbox and immutable recovery. Legacy omitted intent still
   refuses approval. Verify current-source and transaction deadline races.
5. Test an approved supervisor and descendant/member success, then sibling,
   upward, cross-hospital, stale, ended, scheduled, interval, self/cycle/repeated
   Staff, member-as-supervisor, missing-root and depth-bound failures. Implement
   bounded current chains and Organization ancestry callback through commit.
6. Add canonical protected supervision context/choices with explicit budgets and
   current scope; test no private disclosure after source/account changes.
   Add normal submitter/reviewer selection and approval UI, immutable uncertain
   commands, recovery, required attestations, reload and private-state cutoff.
7. Catch removal of each critical root/independence/chain/source/interval/gate
   predicate, restore exact source, then run complete affected race suites,
   relevant Global API journeys, SDK/shell suites, generators, architecture,
   lint/types/build/format, source-review inventory and secret checks.
8. Keep the PR scoped to reviewed supervision/positive approval and its normal
   UI. Rebase only the new feature commits once #1551 merges; its independently
   tested source guard is a prerequisite, not duplicate feature work. Commit,
   push, merge after checks and request the exact new migration/rollout approval.
   Preserve full lifecycle/transfer, employer-scope and departmental/Pharma
   enforcement obligations in the shared tracker; continue those next.

## API checkpoint — 2026-11-04

The root independent approval pair and missing-management/missing-attestation
cases pass with real Global API, PostgreSQL and OpenFGA dependencies (54.953s,
race enabled). The final real two/three-check IAM pair passes in 47.776s; full authz
passes in 6.842s. These complete part of step 4 only. The fully entitled target/submitter
case is written but two runs failed in earlier fixture setup, before its assertion.
The full IAM recovery failure, remaining negative/concurrency coverage, UI,
supervisor chains, lifecycle consumers and deployment gates remain open.
Progress, exact failures and the isolated fixture PR are recorded in
`docs/qa/staff-management-progress.md`. No step is marked fully accepted by this
checkpoint.
