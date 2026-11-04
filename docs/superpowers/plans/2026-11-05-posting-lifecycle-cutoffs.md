# Posting cutoff implementation plan

Status: owner command/receipt implementation and legacy refusal in progress;
the complete vertical feature and acceptance remain open. Follow
[the specification](../specs/2026-11-05-posting-lifecycle-cutoffs.md) and
[ADR-0058](../../adr/0058-retained-posting-cutoffs.md). Deliver the owner workflow
as one vertical feature, then integrate actual operating consumers individually.

1. Pin the public wire models/selectors, failure codes and client compatibility
   behavior. Recheck migration numbers; extend canonical inputs and generate
   Go/Zod/SQL. Preserve every older closed IAM template/catalog snapshot.
2. Observe a real Global API test fail for a current independently authorized
   cutoff command. Add forced-RLS immutable cutoff/head storage and exact scoped
   owner routes, command/receipt recovery, source/audit/outbox consequences.
3. Extend owner current-source traversal with target/ancestor cutoffs, lock and
   commit deadlines. Prove both race orderings, stale target revocation, scheduled
   boundary behavior, replay and preserved approval history. Inject persistence
   faults; verify complete rollback. Prove an absent cutoff guard is caught.
4. Add lifecycle-aware discovery/context and strict SDK command/recovery helpers.
   Test exact version bindings, deadline minimum, linkage, old-selector refusal
   and compatibility of unchanged historical receipts.
5. Add normal Staff administrative cutoff/history UI and independent reviewer
   lifecycle context. Test required fields, scheduled instant confirmation,
   unknown command retention, identical retry and scope/revocation clearing.
6. Run full affected tests/checks/builds, migration RLS/immutability tests,
   representative indexed plans, generation/contractdocs/inventory parity and
   secret scans. Perform real local admin/reviewer UI journeys and retained
   persistence checks; keep unperformed evidence explicit.
7. Commit/push a bounded feature PR, reconcile the existing approval/discovery
   stack, and record exact CI/browser evidence before merge readiness. Prepare
   GitOps freight and a migration manifest. Request production approval only for
   the concrete final revision/migrations; verify deployed images, ledger and
   normal-role smoke behavior before tagging that release.
8. Integrate the first actual Pharma operating command with canonical live
   posting/actor references and product-owned unit/credential checks. Repeat
   positive-role, ancestor/local cutoff, suspension/exit/rehire and dependency-
   failure acceptance there. Do not count owner-only coverage as product QA.

The shared Staff tracker remains 10 partial, 10 awaiting audit, 1 research and
0 fully accepted until the respective acceptance criteria actually pass.
