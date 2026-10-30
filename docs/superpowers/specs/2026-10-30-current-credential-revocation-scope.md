# Current authority for global credential revocation

Status: implementation candidate, 2026-10-30. Related: A06/A11, ADR-0047/0050
and ORG-AC-06/07. This preserves the existing explicit security-revocation action;
it does not turn a hospital employment action into global account disablement.

## Source finding and required behavior

`revocationHandlers.adminRevoke` checks a target's tenant binding using
`RoleLister`, then separately commits a global credential watermark. A stale
graph binding for a former member is insufficient authority to affect sessions
in the person's other tenants. The actor's current administrative role and
credential must also remain valid after external lookup and lock waits.

Keep the existing graph lookup, including its dependency-failure 503 behavior.
Honor the existing optional expected-tenant header before this lookup, rejecting
stale/malformed workspace assertions with 409 without external reads or writes.
Before writing the watermark/outbox, use one ordinary `WithTenant` transaction
for the verified acting tenant. Lock and verify the current target membership
and the actor's administrator role. Verify the actor's authenticated credential
against the fresh IAM revocation watermark under the established credential
advisory lock. Hold current-source locks until the attributable global watermark
and existing outbox event commit. No external call runs inside this transaction.

Acquire actor/target credential keys deterministically, using an exclusive lock
for the target and a shared lock for a distinct actor. Self-revocation acquires
the exclusive key directly, avoiding shared-to-exclusive lock upgrades. Order
membership row locks consistently. Respect the request/idle deadline and bound
authorization work. A missing target returns 404; a missing administrative role
returns 403; a revoked acting credential returns 401; unavailable authority
returns 503. Persistence failure rolls back both watermark and event.

Preserve the existing self-sign-out and identity-inactive paths, target watermark
monotonicity, after-commit cache invalidation, event payload and successful
explicit security revocation's global effect. Factor the existing transactional
write so every path uses the same watermark/outbox implementation. No new route,
permission, event, SQL migration, IdP mutation or private grant vocabulary.

## Verification and constraints

First reproduce a stale graph target in the acting tenant with current membership
only in another tenant; assert 404 and unchanged watermark/outbox. Verify genuine
current members still support the explicit global security action. Test actor
role loss and actor credential revocation during external lookup/lock wait;
target membership loss during lookup/lock wait; cross-tenant isolation; self and
reciprocal revocation; duplicate requests; dependency/persistence failure;
signed-session denial and fresh reads after commit. Existing revocation suites
must pass without weakening their assertions or replacing current membership
with a test-only fallback.

Use the planning budgets and single-owner model in the
[admission design](2026-10-30-current-tenant-role-authority.md). No infrastructure
or data migration cost is introduced. Stronger current-source authorization can
deny graph-only legacy authority, so cohort and recovery review remain release
gates. The full [A01–A12 objective](../plans/2026-10-30-staff-rbac-completion.md)
remains open, including provisioning, membership UI and employment lifecycles.

## Local evidence

The stale former-member regression observed 200 before the source fence and now
returns 404 without a target watermark. A stale administrative role also observed
200 before its transaction check and now returns 403. Disabling the fresh actor
credential check in a controlled regression run reproduced 200 instead of 401
when the actor was revoked during the graph lookup; restoring it passes.
Source closure during lookup reproduced 500 instead of the required 503; the
authorization/persistence distinction now passes. A stale workspace assertion
reproduced 200 before the existing expected-tenant helper was added.

Real PostgreSQL tests verify credential/membership changes during observed
advisory-lock waits, self-revocation and two reciprocally revoking administrators
that both passed initial authentication. Exactly one reciprocal action succeeds
and the other observes credential revocation. A real NATS subscriber verifies
the tenant-less published envelope and existing target payload; a schema-level
outbox failure injected only into an isolated test fixture proves the actual
watermark rolls back. No privileged RLS bypass is used for test observations.

The sign-out harness now loads production audit migrations because membership
removal triggers depend on that owner schema. Initial fixture mistakes (an
invalid revocation reason and attempting to read tenant-less outbox rows through
ordinary SQL) were corrected; no source validation, event scope or RLS rule was
weakened to make the tests pass. The local browser's initial redirect mismatch
was resolved by using its already-registered canonical origin, without changing
browser protections or IdP permissions. The clean `e047c56` runtime subsequently
passed fresh normal Admin and reviewer sign-ins, mandatory blank-input rejection,
reviewer Staff-administration/payroll denial and preservation of the reviewer's
existing read-only directory grant. No membership or access grant was expanded.
These checks do not close independent employment review, missing removal/recovery
UI or the full lifecycle acceptance gates. Production acceptance remains open.
