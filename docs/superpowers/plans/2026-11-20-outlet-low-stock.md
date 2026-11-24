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

Status: the original implementation merged in #1761 and deployed on 2026-11-20.
It remained unavailable behind broad Catalogue/Stock release locks. Continue
with the [narrow activation plan](2026-11-24-low-stock-activation.md); its focused
owner, gateway, built-browser, capacity and deployed acceptance gates determine
#1685 completion. This plan's original test totals are historical, not evidence
that the new release candidate is accepted.
