# Proxy clinical consent implementation

1. Add a real guardian HTTP fixture; observe missing proxy capture fail.
2. Add explicit capacity-specific notices and conditional authority FKs while
   preserving self receipt bytes and every existing RLS predicate.
3. Extend private live-claim checks with exact optional relationship and use them
   in signed proxy capture/recipient decisions; reuse canonical withdrawal,
   quarantine, history and events.
4. Cover the actual source changes, failures, replay and unauthorized boundaries;
   regenerate contracts and run regression, race/coverage, build, vet and lint.
5. Review CI and migration/release evidence, then exercise isolated synthetic
   deployed journeys before claiming integration. Broad parent issues remain open.

## Current implementation checkpoint

- Missing proxy capture and catalog routes observed HTTP 404 before implementation.
- Signed capture, retained read and an exact-recipient gate were exercised using
  PostgreSQL, NATS, OpenBao and workload-authenticated owner HTTP. Parent and ward
  claims use explicit relationships; the omitted self request remains unchanged.
- Added source withdrawal/revocation, outages, integrity quarantine, concurrency,
  atomic audit/outbox rollback, raw patient denial and DOB-without-version tests.
- A retained self-v1 serializer fixture was exported from pre-proxy commit
  3bb59ed and byte equality passed against the new reader/serializer.
- New writes default disabled. The initially failing disabled-capture assertion
  now passes; replay reconciliation is checked before the write gate.
- Source metadata locks use independent GORM read sessions; an initial shared
  query builder carried a notice model into a recipient query and was corrected.
- Targeted rollout/parental/legacy tests passed. Full affected patientmaster race
  tests passed (108.320s), as did MediConnect (29.150s). The architecture pin test
  initially rejected the added relationship PHI field; its private-only purpose
  and the explicit pin were reviewed together, and architecture/contractdocs then
  passed. Build, vet and lint passed (zero issues). Generators/check passed.
- Typed concurrent index migrations are implemented locally. Real PostgreSQL
  tests cover cancellation/recovery, conflicting definitions, independent pools,
  partial progress, timeout restoration and transactional rollback. Tenantdb race
  tests passed (16.472s); the targeted 100k capacity-skew plan passed (31.569s).
- Current-source lint, build, vet and contract generation/check pass. An earlier
  whole-backend run compiled while imports were changing and failed. The fresh
  whole-backend race/coverage gate passed: patientmaster 79.9%, MediConnect 85.9%,
  tenantdb 82.2%. The later exporter-only change passed contractdocs race tests,
  tenantdb race (16.898s), targeted lint/vet/build and regenerated drift checks.
- Existing synthetic reviewer login passed Zitadel authorization, PKCE and Global
  API tenant session exchange; this verifies deployed authentication, not the
  undeployed proxy feature. Credentials remain in protected local QA files.
- Full patientmaster performance suite passed (103.666s), including the existing
  self/recipient catalogs and ledger plans plus the proxy capacity-skew case.
  All 22 workspace lint/type-check/test/build tasks passed under Node 24.
- Review caught an exporter gap: typed indexes were absent from the inventory.
  A new real inventory assertion failed, then passed after exporting exact
  table/key/direction metadata and rendering the indexes in the reference.
  Contractdocs race tests and generation/check passed.
- Deployment is not complete. CI, reader-before-writer release proof and synthetic
  deployed proxy QA remain. Real notice publication requires accountable review.
