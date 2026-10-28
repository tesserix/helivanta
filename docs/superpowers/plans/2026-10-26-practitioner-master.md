# Practitioner master implementation plan

Specification: [owner and sequencing](../specs/2026-10-26-practitioner-master.md).
This plan does not close #39/#474/#77/#102 for a partial implementation.

- [x] Observe strict base-record intake/read integration tests failing, implement
      immutable tenant-scoped records and attributable read/write audit, then verify
      concurrent replay, changed intent, permission denial and dependency failures.
- [x] Implement local unverified council-claim intake, immutable renewal receipts,
      bounded history, replay/concurrency, isolation, audit-failure and cancellation
      verification. This is not council verification or runtime integration.
- [x] Implement local accepted/time-bounded registry reviewer appointments, private
      governance history, replay/expiry/concurrency, membership and audit failures.
      These appointments are not council verification or clinical privileges.
- [ ] Complete registration/qualification/evidence and independent verification
      workflows; test lapsed/suspended sources and explicit HPR unavailability.
      Storage naming and immutable, audited revision-bound evidence metadata intake
      and byte upload/quarantine with an atomic scan outbox are implemented locally.
      The scoped, exact-generation scanner/promotion processor is implemented and
      tested locally, including bounded Temporal activity retries, private content
      release and retained revocation. Retained manual retry and attempt fencing recover exhausted executions locally.
      Independent revision-bound source-review receipts, released evidence snapshots
      and current accepted reviewer checks are implemented locally; effective
      verified identity and conflicting-claim reconciliation remain unfinished.
      Reported qualifications now retain immutable correction history with a
      tenant-local controlled specialty catalogue and stable head pagination.
      Local catalogue retirement/replacement now preserves history and blocks new
      retired references; qualification evidence/source review now reuses the same scoped scan/release/
      retry/revocation engine. Effective qualification verification remains open.
      The module consumer dispatches committed scope
      to a receipt-checked Temporal execution in local acceptance tests. Production
      delivery/worker wiring and effective professional verification remain
      required before runtime activation.
- [ ] Complete reviewed facility/role/privilege ownership, current and historical
      checks, expiry workflows and reconciliation; preserve prior signed attribution.
- [ ] Register the complete owner, publish reviewed contracts, regenerate inventory
      and run architecture/matrix tests, race/coverage, lint, build and client checks.
- [ ] Implement scheduling and booking against those live owner contracts, followed
      by consent-aware reminder cancellation and actual provider verification.
- [ ] Review/merge through CI, deploy through GitOps with specific approved
      migrations, then verify synthetic end-to-end cases and broad story criteria.

## Effective verification follow-up

Issuer configuration (explicit normalization, calendar, freshness/date rules and
versioned accepted authority) is implemented locally after foundation #1344. See
[verification design](../specs/2026-10-26-practitioner-verification.md). It leaves
professional facts unverified; effective decisions, canonical identifier conflict
handling and runtime/clinical authority integration remain required.
