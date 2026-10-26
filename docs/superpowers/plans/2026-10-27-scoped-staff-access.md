# Scoped staff access implementation sequence

Applies the [scoped access design](../specs/2026-10-27-scoped-staff-access.md).

1. Complete the membership-only prerequisite with a failing real-OpenFGA test,
   the central role key and IAM label, and API grant/revoke coverage. Verify the
   complete permission matrix and the workforce link flow with this role.
2. Establish the published owner-scope verification contract without IAM reading
   workforce tables. Proposal intake now uses the existing event transport and
   registered IAM/workforce consumers; this preliminary result admits review
   only. Approval requires fresh owner verification, not reuse of intake proof.
3. Implement IAM's reviewed assignment lifecycle, immutable history/templates,
   scope epochs and durable projection intent in one transaction. Verify RLS,
   independent review, concurrency, retries and validity boundaries.
4. Implement projection outside database transactions and compare-and-set
   acknowledgments; wire current-state authorization and epoch rechecks into the
   consuming request path. Prove delayed work, partial writes and outages deny.
5. Complete staff-self leave/balance/ledger APIs and UI through Global API,
   resolving staff from the active reviewed link and forbidding self-approval.
6. Verify synthetic authenticated browser journeys and downstream lifecycle
   recovery. Legacy grant migration and production rollout are separate actions.

The graph foundation, step 1, preliminary proposal-scope exchange and independent
review/withdrawal commands are implemented in the worktree. Approval obtains a
fresh owner check and stops at `approved_pending_activation`, granting no access.
Immutable approved permission snapshots, subject/hospital scope epochs and durable
projection intent are now implemented, together with the idempotent graph worker
and compare-and-set acknowledgment. Durable membership generations now invalidate
old proposals/snapshots after final-role removal, preserving attributed revocation
evidence. Guarded IAM activation and the current-state authorization provider now
pass real gateway/database/graph integration tests, including concurrent withdrawal
and expiry. The provider is installed in API bootstrap and the shared gate passes Global API
tests. Production workforce endpoints have not opted in; their narrow RLS and
current staff/link checks remain required.
Staff/link lifecycle checks, broader administrative revocation, UI and browser
acceptance remain open; a graph acknowledgment does not close steps 3–4.
Later steps remain open until their
actual integration tests and review gates pass; no scaffold closes a story.
