# Licence review implementation plan

Issue #1135; spec: ../specs/2026-10-16-supply-licence-review.md.

1. Add failing public API tests for clean-evidence submission and independent
   verification against real PostgreSQL and the existing GCS protocol fixture.
2. Add generated input contract, append-only forced-RLS review migration and
   guarded API transitions/history/effective-state query. Observe red then green.
3. Add negative, concurrency, replay and SQL boundary tests; prove stale source
   revisions and revocation invalidate verification immediately.
4. Add an accessible API-backed review panel next to each declared licence with
   source evidence, independent-review form and paginated immutable decisions.
   Write failing UI tests before implementation; verify all state/error cases.
5. Exercise legacy forms read-only and local real API/UI with synthetic India and
   Pakistan records. Update architecture/legacy coverage and actual test evidence.
6. Run full required checks, commit/push a scoped PR referencing #1135/#1133, then
   seek the specific production migration approval only when the result is concrete.
7. Continue organisation lifecycle and bilateral relationships, offers and orders
   under their existing stories; preserve denied transaction activation until
   separately reviewed country/category policies allow it.
