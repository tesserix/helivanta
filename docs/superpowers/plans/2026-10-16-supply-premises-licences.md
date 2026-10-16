# Private premises and licence declaration implementation

Spec: [2026-10-16-supply-premises-licences.md](../specs/2026-10-16-supply-premises-licences.md). Issues #1135/#1137 remain open until their wider acceptance is delivered.

1. Add generated contract inputs and client schemas, keeping the existing address JSON compatible. Prove new routes fail before implementation.
2. Add tenant/party/country-constrained head and immutable revision tables in `0002_supply`, forced RLS, guarded versions, read-only auditor boundaries and atomic audit/outbox events.
3. Implement strict bounded handlers and optimistic exact/concurrent retries. Reproduce missing-history and duplicate-message regressions before fixing.
4. Integrate premise/licence cards and shared forms in the private party view. Exercise focus, errors, retained IDs, country time zones, paginated premise selection and histories.
5. Run real PostgreSQL/NATS concurrency/isolation/failure tests, full affected tests, architecture tests, web suite/lint/typechecks/build, Go lint/vet/build/race and coverage gates. Run local browser acceptance with synthetic data and check the real relay.
6. Submit a scoped PR with migration/recovery and test evidence. Do not close broad stories or claim production rollout. Then implement the independent private evidence/review slice from ADR 0028; no unused organisation attachment API ships in this registry slice.
