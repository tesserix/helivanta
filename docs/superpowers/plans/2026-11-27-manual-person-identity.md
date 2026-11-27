# Manual person identity implementation

Implement the approved manual-source design in the matching specification.

- [x] Define canonical bootstrap, self-case, original-examination and decision contracts.
- [x] Observe input/state/physical-person independence tests fail before implementation.
- [ ] Implement source/person/account/case transition rules with explicit validity.
- [ ] Add IAM-owned forced-RLS persistence, purpose guards, audit and replay receipts.
- [ ] Add validation-only-by-default schema-owner source import command.
- [ ] Extend reviewed governance templates/source use without implicit owner review.
- [ ] Register account-bound, compiled-unreleased case/evidence/review APIs.
- [ ] Add named account projections, SDK and complete manual-review UI states.
- [ ] Verify real database scope, concurrency, source revocation and audit failures hosted.
- [ ] Run focused local builds/typechecks/lints and regenerate all owner/release inventories.
- [ ] Continue qualified clinical-policy approval and actual dispensing enforcement.

No profile is imported live, clinical authority inferred, migration executed or
feature enabled by this plan. Source-method approval is recorded separately from
approval of any actual source profile or operational clinical policy.

Implemented internal validation and pure transitions are described in
[the current API boundary](../../api/manual-person-identity.md). Persistence,
current-record authority and commit-time controls are still unchecked above.
