# ADR-0049: Reviewed employment basis for hospital operational access

Date: 2026-10-29. Status: accepted by the product owner in the Staff lifecycle
conversation. This records a policy decision; it is not a production activation
or a claim that all protected consumers already enforce it.

## Decision

Hospital operational access must have a reviewed Staff/employment link. Existing
legacy grants without that basis must enter an attributable review queue before
enforcement is activated. Do not infer an employment binding from an email, job
title, department, shared ownership, manager checkbox or existing grant alone.

Governance and recovery access remain separate. One hospital's employment action
must not disable the person's global identity, remove another hospital's valid
employment, appoint/remove a tenant owner, or bypass the verified recovery path.
Clinical credentials and privileges remain additional owner checks; an employment
link cannot grant clinical authority.

Review must identify the verified tenant, hospital, canonical Staff, login subject,
employment generation and affected operational assignments/grants. Preserve the
source authority, reviewer, decision, scope and migration history. A person cannot
approve their own link or access; reuse independently reviewed IAM/Workforce
boundaries rather than creating a second permission system.

Suspension, accepted exit and contractor expiry must deny affected employment-
derived authority at the effective cutoff even with old sessions/FGA/cache or
failed asynchronous cleanup. Resumption/rehire must not resurrect historical
links, assignments, queued approvals or cards: require current reviewed employment
and fresh authority decisions. Preserve clinical, payroll, HR and audit history.

## Activation gates

Inventory every operational consumer and existing authority source. Surface
unclassified grants and unresolved decisions before activation; do not silently
assume all grants are employment-derived. Confirm the governance/recovery path,
review the migration cohort, make effective cutoff authoritative, and verify
current signed sessions and transaction-boundary races across actual owners.

The review queue, owner contracts, lifecycle transitions, legacy migration,
consumer enforcement and complete end-to-end QA remain implementation work.
Production rollout/migration requires approval for its exact revision and action.
No existing production grants are changed by this ADR.

## Alternatives and related decisions

Rejected: blanket global identity disablement for a hospital suspension; silently
classifying legacy grants as reviewed employment; restoring old authority after
rehire; treating an inactive directory flag as complete offboarding.

Read with ADR-0044 and the Staff lifecycle research/delivery tracker. This decision
is distinct from ORG-D6, which asks whether tenants may define new permissions.
ORG-D2, ORG-D5 and ORG-D6 are not settled by this decision.

## Source availability decision — 2026-11-02

The product owner explicitly requested production source activation be switched on
and remaining Staff work continue. This authorizes the reviewed-source eligibility
and consumption commands on approved `02b1e2b7`, through GitOps PR #1619, after
local digital-recipient and deterministic lock acceptance. The command records its
employment generation and transactional IAM basis; fresh login linking and scoped
assignment decisions remain independently approved. It grants no legacy role,
clinical privilege, payroll or other product authority.

This does not activate a blanket migration of existing operational grants or settle
legacy/governance cohorts. The operational consumer, hospital-specific suspension,
exit, contract-expiry and non-resurrection obligations above remain required.
Detailed browser/source-cutoff evidence and remaining limits are recorded in
[the 2026-11-02 acceptance](../qa/2026-11-02-employment-activation-enablement.md).
