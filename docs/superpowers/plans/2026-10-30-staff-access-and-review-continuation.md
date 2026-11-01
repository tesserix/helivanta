# Staff access and independent-review continuation — 2026-10-30

Status: source audit and continuation plan, not implemented APIs, activated policy
or an approved new role assignment. The deployed boundary is the employment
preparation/evidence release `22b6241`. The [tracker](../../qa/staff-management-progress.md)
and [issue map](../../qa/staff-management-open-issues.md) retain remaining scope.
Follow ADR-0048/0049 and ORG-AC-01/02/04/05/06/07/08; do not settle ORG-D2/D5/D6
by adding a template or claiming a workflow engine exists.

## Source facts and ownership at `22b6241`

- `contracts/staff-access.json` defines three shared IAM templates: leave
  reviewer, identity checks manager and identity checks reviewer. None is a
  general Staff intake administrator. An identity role is not authority to edit
  every private employment/profile/document field.
- `workforce/enrollment.go:enrollmentScope` locks and requires the explicit
  current hospital manager row. `workforce/handlers.go:transaction` sets tenant,
  subject and hospital context; it does not itself introduce an IAM scoped
  assignment command guard. Current flat-permission/manager checks must not be
  mistaken for an implemented delegated HR template.
- `workforce/identity_source.go:identityScopeTransaction` demonstrates the
  current mandatory IAM scoped-command guard and reviewed actor pattern. Its
  identity permissions and semantics must not be silently reused as employment
  decision authority.
- `workforce/login_links.go` refuses a new or active login link unless the Staff
  record is active. This existing guard must not simply be removed to unblock
  onboarding. A design that requires an active operational link before reviewing
  new employment can create a circular prerequisite. Non-digital workers must
  remain supported without creating a global login solely to issue an ID/card.

The `22b6241` source inventory has 207 Workforce routes: 22 declare enrollment
management and 10 document management as their primary permission. These 32
entries are not the full compound authorization contract. The withdrawal
candidate in PR #1463 adds one enrollment-management route: its inventory has
208 Workforce routes, 23 enrollment-management and 10 document-management entries. The enrollment helper
also has credential/reminder and leave-standing/report consumers; changing it
globally could alter those journeys. A scoped-intake change needs an action/route
inventory and tests for these existing consumers, not a blanket helper replacement.
The graph baseline omits newer employment call sites, so confirm against current
source and the generated inventory before implementation.

Workforce owns private Staff/profile, HR classifications, login-link facts,
identity cases, employment terms and evidence. IAM owns membership, reviewed
assignments, scope epochs and authorization projections; the identity provider
owns global credentials. Global API routes UI traffic; owner checks remain
mandatory. No cross-module SQL or copied identity/financial master is proposed.

Assets are private Staff identity/employment files and delegated HR authority.
Threats include another tenant/hospital, a stale/revoked actor, an insider approving
their own history, and an unavailable or lagging projection. Signed current
identity, owner object scope, current permission/assignment generations and legal
state transitions must all fail closed at their actual command/disclosure boundary.

## Bounded implementation sequence

1. Inventory every profile, employment draft and document transaction/disclosure
   phase before introducing scoped HR intake. Inspect callers and consumer tests,
   including file storage I/O between authorization phases and old/new deployment
   compatibility. Preserve legitimate current governance/recovery authority.
2. Design the shared IAM intake template, its immutable assignment/projection
   constraints, current hospital capabilities and UI together. A template-only
   patch is insufficient. Role assignment must not manufacture the product's
   manager grant or grant clinical, payroll, membership or reviewer powers.
   Decide the transaction-bound guard/composition using the actual owner contracts
   before changing permissions or consumers.
3. Define a current-person/identity basis for new hires and non-digital workers
   separately from operational eligibility. Reuse reviewed actor/subject history
   where available; an absent target login is not proof of independence. Do not
   activate employment just to satisfy its identity prerequisite, fabricate a
   verified identity, or bypass current inactive-account protections.
4. Implement immutable employment submission and independent decision as an
   end-to-end owner/UI slice: exact terms/source generations, required evidence
   and review-policy basis, immutable contributor/subject history, bounded queue,
   current authority, reasoned terminal decisions and retained receipts. Complete
   canonical contracts, RLS, audit, concurrency/replay and disclosure checks with
   the UI. No decision may imply operational activation before ADR-0049 consumer
   and legacy-cohort gates are proved.
5. Implement effective expiry, suspension/resumption, notice/exit and rehire only
   with the owner eligibility projection and every actual consumer cutoff. Old
   sessions/graph tuples/queued approvals and failed asynchronous cleanup must not
   retain affected authority. Preserve governance, another hospital's employment,
   clinical/financial history and physical handover acknowledgments.

The simplest rejected alternative is adding a global HR permission/checkbox and
calling it scoped delegation. Also reject removing the active-link guard or
using job category/email/shared ownership as trusted identity or approval proof.

## Planning budgets and acceptance

Reuse the explicitly unmeasured Staff assumptions: 500,000/1.5 million Staff at
12/36 months, 50 peak metadata requests/second, 80:20 read/write planning ratio,
99.9% availability and metadata p99 below 500 ms. Commands remain bounded by the
existing 4–8 KiB owner limits; lists use default 50/maximum 200 keyset pages.
Keep the current monolith/PostgreSQL/file owner; another service/cache/store is
not justified. This document changes no runtime cost. Future schema changes need
bounded additive migrations, retained history on application rollback, measured
query plans and a separately approved exact production candidate.

Acceptance must prove real owner services/ledgers and signed sessions: Staff
Admin, independently assigned reviewer and worker; hospital/tenant isolation;
missing either required permission; explicit owner grant; unknown/stale identity;
withdrawal/expiry/disabled membership; historical self-review; stale generations;
concurrent and exact duplicate commands; projection/outbox dependency failures;
revocation during file retrieval; and new-hire/non-digital-worker behavior without
operational access. Run real DB/race, schema/generator, contracts, lint/build,
web/CI and actual local/deployed UI checks. Scope reductions may omit a journey
only when clearly recorded; they cannot omit its safety controls or display
fabricated completion. Country/state legal/payroll rules still require applicable
primary sources and qualified-owner review before activation.

## Current source inventory — 2026-10-31

The [33-route source/transaction-phase inventory](../../qa/staff-intake-access-inventory.md)
now enumerates all 23 enrollment and 10 document primary-permission routes,
secondary employment-evidence permissions, storage authorization phases and
shared leave/credential consumers. It also records why current template generation
must gain forward-only evolution before adding an intake family. The workspace
assertion correction discovered during this audit is tracked separately; scoped
HR delegation, compound command authority, immutable employment decisions and
all effective lifecycle/cross-module gates remain open.
