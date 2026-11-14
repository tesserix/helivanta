# Staff initiation, reviewed linking and pharmacy-access acceptance

Status: incomplete candidate; no merge or live rollout is established here.
This checkpoint preserves the full goal across the hospital-owned pharmacy and
independent pharmacy/supplier models. Source inspection, component checks,
service integration, browser testing and live acceptance are distinct evidence.

## Candidate and authoritative evidence

- HMS PR: [#1669](https://github.com/tesserix/helivanta/pull/1669).
  Native UI head: `89605942`; approval diagnostic head: `90950803`.
  Source-review correction `0c590133` is local pending the next push.
- Identity owner: [onboarding-service #39](https://github.com/tesserix/onboarding-service/pull/39),
  pinned source `20d240693ac04d779fec504720803a989ca6f5ce`.
  Its private assignment-candidate purpose is a dependency, not a deployed API claim.
- [CI43595306491](https://github.com/tesserix/helivanta/actions/runs/43595306491)
  at `89605942`: web/scripts/secret checks passed; browser job `132448498978`
  passed all three phases. Main browser suite: 99 passed and one skipped;
  subsequent workflow phases: one pass each. The two native operating-page
  cases cover bounded employer discovery and denial without private requests;
  they do not exercise a complete native Staff-to-duty browser journey.
  The Go job was cancelled by the subsequent push; it did not pass.
- [Supplier43595306385](https://github.com/tesserix/helivanta/actions/runs/43595306385)
  at `89605942`, job `132448415183`: all stages through supplier grant projection
  isolation passed, including real owner allocation/recovery, financial
  acquisition and reviewed receiving corrections. The final current-hospital
  Staff lifecycle stage failed in `activation_posting_chain_child` at
  `workforce_posting_chain_test.go:135`: expected 200, received 503
  `posting_source_unavailable`. This is not a successful complete supplier run.
- `90950803` diagnostics passed local race-tested privacy/status cases, backend
  build, affected vet/lint, compile-only owner/gateway tests, contractdocs check
  and redacted secret scan. No authority budget, source predicate or expected
  lifecycle response was relaxed. Its web job found the source-review hash
  mismatch corrected in `0c590133`; this is separate from the posting failure.
- The live environment was inspected read-only on 2026-11-14 UTC: project
  `tesseracthub-480811`, cluster `tesseract-prod-in-gke` in `asia-south1`,
  application namespace `helivanta`, Kargo namespace/stage
  `kargo-helivanta/prod`. Kargo was Healthy/Verified, Helivanta Argo applications
  were Synced/Healthy, and workloads still used `0a1fad14`. Candidate runtime
  behavior cannot be inferred from the health of that earlier deployment.

## Completion gates

Every unchecked item remains required; a passing neighboring row does not close it.
Use only authorized synthetic people, establishments and stock. Keep retained
legacy identity/history untouched as specified by the fresh-tenant plan.

- [ ] Merge and deploy the identity-owner dependency through its owning delivery
      path, then verify the workload-only candidate projection and refusal/audit
      behavior against the deployed owner.
- [ ] Complete a fresh normal tenant signup with a distinct native organization,
      verified address, owner password/MFA and correct hospital or independent
      pharmacy ownership. Use the user-driven identity steps in the
      [fresh-tenant plan](2026-11-14-pharmacy-acceptance-fresh-tenant.md).
- [ ] Complete Staff initiation without requiring operational Staff authority
      from the first owner: bounded native employer discovery, canonical Staff
      and person data, employment preparation and protected evidence.
- [ ] Independently review the person/account link and first digital employment
      activation. Verify wrong organization, nonhuman/unverified account, stale
      identity/profile/link, missing membership, duplicate command, source loss
      and audit/persistence failures. Membership alone grants no business role.
- [ ] Finish the native hospital assignment preparation UI with qualified
      existing-account and Organization-owned department selection. Current
      proposal/account presentation and hospital discovery do not complete the
      missing target selectors; raw subject/department IDs and broad legacy
      directory permissions do not qualify as completion.
- [ ] Propose, independently review, activate, withdraw and revoke hospital and
      department assignments with real current sources, exact tenant/account
      context, stale/duplicate handling, dependency failure and receipt recovery.
- [ ] Resolve the supervised child-posting deadline and the earlier coordinated
      transfer persistence failure. Re-run the entire original current-Staff
      lifecycle test; do not substitute a smaller scenario or change its expected
      200/500 responses to accept unavailable sources.
- [ ] Verify department staff, heads, supervisors, chains and transfers through
      canonical owner workflows, including source cutoff and rollback. Job title
      or department ownership does not confer IAM, clinical or statutory authority.
- [ ] Execute the complete native independent employer -> reviewed Staff ->
      product-owned outlet/premise -> operating proposal -> independent decision
      browser journey for both pharmacy and supplier owners. Include pending
      command scope retention, lost response/GET recovery, stale source and
      unauthorized access. Current discovery-only browser cases are insufficient.
- [ ] Verify effective allowed and denied pharmacy access for current hospital
      and independent Staff, then remove/revoke/change each relevant source and
      verify access cutoff. Check another tenant, employer, department and outlet
      and unavailable dependencies; UI visibility alone is insufficient.
- [ ] Verify the requested collaboration journey on deployed synthetic fixtures:
      effective supplier grant, each stock owner's fulfilment, hospital receiving,
      discrepancy/correction, release and financial integration. The passing
      supplier stages above do not establish this complete live journey.
- [ ] Pass required checks on the actual merge candidate, keep the repository
      public, merge the candidate, execute the approved migrations/rollouts through
      the owning GitOps/Kargo path, and verify the resulting freight, Argo sync,
      workloads, image digests and health. No imperative bypass or private seeding.
- [ ] Test the deployed web login and both Staff/pharmacy journeys with the
      authorized distinct users. Record attributable owner/API/browser evidence
      and all exclusions. Preserve professional/licence/trading boundaries; do
      not use real patients, real licence assertions or real payments as fixtures.

## Current next action

Preserve the running `90950803` supplier lifecycle run long enough to capture its
bounded approval phase/elapsed diagnostic. Use that evidence to repair the actual
failure. Include the reviewed ledger correction in the next push, re-run its
inventory gate, and retain the full completion list above through further work.
