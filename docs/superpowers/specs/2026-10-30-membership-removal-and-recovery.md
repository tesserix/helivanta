# Membership removal and recovery

Date: 2026-10-30. Status: active local command/UI implementation; compatibility,
projection, browser and release gates remain open. Not deployed or accepted.
Scope: A02/A11 of the [full RBAC plan](../plans/2026-10-30-staff-rbac-completion.md),
with A01 identity-owner validation as an activation prerequisite. Related issues:
#40, #118, #417 and #1053. ADR-0044/0047/0049/0050 apply.

## Source findings and authority

`iam/module.go` currently lists immutable membership row IDs but removes by
subject/role. Repeating that DELETE after removal and re-enrollment selects the
replacement row. Its successful response does not identify the removed
incarnation or distinguish a replay. Grant retries return the existing row ID
and republish a projection event, but a grant after removal creates a new row.
Neither command records a mandatory business reason or durable command receipt.
The Staff access UI offers enrollment, not removal or explicit recovery.

An isolated real PostgreSQL/NATS regression on 2026-10-30 reproduced the
incarnation defect: grant returned ID A, removal succeeded, regrant returned
distinct ID B, then retrying the original DELETE returned 202 and the membership
list became empty. The assertion that ID B survives failed. This is reproduced
behavior, not a completed fix. No local application or production membership
was changed by that disposable fixture.

`assignment_membership.sql` already preserves subject membership epochs. Removing
the final role increments the epoch and records attributable immutable evidence;
removing one of several roles does not end tenant membership. Final-role removal
also advances scoped-assignment epochs. Preserve these rules rather than create
a second membership-generation counter. A row ID distinguishes replacement of
one role even while another role preserves the subject's membership epoch.

`sync.go` applies each grant/revoke event directly to OpenFGA. A delayed revoke
can therefore remove a replacement grant; a delayed grant can recreate a removed
tuple. Current-source admission prevents the latter from granting permission,
but does not make recovery availability correct. Source-based projection ordering
and failure/retry tests are required before acceptance.

A second isolated regression used real PostgreSQL and OpenFGA and delivered a
historical grant through the registered grant consumer while IAM held no
membership. The graph reported membership afterward; the expected-false
assertion failed. Current-source admission from PR #1480 separately denies
effective authority in this situation. These tests establish distinct command
and projection defects; neither is resolved merely by passing admission tests.

A third real PostgreSQL/OpenFGA regression delivered the original grant through
the registered owner consumer, removed final membership and inserted a distinct
new membership without projecting it. Current admission still reported membership
from the retained old graph tuple. The expected-false assertion failed. Comparing
source and graph role keys does not bind graph authority to the current row or
generation. This is an additional re-enrollment gap in A11, not a claim that the
merged admission fix completes generation enforcement.

IAM owns membership, command receipts, authority and projection. Zitadel owns
global identity and contact verification. Workforce owns employment and login
links. A tenant administrator acts under `iam.member.manage` in the verified
session tenant; Staff management permission alone is insufficient. A reviewer
does not acquire membership-management authority from a hospital reader grant.
No membership action grants clinical authority, ends employment, erases Staff,
revokes a global credential or changes another tenant.

Assets are membership, governance continuity and private account references.
Threats include a stale administrator tab, a compromised current member, replayed
commands/events and another tenant's administrator. Validate the actor, exact
target incarnation, verified tenant and current state at the transaction boundary.
Client-supplied subject/role values are assertions, never acting authority.
New command/candidate/history routes require exactly one expected-tenant and one
expected-subject header matching the verified session. This prevents same-tenant
account changes as well as workspace changes; it adds no header authority.

## Command and history contract

Add commands under the existing IAM owner and Global API, with canonical models
in `contracts/staff-access.json`, generated Go/Zod outputs and registered routes.
Do not silently change the published meaning of v1 grant/DELETE.

- Removal addresses an immutable membership ID and carries a UUIDv7 command ID,
  expected subject, expected role, expected membership epoch and a required
  bounded reason. It removes exactly that row. It cannot mean remove all roles.
- Recovery addresses a retained successful removal receipt, not a free-text
  subject. It carries a new command ID, current expected epoch and required
  reason. It creates a fresh `staff_member` incarnation only; it never restores
  a privileged role, old assignment, login link, employment decision or card.
  Existing membership is displayed explicitly rather than described as restored.
- A recovery whose prior removed role was privileged still offers ordinary
  membership only. Any privileged authority needs its own current authorized
  assignment and review. Recovery cannot bypass the identity-owner eligibility
  check or any governance recovery policy.
- History is tenant-bound, cursor-paginated and permission-protected. Results
  contain operation, command/receipt ID, actor, reason, original membership ID,
  predecessor receipt, before/after membership epoch, result ID and timestamp.
  Preserve all role-removal history, not only final-membership revocations.

Within one tenant transaction, commit the mutation, immutable receipt, audit and
outbox together. Enforce unique command ID per tenant and immutable results in
SQL. A command ID reused with different operation, target, payload or actor is
409; an exact retry returns its original result without publishing twice. Read
receipts only after current actor authorization, including on retries. A lost
response is recovered by retrying the frozen command; the UI must not generate
a second command ID after a timeout. Retain the receipt after target removal.

Out-of-scope target/receipt returns 404, missing current permission 403, stale
workspace/epoch/incarnation or command reuse 409, invalid input 400, invalid
acting session 401, unavailable identity/authority 503 and failed persistence 500. Responses and history are no-store. Do not disclose another tenant's
membership or identity-provider profile during error handling.

## Concurrency and current authority

Use deterministic actor/target credential and membership locking consistent
with the existing transaction-bound credential-revocation design. Recheck actor
credential watermark and current administrative membership after lock waits.
Scope every query through normal forced RLS and verified tenant. Do not trust
middleware permission resolution as the final write authority.

Serialize per-subject membership transitions and projection through an explicit
documented lock order, including both legacy and new mutation paths. Test this
against the existing membership-epoch trigger; row/epoch locks acquired in the
opposite order can deadlock. No identity-provider network call occurs while
membership transaction locks are held. Recovery first obtains fresh identity
eligibility through its owner, then rechecks command/source preconditions in
the write transaction. Provider verification is evidence at an observation
instant, not a transaction held across two systems; subsequent identity guards
continue to fail closed.

Concurrent remove/recover/regrant and self/reciprocal administrator operations
must have defined outcomes. Removing the final governance administrator requires
the accepted owner-recovery process; it cannot be enabled by ordinary self-removal
or by supplying a different target. Classify current tenant-admin membership
against accepted owner policy before implementing this invariant. Do not invent
an unreviewed support bypass or confuse a Staff administrator with a tenant owner.

Projection must converge to current committed IAM state for the tenant/subject/
role, irrespective of which historical event triggered work. Serialize conflicting
projection work and recheck authoritative state so an older external write
cannot win after newer state. Acknowledging an event requires the desired graph
result; dependency failure retries durably. A source-read followed by an unlocked
graph write is insufficient. Do not treat a 202 database receipt as graph-ready
or operationally authorized. Other roles and other tenants remain unchanged.

The implementation must address an external write whose response times out and
finishes after a newer write. A database mutex alone does not fence an already
in-flight external request if its connection/lock is lost. Do not claim strict
cross-system ordering from an advisory lock or source reread alone. Evaluate
incarnation-specific graph references and current-generation admission against
the existing model, or durable reconciliation with explicit convergence and
pending-state semantics. Record the accepted guarantee before changing the
projection consumer. Keep external network work outside database transactions;
the existing consumer's network-in-Handle pattern is not a reason to expand it.

The graph proof must bind the exact current membership incarnation and each
effective permission; an activation flag combined with a generic permission
lookup can still borrow authority from an old incarnation. Inspect the existing
versioned scoped-assignment pattern and actual resolver/model before selecting a
model. Trace role discovery/tenant switching, owner projections and boot-time
tuple pruning as well as request admission. Older revoke/repair work must not
delete a newer incarnation's authority, even after an external write timeout or
lost lock. The current role-key intersection remains a useful removal fence;
it is not that stronger proof. Do not declare a new graph model or migration
accepted from this proposed direction alone.

Membership recovery also needs owner evidence that historical Staff links and
legacy hospital/outlet/base grants cannot become eligible merely because source
membership exists again. The new command does not update another owner's tables
and does not prove these cutoffs. Retain A03/A04/A11 owner-contract acceptance as
a release gate; source membership IDs and passing command tests alone are
insufficient.

## Legacy compatibility and rollout

The existing raw subject/role DELETE remains unsafe against delayed client
retries while it can remove an arbitrary current incarnation. Adding a new UI
alone cannot close A11. Inventory all callers and publish an explicit migration
and deprecation decision before enforcing incarnation preconditions on legacy
clients. Move first-party clients to the new command and expose a clear migration
error for unsupported legacy removal at the approved contract boundary. Never
silently map an old DELETE to whichever membership currently exists.

The old grant path also cannot provide explicit recovery if it silently
re-enrolls a previously removed account. Define its historical-target behavior
in the same compatibility review and require explicit recovery for ended
membership. Initial provisioning and duplicate retry remain distinct.

New SQL is append-only in IAM's embedded registry, with forced RLS, immutable
evidence constraints and indexed command/target/history access. Test schema
replay and failed writes in an isolated database. Do not edit historical SQL.
Production needs the exact source/migration approval and provenance manifest.
Rollback cannot delete evidence or restore old generations; prefer a forward
correction if a deployed contract or schema must change.

## Admin and recipient UI acceptance

1. A current Admin lists exact memberships with roles and IDs, selects one role,
   sees the distinction from employment/global identity, supplies a reason and
   reviews the precise consequence before committing.
2. Blank reason and stale workspace/account/row reject with preserved input.
   Duplicate click, response loss and reload recover the same receipt.
3. Another role remains usable after one-role removal. Final membership removal
   denies old-session business access while authenticated sign-out remains usable.
   Another tenant's normal session and authority continue to work.
4. Recovery starts from retained history, verifies current identity eligibility,
   shows ordinary-membership-only consequences and saves a fresh incarnation.
   Old assignments, links and cards remain invalid. A fresh proposal and distinct
   review are necessary to regain hospital operational access.
5. Replaying the old removal after recovery cannot delete the new row. Replaying
   old grant/revoke events cannot change effective authority or strand recovery.
6. A restricted reviewer cannot list private membership history, remove, recover
   or acquire additional authority. Its legitimate hospital reader scope remains.
7. UI pending/error/success states distinguish database commit, projection pending
   and actual authorization. No fabricated verification or completed-access claim.

## Validation and budgets

Planning assumptions, not observed traffic or production SLOs: 10 peak commands/s
per large tenant, at most 4 KiB command body, 100:1 read/write, 100,000 subjects
and 1 million receipts at 12 months, 300,000 subjects and 3 million receipts at
36 months. Target local owner transaction p99 below 100 ms excluding lock waits;
end-to-end provider/graph operations have explicit bounded timeouts and retain
pending/retry state. No new service, datastore, paid dependency or worker fleet.
Validate indexes at 100,001 memberships and history pagination at realistic
receipt volume. These are test budgets, not hospital availability guarantees.

TDD must cover real Postgres/forced-RLS state, exact retry, stale incarnation,
other roles/tenants, reciprocal/current-actor lock waits, source/provider/graph
failure and transaction rollback. Projection tests use real OpenFGA and observed
ordering, not a recording writer alone. UI acceptance uses normal Admin/reviewer/
recipient login through Global API. Run canonical generation/check, contractdocs,
affected race/coverage/vet/lint/build, full required CI and deployed acceptance.
Record each environment separately in the delivery tracker. A02/A11 remain open
until these gates and legacy compatibility are proven.

Rejected: raw subject DELETE for a new command loses incarnation safety;
global sign-out for tenant removal harms unrelated scopes; regranting historical
authority on recovery bypasses review; adding receipt tables without first-party
UI does not prove the workflow; graph tuples alone cannot establish current IAM
authority; replaying historical events without current-state reconciliation
cannot guarantee recovery.
