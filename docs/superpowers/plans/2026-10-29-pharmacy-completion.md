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
