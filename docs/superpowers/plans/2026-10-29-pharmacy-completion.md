# Pharmacy completion sequence

Worktree: `/private/tmp/helivanta-pharmacy-completion`.

- [x] Check accepted main and concurrent work. #1456 fixes registration envelope;
      #1453 implements ward billing. Do not duplicate either.
- [ ] Unused verified-UPI: immutable scoped refund record, mutually exclusive
      sale/refund, counter customer/tender hold and recovery tests.
- [ ] Prescription-image enforcement: require released evidence for new cited
      sales, preserve existing invoices, fail closed pending worker rollout.
- [ ] #1406: prepare and validate least-privilege worker GitOps rollout, including
      scoped storage/credentials and synthetic acceptance.
- [ ] Real browser journey: verified UPI, refused customer switch, complete sale,
      then return; cover unused refund recovery separately.
- [ ] #1391: scanned licence evidence before review.
- [ ] #1371: versioned reviewed NPPA ceiling data and fail-closed scheduled pricing;
      verify current primary sources and record activation review requirements.

Each slice gets its own validation and reviewable commit/PR. Do not activate
unreviewed drug/tax policy or perform real patient/payment transactions.

## Unused UPI local evidence (#1459)

- Regression observed red: paid UPI left Customer and Paid by editable.
- 25 focused counter/UPI component tests pass, including refund recording and
  Enter refusing accidental sale submission.
- Full pharmacy race tests pass (424.7s); bootstrap owner-event integration tests
  pass (61.6s). New refund tests also pass repeatedly with `-race -count=3`.
- The new receipt GET needed the architecture allowlist because it returns one
  primary-keyed receipt, never a collection. The corrected pagination gate passes.
- Pharmacy Go lint passes. API/event inventory was regenerated and checked;
  canonical refund input is generated from `contracts/pharmacy.json`.
- The complete browser journey and production deployment are still pending.

## Prescription evidence local evidence (#1467)

- Observed red for G/H/H1 quotes without images (200 instead of 409).
- Absent/pending/rejected/scan-error evidence refuses; another prescription's
  clean image grants nothing. Actual owner processor release permits the same
  previously refused command, whose replay neither sells nor invoices twice.
- Direct SQL attempting a prescription sale without released evidence is refused.
- Prescription and H1 regression tests pass with `-race`; existing sale fixtures
  now upload and scan through owner APIs/processor rather than writing projections.
- The full pharmacy run found one custom H1 formula-export fixture without an
  image; corrected it through the same owner flow and rechecked the affected suite.
- Pharmacy lint and generated contract inventory checks pass. Worker dependency
  remains explicit; production enforcement is not activated yet.

## 2026-11-06 reconciliation

- Registration compatibility shipped in #1456; unused-UPI recovery shipped in
  #1465. The sale/payment/return and unused-refund browser journeys pass through
  real owners and the restricted evidence worker (#1496, follow-up #1581).
- Licence processing merged in #1528 and is present in the promoted production
  worker artifact. Licence entry/review UI and renewal advice remain draft #1581.
- Reviewed NPPA pricing shipped separately in #1529; do not duplicate it or infer
  a production policy activation from the implementation.
- This prescription gate is rebased onto current main and uses additive migration
  0036, preserving NPPA, service-charge, OPD and department-binding migrations.
  Focused prescription/evidence/register race tests and pharmacy lint pass.
- Production evidence workers were rechecked at two ready replicas. Designated
  synthetic tenant/outlet acceptance remains required before enabling this gate.
  No real patient, dispensing, payment or refund transaction is authorized here.
