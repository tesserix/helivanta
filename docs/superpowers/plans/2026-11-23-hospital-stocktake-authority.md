# Hospital-owned pharmacy stocktake delivery — #1686

Status: implementation plan, not implemented authority or release approval.
Depends on the independent worker continuation in #1823. This completes the
hospital side of the [reviewed authority design](../specs/2026-11-23-reviewed-stocktake-authority.md);
it does not redefine the issue around the independent path already implemented.

Development checkpoint: the candidate now declares Workforce-owned
`workforce.stocktake_actor.use` and a separate callback guard using the existing
current-employment checks. Source declaration and missing-basis unit checks are
tested. The module declares no legacy role grant; the independent permission
matrix records the platform's existing automatic tenant-admin reconciliation,
which cannot establish the exact guarded assignment or current Staff basis.
Canonical department-only reader/counter/reviewer templates and their separate
assignment-list route are now candidates under forward IAM migration
`0056_iam_pharmacy_stocktake_templates`. SDK family routing and release locking
have focused tests. An actual reviewed-employment fixture now additionally
exercises this guard under the precise department assignment, including purpose
separation, scope/escaped-context denial and audit rollback. The prior current-source fixture passed hosted execution in run 45133937513.
A hospital historical-participant guard and regression fixtures are now candidates;
their complete hosted fixture passed in run 45181539642. These changes do not yet provide a
hospital count consumer or operating UI.

## Verified starting point

- Pharmacy's `operatingStock` consumes independent IAM duties. Its employer guard
  correctly rejects a hospital-owned pharmacy. Hospital Staff must never be
  represented by fabricated independent employment to pass this check.
- `fulfilment_stock.go:hospitalFrame` demonstrates current approved
  outlet/hospital/department binding and Organization-owned revision checks. The
  Supply-owned `HospitalSourceAuthority` uses procurement actor eligibility and
  fulfilment permission; it must not authorize a physical stocktake.
- Workforce's `staffActorGuard.withCurrentEmployeeActor` locks current Staff,
  account link and employment generation, requires an already-held exact IAM
  source-use grant, constrains its guarded transaction deadline and audits the
  consumed source. It currently permits governance and procurement purposes only.
- `IndependentStaffHistoryGuard` verifies retired observation evidence and known
  Staff/account aliases. It does not establish hospital employment history.
- Pharmacy's current header discriminator permits legacy and independent
  operating counts. Independent evidence records duty, Staff revision and link
  revision; hospital evidence needs assignment and employment generation instead.

## Owners, authority and invariants

Pharmacy owns one existing snapshot/count/command/adjustment/physical ledger.
Workforce owns hospital Staff, account-link and employment-generation history.
IAM owns reviewed department assignments and their acknowledged snapshots.
Organization owns the department revision. Pharmacy owns the approved outlet
binding. No module reads another owner's private tables or creates a second
Staff, stock or account master.

Use department-scoped reader, counter and independent reviewer templates in
`contracts/staff-access.json`. Counters receive stocktake read/count, reviewers
read/review, and readers read only. Each template includes a separate
Workforce-owned stocktake actor-use permission. An owner role, hospital grant,
posting, job category or procurement assignment grants none of these operations.
No dispensing, stock release, controlled-drug designation or financial permission
is added.

Retain ADR-0073's budgets: 10,000 snapshot buckets, 100 observations per command,
500 posted differences, 200-row pages, five-second start/post and page p99 below
300 ms. The existing planning workload is 10 commands/s with 10:1 reads/writes;
200 outlets produce approximately 14.4 million snapshot rows/year. Hospital
source locks, historical participant checks and binding checks must be measured
inside these budgets, without changing their thresholds.

## Delivery sequence and acceptance

1. Add red-first real Workforce/IAM tests for a separate hospital stocktake
   actor-use boundary. Valid current reviewed employment succeeds only inside
   the exact department assignment's guarded transaction. No IAM basis,
   governance/procurement purpose, wrong tenant/hospital/department, retired or
   replaced account/generation and expired employment all refuse before callback.
   Dependency/audit failures fail closed. Keep the source-use permission without
   legacy role grants. Do not ship an unused wrapper: wire it to the hospital
   stocktake consumer in the same implementation continuation.
2. Add a callback-only Workforce historical independence check for hospital
   observations, under the reviewer's current held authority. Retain exact Staff,
   account-link version, employment generation and employment epoch. Verify the
   original accepted source even after retirement; reject same Staff, original
   account, relinked account and historical alias overlap. Missing historical
   proof is a refusal, never independent-by-default. Query through owner RLS,
   audit before returning and restore narrower transaction settings on all exits.
3. Register the hospital route family under
   `/pharmacy/hospitals/:hospital/departments/:department/pharmacies/:pharmacy/outlets/:outlet/stocktakes`.
   Resolve the canonical department path through its owner. Hold exact current
   IAM operation and actor-use authority, Workforce actor eligibility, approved
   binding and current department revision through the Pharmacy command commit.
   Bind every lookup to verified tenant, hospital, department, pharmacy and outlet.
   Return 404 for foreign or unavailable resource scope, 403 for a proven scope
   with missing operation permission, 409 for stale source/session/command intent,
   401 for revoked credentials and 503 when required authority or audit cannot be
   evaluated. Confirm these errors against the final registered router/middleware.
4. Reuse existing stocktake protocol helpers for start/all/selected, lines,
   counts, submit/cancel, independent approve/reject and CSV. Add a hospital
   authority discriminator and append-only hospital evidence, with assignment
   snapshot, binding/revision and Workforce source references. Evidence is not a
   duplicate count or Staff master. Legacy and independent commands must not
   adopt hospital history; hospital commands must not adopt their history either.
   Reserve actual forward migration IDs after rechecking merged registry state.
   Keep historical migrations immutable, tenant/composite foreign keys, forced
   RLS, UUIDv7 evidence IDs and bounded migration lock/statement timeouts.
5. Test current-duty/assignment separation and participant independence through
   Global API with real IAM, Workforce, Organization and Pharmacy owners. Cover
   binding revocation/replacement, changed department revision, sibling/foreign
   scope, current Staff/link/employment changes, concurrent correction/reservation,
   complete rollback, exact replay and source/audit failure. A current reviewer
   may review retained observations from a retired different counter only when
   Workforce can still prove attribution and historical independence.
6. Add named current-account hospital working-location discovery and exact-outlet
   product/quarantine projections. Do not use management, procurement, general
   catalogue or receiving discovery as worker authority. Use sealed bounded
   pagination and current account/scope/version cache keys. Keep independent
   directory behavior intact; hospital and independent choices need explicit
   authority discriminators rather than guessed routing.
7. Connect the same start/count/review/export UI to the hospital authority.
   Current assignment hints govern presentation; every API operation reauthorizes.
   Account/scope/source/read failure removes private state. Persist only scoped
   command references, not private counts or tokens. Capability withdrawal must
   preserve exact original receipts, retained status and authorized cancellation
   while removing new-work controls and lines/export.
8. Run the real built-shell journey with separate hospital owner, access approver,
   counter and stocktake reviewer. Observe count and approval response loss,
   reload recovery without another mutation, selected/all snapshots, scheduled
   discrepancy sections and unchanged later receipts. Re-run maximum capacity
   through actual hospital source admission; independent/legacy measurements
   are not hospital performance evidence.

## Merge, rollout and closure

Run focused hosted builds/race tests and canonical generation/checks, then the
current-head required PR checks. Align contracts, registered inventory and
consumer/release bindings without implying live acceptance. Keep the repository
public and do not use Colima or start local apps/databases/containers.

Before the protected production release, enumerate the actual pending migrations,
capture the affected schema definitions and obtain named migration/rollout
approval. Migrations are forward-only; application rollback preserves new
history and needs an older-code compatibility check. Promote signed main images
through Release/Kargo and verify migration completion, Freight/image revisions,
readiness and actual authenticated browser behavior.

Stocktake remains release-locked until the applicable independent and hospital
acceptance gates are satisfied. Do not enable general Stock/Catalogue as a
shortcut. After the reviewed release change, use Console's audited desired-state
control and verify effective UI/API behavior under real authorized synthetic
Staff, including both owners and negative scope checks. Record pending cases
explicitly. Close #1686 only after its requested end state is demonstrated live.

### Hospital historical participant verification (candidate)

Workforce retains ownership of canonical login-link history and employment
generations. A stocktake reviewer must carry both a current department-scoped
`workforce.stocktake_actor.use` assignment and `pharmacy.stocktake.review`.
The historical check binds the saved counter link version, generation and
employment epoch, rejects the same Staff or subject and shared historical Staff
aliases, and does not require the former counter to remain employed. It restores
its narrow read context before calling the Pharmacy consumer; failed consumer
work rolls back the Workforce audit too.

Candidate migrations `0122_workforce_stocktake_actor_history` and
`0123_workforce_stocktake_actor_history_index` preserve the previous selection
integrity checks, update only the three explicitly changed policy fingerprints,
and refuse prior authority drift. Runtime checks require canonical immutable
history and forced row-level security. These are development changes only:
hospital commands, UI and live migration/rollout
remain pending; the release capability stays compiled ineligible.

### Application source wiring checkpoint

The application and integration harness now resolve a combined current/history
stocktake guard from exactly one Workforce owner. Missing database, missing or
duplicate Workforce ownership, or an incomplete source provider deny admission.
The provider retains the existing Workforce factory and does not expose
procurement authority. The real-owner platform test verifies these refusals and
that neither current nor historical source callbacks run without guarded IAM
authority. This wiring adds no hospital route or stock write admission by itself.

### Hospital command candidate

The command branch now registers the hospital stocktake route family, reuses the
single stocktake snapshot/observation/decision/ledger implementation and retains
separate hospital evidence. Admission holds the actual operation and Staff-source
assignments, locks each assignment's department scope against current ancestry,
verifies current reviewed Staff, and pins the approved hospital-owned outlet
binding and Organization revision through the command transaction. Historical
review consumes only Workforce's hospital identity boundary. Independent employer
admission and its evidence remain separate.

Source verification found the current last Pharmacy migration is **0065**, so the
new forward-only candidate is `0066_pharmacy_stocktake_hospital`. No live migration
has run. Its new evidence table has forced RLS, composite source/command foreign
keys, immutable history, bounded participant indexes and operating-choice
restrictions. The prior choice integrity is verified before adding the new table's
policy fingerprints. Hospital and independent command triggers require original
source evidence; legacy commands cannot adopt either count. Explicit route
capability bindings preserve authorized recovery while new work remains locked.

Hosted run 45183518049 observed the intended red at the hospital start route
(404, expected 200), after real owner setup, department approval, stock receipt
and reviewed assignments. Positive command acceptance, binding-change recovery,
hospital discovery/UI/capacity and authenticated live release verification remain
pending. No readiness or rollout claim follows from compilation or static SQL
parsing.

### Hospital client scope checkpoint

The SDK now selects hospital routes only from explicit hospital authority plus
both hospital and department coordinates. Incomplete or ambiguous authority fails
before traffic/cache access. Cache and tab-local recovery keys include authority,
account, tenant, hospital, department, pharmacy and outlet; existing independent
and legacy recovery keys retain their prior bytes. Recovery hooks observe
hospital/department changes. Selected-product start consumes the hospital
stocktake's named product/quarantine choices without general inventory grants.
Red-first SDK, selected-product form and recovery tests exposed the unsupported
route and the legacy recovery-key collision before these were corrected.
Hospital working-location discovery and the real built-browser journey remain
pending; these reusable components alone do not create hospital worker access.
