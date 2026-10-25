# Clinical sharing notices implementation

1. Observe draft creation HTTP test fail before implementation.
2. Add immutable forced-RLS migration and scoped draft/read handlers.
3. Observe independent approval/retirement tests fail, then add transitions,
   strict replay and atomic metadata-only outbox publication.
4. Verify wrong tenant/hospital, revoked grant, invalid input, concurrent retries,
   audit/outbox rollback and SQL history guards.
5. Update registry, independent permissions/event gates and API/delivery docs.
6. Run real Postgres race tests, contract drift checks, build/vet/lint and aggregate
   regression gates. Do not infer production deployment from passing local tests.
