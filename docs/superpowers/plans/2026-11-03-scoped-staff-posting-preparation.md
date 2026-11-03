# Scoped Staff posting preparation delivery plan

Follow the [specification](../specs/2026-11-03-scoped-staff-posting-preparation.md)
and retain the [parent delivery plan](2026-11-02-reusable-staff-foundation.md).
This is the next implementation slice after deployed `305294ab`; none of its
unchecked runtime gates is completed by publishing this plan.

## Source and boundary review

- [x] Verify deployed preparation remains draft-only and uses legacy hospital
      grants; normal restricted-reviewer denial is not positive scoped acceptance.
- [x] Inspect IAM current assignment/membership/basis guards, the two-permission
      bound and root expiry cancellation.
- [x] Inspect current Workforce actor/generation checks, owner-private purpose
      restoration, reviewed evidence locks and returned effective cutoff.
- [x] Identify the legacy nil-generation exception that new scoped access must
      reject without silently changing older unrelated workflows.
- [x] Inspect Organization's structural guard/list and the current exclusive
      supervision gate that also protects same-command retry.
- [x] Reconcile affected API/index guidance with the deployed release; preserve
      open clinical/default-role and full operating-lifecycle gaps.
- [ ] Audit the complete affected source-writer lock graph, including identity,
      review, profile, source-end, assignment activation and structural writers.
- [x] Resolve ADR-0052's closed provider/wiring allowance in architecture tests;
      prove no cross-module business calls or cross-owner table access.
- [ ] Freeze exact additive owner routes/projections, canonical fields, errors,
      permission/read compatibility, cursor bindings and payload budget.

## Implementation cycles

Complete one public-behavior RED → GREEN cycle at a time. Integrate both real
source-guard consumers within the feature before merge; do not ship an unused
authorization interface as a completed Staff workflow.

1. **Current actor and command boundary.** Start with a public consumer request
   lacking current IAM binding; observe refusal. Implement IAM-minted ephemeral
   scope/action binding and Workforce-owned actor validation. Prove missing,
   legacy-generation, wrong-subject, future/expired and changed evidence states.
   Prove foreign database/tenant/action, escaped callbacks and exact deadlines.
   Keep private findings inside Workforce and propagate the earliest cutoff to
   the root guarded transaction rather than only a child query context.
2. **Organization choices.** Observe the scoped preparer's current refusal;
   implement its owner-declared read-only choice permission/projection. Reuse
   canonical structural models, active exact revisions, bounded cursor pages,
   atomic disclosure audit and the current Staff actor guard. Prove foreign
   scope, retired/changed revisions, role withdrawal and dependency failure.
   No structural mutation or broad governance history becomes accessible.
3. **Posting authorization and persistence.** Observe scoped preparation/read
   refusal, then implement additive current-grant/actor admission and narrow
   purpose RLS. Preserve legacy v1 semantics without fallback after scoped
   admission. Prove reader-write denial, stale source, actor pin changes,
   audit/outbox rollback, source-ending races and both lock orderings. Preserve
   exact retained retries, including simultaneous same-key creation.
4. **Immutable access catalogs.** Add new preparer/reader keys and append a new
   catalog/migration; never rewrite previous catalog snapshots. Exercise real
   independent proposal/review/activation and OpenFGA projection against the
   runtime consumers. Test self-grant refusal, membership/credential revocation,
   current generation expiry and changed assignment epochs. Verify each template
   grants only its declared read/prepare/choice capabilities.
5. **Canonical SDK and normal UI.** Generate models; add scope-bound discovery,
   directory/source projections and SDK methods/query keys. Reuse the deployed
   editor/history/recovery behavior. Prove overlapping role discovery, original
   account binding, cancellation, inaccessible-state cache clearing and no
   reader editor. Complete normal local logins as Admin, independently approved
   scoped preparer, scoped reader and unrelated reviewer; capture screenshots
   privately and verify persistence with read-only SQL.

All five cycles belong to one coherent feature branch; these are development
ordering steps, not independent claims of full acceptance.

## Verification and release gates

- [ ] Real PostgreSQL/OpenFGA tests cover foreign scopes, retained sessions,
      revoked roles/links, current source/deadline and dependency failure.
- [ ] Deterministic source/command ordering, same-key races and rollback pass;
      remove a critical lock/predicate, observe failure, restore and rerun.
- [ ] Maximum Unicode choice page fits the documented serialized response budget.
- [ ] Affected full Go race suites, build/vet/lint; SDK/shell tests, typechecks,
      lint/format and production-style build pass using cached dependencies.
- [ ] Canonical generators/checks, contractdocs generation/check, architecture
      tests and reviewed source-inventory evidence pass without blind hash refresh.
- [ ] Normal local browser matrix and retained-command recovery pass; one master,
      exact history/audit and no operating authority are verified in persistence.
- [ ] Final exact-head CI passes; commit identity resolves from origin and the
      public-repository requirement is checked before merge/release.
- [ ] Specific production approval names the merged revision and every new
      forward migration. Persist SQL and record recovery before promotion.
- [ ] GitOps promotion and installed schema are verified; normal synthetic
      production preparer/reader acceptance passes with private evidence.
- [ ] Update the full Staff tracker and affected specifications with actual
      evidence. Keep #417/#418/#429 open until all their distinct acceptance
      criteria pass; do not apply a full Staff E2E tag for this slice.

## Required subsequent parent work

Independent effective posting approval; supervisors and cycle protection;
transfers and contractors; suspension/end/resume/rehire; standalone employers;
Pharma professional acceptance, PIC/cover/assistant duties and modern/legacy
operating enforcement; complete Staff audit/export and end-to-end role coverage.
Each remains a tracked implementation/acceptance obligation after scoped
preparation, with jurisdictional professional policy reviewed under the relevant
India primary-source gates before activation.

## Development checkpoint — 2026-11-03

The candidate now contains append-only reader/preparer catalogs, a distinct
posting assignment list, SDK/admin catalog discovery and a Workforce current
actor guard. Catalog/projection, unavailable/malformed-catalog UI and historical
link rejection have local regression evidence in the [tracker](../../qa/staff-management-progress.md).
The historical-link integration exercises real independent IAM approval rather
than minting source-use evidence outside IAM. Prior catalogs remain unchanged.

These components do not check off cycles 1–5 or their release gates: guard
consumer composition, current department choices, posting-purpose
RLS, discovery, positive digital-actor and command ordering tests, normal scoped
browser acceptance and production rollout remain unfinished. No standalone
foundation component is to be merged/deployed as completed posting acceptance.

## 2026-11-03 candidate checkpoint

Tasks 1–4 and the scoped workspace implementation in Task 5 now have local code.
Workforce exposes separate `/posting-staff/:id` routes and bounded workspace/target
discovery; current department choices are guarded by Organization. Migration
0074 retains the tenant-policy validator and denies scoped Staff writes/deletes.
SDK/UI tests distinguish reader/preparer behavior and the scoped save path.
Normal-role acceptance remains open, with positive core journeys now verified.
A fresh digital actor completed normal UI source/evidence, independent reviews,
activation and link/assignment decisions. Reader history/reload and preparer
required-field create/edit/history/reload plus unsaved-navigation protection pass
without a legacy hospital grant. The old revoked-review source remains negative
evidence. See the [UI checkpoint](../../qa/2026-11-03-scoped-posting-positive-ui.md).
Follow-up normal UI acceptance also verifies exact receipt recovery after an
owner-committed response is deliberately lost, preparer withdrawal with fresh
reader-only discovery/history, and unrelated reviewer denial. One retained
command produces one revision/audit; the local relay is removed and original
routing restored. Retained-session cutoff and the remaining account/foreign-scope
negative matrix remain required. PR #1532 stays draft; full acceptance tags and production
promotion remain withheld pending their distinct gates and named approval.
