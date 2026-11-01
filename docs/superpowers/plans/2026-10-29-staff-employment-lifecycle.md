# Staff employment lifecycle delivery

The complete owner research remains
[staff lifecycle R&D](../specs/2026-10-27-staff-lifecycle-rnd.md); current acceptance
is tracked in [the delivery tracker](../../qa/staff-management-progress.md).
No row below is complete merely because a UI status exists.

- [x] Implement and verify the account-bound/versioned basic editor prerequisite, including
      synthetic manager/reader UI and preserved inactivation/link history.
      Merged as `1be494b0d6def7341ec28c10d4c3596cbe7afc9b` in
      [PR #1454](https://github.com/tesserix/helivanta/pull/1454); full PR CI, local
      Workforce race/coverage and web gates passed. Production rollout is now verified on `22b6241`; see the current Staff tracker. Broader manager/reader and complete lifecycle acceptance remain separate gates.
- [ ] Model employment periods and distinct employee, consultant, individual
      contractor and agency/outsourced-worker relationships. Reconcile #1035 with
      facility contractor/vendor work in #298; a service vendor is not automatically
      a Staff employment record. Contract dates/type, employer, supervisor, permitted
      hospital scope and renewal evidence need owner contracts and required fields.
- [ ] Define reviewed effective transitions: suspend, resume, notice/resignation,
      termination, contract expiry/extension and rehire. Keep security disablement,
      tenant membership removal and employment decisions separate. No fixed notice,
      pay, tax or dismissal policy is invented by the software.
- [ ] Introduce a new employment generation for rehire/resume where required,
      invalidate old bindings/assignments/cards and require new independent approval.
- [ ] Implement the accepted reviewed-employment migration policy in
      [ADR-0049](../../adr/0049-reviewed-employment-access.md); inventory legacy authority provenance;
      publish current employment authority to every actual consumer and prove cutoff
      despite old cookies, FGA tuples, queued work and missed broadcasts. Do not
      implicitly remove tenant governance or another hospital's employment.
- [ ] Complete dated postings/transfers and supervisor changes, handover owner
      acknowledgments, contract renewal and expired-contractor denial.
- [ ] Integrate current identity/background/photo evidence and revocable cards;
      retain historical evidence and separate physical-access acknowledgment.
- [ ] Complete leave/roster/attendance and payroll/final-settlement obligations,
      professional payslips/offer/exit letters, accountable exceptions and recovery.
- [ ] Run synthetic Admin, independent reviewer and worker journeys for every
      transition, foreign scope, self-review, stale/duplicate command, future cutoff,
      dependency outage and same-person/multiple-hospital case before acceptance.
- [ ] Commit, push and merge only verified slices. Review the exact combined
      deployment candidate and migrations; obtain named production approval before
      any new rollout or migration.

The next record/contractor slice is designed in
[reviewed employment periods](../specs/2026-10-29-reviewed-employment-periods.md).
Existing contractor/locum/trainee/volunteer engagement types are already supported;
the missing workflow is reviewed, versioned employment/contract lineage and its
effective authority/closure, not merely adding a dropdown option.

## Registered-consumer baseline — 2026-10-31

- [x] Derive a checked ledger from every registered module HTTP route, async
      subscription, broadcast and unauthenticated allowlist entry; keep missing
      source reviews explicit and reject stale/removed/duplicate classifications.
- [x] Add source-pinned classifications for the four directly inspected MediCore
      encounter handlers; record missing reviewed-employment cutoff as remaining
      work rather than runtime acceptance.
- [ ] Complete the remaining 707 registered source classifications and trace the
      separately wired background/Temporal/external execution paths.
- [ ] Implement reviewed employment and every consumer cutoff with normal
      Admin/reviewer/recipient UI and signed-session boundary proof. The ledger
      classification check alone cannot satisfy this item or authorize activation.

## Employment-evidence contributor prerequisite — 2026-10-31

- [x] Reproduce missing employment-evidence provenance through the existing
      signed Global API/scoped HR intake upload flow.
- [x] Persist Workforce 0059 to extend only the owner-maintained enrollment
      evidence contributor trigger; retain immutable, private history and no
      fabricated legacy backfill.
- [x] Verify human attribution, worker exclusion, duplicate commands,
      unauthorized scopes and atomic failure through real owner transactions.
- [x] Reproduce late edits wrongly satisfying old identity-file provenance; add
      captured-creation checks and before/after-0059 upgrade proof.
- [x] Finish current-source full owner/arch/lint validation after that fix.
      Workforce/Global API/arch/contractdocs race, vet/build/lint and generated
      checks pass; real-provider/browser acceptance remains separately open.
- [x] Locally verify consumption of exact frozen terms/evidence/person-review contributors in the
      separate independently reviewed employment submission/decision workflow.
      Contributor capture alone neither approves employment nor closes A01–A12.

The immutable submission/decision candidate is specified in
[independent employment review](../specs/2026-10-31-independent-employment-review.md)
and [its delivery plan](2026-10-31-independent-employment-review.md). Source now includes Workforce 0060/0061 and IAM 0021 with signed API/SQL/SDK
checks and separate reviewer-assignment UI. Local non-digital preparer/reviewer/scanner workspace acceptance is now recorded
in the [workspace matrix](../../qa/2026-11-01-employment-review-ui-acceptance.md).
Digital recipient provisioning, production rollout, operational activation and
complete A01–A12 acceptance remain open. The current ledger has 724 registered
surfaces, 17 source classifications, 707 unclassified surfaces and zero complete
employment-cutoff acceptances.

## Next operational-generation boundary — 2026-11-01

The next implementation must consume an independently approved frozen employment
review without treating directory activation or session renewal as admission.
Source verification establishes these constraints before new public contracts:

- `tenantdb.WithGuardedTenantCommand` and `WithTenantCommand` already join IAM
  authorization and synchronous Workforce work in one tenant transaction. Reuse
  that boundary; do not introduce nested root guards or cross-owner SQL.
- `workforce_staff_employment_epoch` increments the source epoch on inactive/active
  changes. An approved old-epoch review needs an explicit immutable consumption
  relation to the new operational generation. Re-evaluating its source after
  changing status cannot silently establish that relation.
- Current identity/background/photograph case drafts do not establish completed
  independent verification, card issuance or background clearance. Person-review
  approval alone must not be labelled all verification complete.
- Shared renewal/activity sessions contain no hospital employment generation.
  Operational owners must check current exact employment at their transaction
  boundary and after bounded external reads. Global sign-out or identity disable
  cannot substitute for hospital suspension, exit or contractor expiry.
- Future terms must not end an existing valid period early. At the effective
  cutoff, owner-time checks must deny even with stale graphs, failed cleanup or
  old sessions. Resume/rehire must require fresh authority and cannot revive old
  assignments, links, queued decisions or cards.

Before exposing activation, prove review/source consumption, replay and
concurrent activation, current-actor revocation, effective-time boundaries,
expired-contract renewal, suspended/ended generation denial, dependency failure,
and independent other-hospital/governance access. Classify the remaining legacy
and consumer authority sources and preserve an explicit attributable review queue.
No operational-generation endpoint or runtime enforcement is implemented by this
plan update; the activation gate continues to refuse completion.
