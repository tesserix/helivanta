# Implementation plan — #1685

1. Add failing generated-input/consumer tests for nullable integer minimum,
   explicit versions, strict fields and exact quantities. Add owner integration
   assertions for the public routes and stock semantics; execute infrastructure
   tests in hosted CI, never local containers.
2. Generate canonical Go/Zod models from pharmacy.json. Add 0052 append-only,
   scope-protected configuration history and database transition guard. Implement
   current/receipt/save and keyset low-stock routes, then overview parity.
3. Build named UI configuration and low-stock list, exact replay recovery, overview
   link and existing release-gate mapping. Add component and production-shaped E2E.
4. Generate contract documentation and run generator, unit, type, lint, format,
   affected local Go/Next builds. Remove only generated build artifacts.
5. Commit/push/PR with actual red/green evidence, run hosted integration/E2E and
   migration checks, merge passing code. Obtain concrete migration/release approval
   where required; verify signed Kargo rollout, enabled prerequisites and browser
   behavior before closing the issue. Continue #1686 only after #1685 acceptance.

Status: owner API, configuration migration, generated contracts, SDK, UI and tests
implemented. Local input and consumer tests pass. Hosted database/E2E, full local
build checks, review and release acceptance are pending; #1685 remains open.
