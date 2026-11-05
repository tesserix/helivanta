# Current posting source use implementation

Branch `codex/current-posting-source-20261105`, based on completed local history
increment5c48f95d. Status: owner callback candidate locally verified;
not a complete Pharma consumer or production release. All five reusable Staff
requirements remain in scope.

## Owner implementation

[ADR0060](../../adr/0060-current-posting-source-use.md) and the
[Workforce contract](../../api/workforce-postings.md) define the boundary.
Workforce supplies live factual source admission through the consumer's guarded
transaction, including caller Staff actor, selected/ancestor employment, retained
cutoffs, current structural references and exact original approval proof. Source
use has its own read-only RLS purpose and requires independent scoped authority. A narrow closed
IAM role provides source use/actor use; product permission remains independent.
Owner settings are restored before the consumer callback. An attributable
identifier-only audit shares the same commit and rollback consequences.

The current-use deadline reducer is distinct from approval fit validation:
shortened future employment truncates use, later renewal does not extend nominal
posting eligibility, and future/inactive/stale sources deny. The bounded source
plan is shared with existing supervised approval, preserving its period checks,
32-node ceiling, anti-cycle rules and department references. Product callbacks
receive factual provenance, not a permission token or private HR data.

Persisted candidates: Workforce0086 `posting_source_use.sql`; canonical IAM0028
`assignment_posting_source_templates_generated.sql`. No production or local QA
fixture migration has been applied. Old template definitions/SQL hashes remain
frozen. The two append-only test fixtures were updated for the new latest catalog;
new exact-role tests reject private-read widening and missing actor use.

## TDD and observed evidence

- Deadline and source-plan tests fail before their implementation, then pass.
  Scheduled earlier end, renewal cap, nominal end, future/inactive/stale sources,
  root/2/32-node plans, 33-node refusal, cycles, foreign hospital, wrong revision
  and a member used as supervisor are covered.
- Owner tests include genuine tenant command without IAM evidence: no structural
  dependency or consumer callback is reached. Focused native race suite passes
  **4.705s**. An earlier direct native harness lacked Docker routing; the cached
  existing Colima profile was reused without install/restart.
- Closed-role generator rejects the source consumer before its explicit minimal
  policy is added. Updated append-only fixture/schema tests pass **57**; full root
  script tests pass **96**, no skips. SDK suite passes **737 /81 files**.
- The staged global fixture initially applied0086 ahead of its deliberately
  delayed0075 installation. Including0086 in both delayed lists preserves normal
  registry order and allows the real IAM/OpenFGA/Postgres journey to pass
  **111.184s**. No production migration order was changed.
- Expanded test setup had compile/name and cutoff URL/body errors; those runs are
  failures. Correct setup then passes **118.373s**, proving ordinary own-source
  use, source context restoration, denied raw posting writes, mandatory audit,
  hidden original chain proof, consumer rollback, scheduled ancestor bounds,
  immediate ancestor ending, and actual source expiry despite nil callback return.
  Actor/source evidence rolls back on all failed commands.
- Both database-lock-controlled consumer/writer race orderings pass **125.809s**.
  After injecting the current-actor provider at the platform wiring boundary,
  focused owner tests pass **6.018s**, and real IAM/FGA/Postgres source integration
  passes **222.772s**. Full Workforce passes **492.096s** before that constructor
  injection; the final focused tests cover the injected wiring.
- Final full IAM race suite passes **595.271s**, full Global API race suite
  **727.458s**, architecture race suite **141.268s**, and full shell
  **1,559 tests /467.96s**. Updated web lint/types/format tasks pass **9 /32.651s**.
  Native Go vet/build and final unfiltered lint pass, with **0 lint issues**.
  Earlier architecture runs correctly rejected the unreviewed actor consumer,
  owner-side provider resolution and stale permission matrix. The final matrix
  preserves the platform's coarse TenantAdmin default; only independent scoped
  IAM evidence admits the source callback. No coarse role substitutes for it.
  The IAM migration-manifest test was stale at27 after the additive28th migration;
  its historical assertions remain and the new migration is asserted explicitly.
- Final `next build --webpack` passes with the existing loopback Global API URL,
  TypeScript and69 prerendered pages. An initial command without `API_URL` failed
  configuration validation. Build warnings report the deprecated middleware file
  convention and shared UI React Hook Form re-exports; these are not new source
  guard behavior and are not represented as warning-free validation. Canonical
  generator, contractdocs, final inventory, Go formatting and staged secret scan
  pass. A stale generated registry fingerprint was regenerated without changing
  any surface classification.
- Unfiltered repository-wide Go lint passes **0 issues**; contractdocs generation/
  check and inventory generation/check pass. Inventory stays824 registered /
  801 unclassified. Nineteen whole-file source pins were refreshed only after
  checking the pinned Routes bodies byte-for-byte unchanged; none was promoted.

## Remaining product and rollout gates

Pharma still needs its actual unit-bound professional/operating integration and
trusted hospital/outlet lineage for legacy rows. No source permission is operating
or clinical authority. Relevant IN-PH criteria need actual jurisdiction/service
and qualified review before production policy activation. ADR0054 states it needs
product-owner acceptance before standalone-employer migrations/routes; an explicit
text decision was requested asynchronously and remains pending. Hospital source
work is independent and continues; all standalone work remains in the objective.

The QA runtime is still immutable UI6d541321/backend e1fd098f, ledger237, from the
history increment. This source-use candidate is not activated there. No local
credential changes, tooling installs, VM restart or production change occurred.
The extension's DOM and screenshot controls still fail before interaction, but
native Chrome accessibility recovered normal saved-login OIDC sign-in. The real
access UI loads the verified six-role lifecycle catalog and existing assignments.
A one-day reviewer proposal was created through the UI for the existing synthetic
reviewer, asynchronously verified by its owner and viewed by a separate normal
approver account. It remains awaiting independent review, with no access active;
action-time confirmation for this specific grant is pending. This is preparation
evidence, not positive reviewer acceptance. Staff counts remain10 partial,
10 awaiting audit,1 research,0 fully accepted. No release tag or production rollout.
