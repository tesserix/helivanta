# Reviewed guardian authority implementation

1. Reproduce missing guardian authority review with a real owner HTTP test.
2. Add an additive owner migration, narrow RLS/state/immutability guards and
   metadata-only event; implement reviewed proposal, own acceptance/history and
   subject/registrar revocation without changing self-consent semantics.
3. Test authorization and failure/replay/concurrency boundaries through actual
   handlers, database and Global API. Ordinary patient reads stay unauthorized.
4. Update canonical contracts, permissions, event registry, examples and delivery
   evidence; run generators, full affected tests, vet/build/lint and CI.
5. Deploy through the existing release flow and verify with isolated synthetic
   actors. Keep #552/#60/#556 open; proceed to signed proxy consent, distinct
   adult delegation and owner-authorized family-profile composition.

## Validation checkpoint

- Existing clinical-consent allow assertion reproduced a migration-policy
  regression; preserving the prior recipient predicate restored behavior.
- Full patientmaster package passed after correction (65.581s), then passed
  independently with `go test -race -p=1 -count=1` (78.767s).
- Architecture suite passed after explicit singleton, permission-matrix and event
  privacy classifications. Contract generation/check, build, vet and lint passed
  (zero lint issues); secret scan found no leaks.
- Protected synthetic QA JSON credentials completed fresh Zitadel PKCE and Global
  API session exchange for the expected tenant. Sixteen live tenant-precondition
  checks passed without rejected-command domain audit side effects.
- Full backend `GOFLAGS='-p=2 -timeout=20m' ./scripts/coverage-gate.sh` passed
  (race-enabled, no failures): patientmaster 79.3%, MediConnect 85.9%, pharmacy
  88.1%, workforce 83.8%, IAM 80.6%. The new registry is undeployed; these checks
  do not establish complete proxy consent,
  record disclosure, booking/reminders or approved real notice publication.
