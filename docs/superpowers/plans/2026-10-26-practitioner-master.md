# Practitioner master implementation plan

Specification: [owner and sequencing](../specs/2026-10-26-practitioner-master.md).
This plan does not close #39/#474/#77/#102 for a partial implementation.

- [ ] Observe strict base-record intake/read integration tests failing, implement
      immutable tenant-scoped records and attributable read/write audit, then verify
      concurrent replay, changed intent, permission denial and dependency failures.
- [ ] Complete registration/qualification/evidence and independent verification
      workflows; test lapsed/suspended sources and explicit HPR unavailability.
- [ ] Complete reviewed facility/role/privilege ownership, current and historical
      checks, expiry workflows and reconciliation; preserve prior signed attribution.
- [ ] Register the complete owner, publish reviewed contracts, regenerate inventory
      and run architecture/matrix tests, race/coverage, lint, build and client checks.
- [ ] Implement scheduling and booking against those live owner contracts, followed
      by consent-aware reminder cancellation and actual provider verification.
- [ ] Review/merge through CI, deploy through GitOps with specific approved
      migrations, then verify synthetic end-to-end cases and broad story criteria.
