# Clinical consent authority implementation — #60 / #552 / #556

1. Record the separate owner, reviewed self-capacity attestation, patient acceptance,
   current source limits and transition rules in the paired specification.
2. Observe a missing-route failure, then implement the reviewed proposal with
   forced RLS, immutable lineage, bounded validation and atomic audit/outbox.
3. Observe the missing receipt/lifecycle route, then implement subject reads,
   bounded listing, explicit acceptance and attributable terminal revocation.
4. Verify tenant/subject/hospital isolation, current grants, stale identity,
   invalid/expired input, duplicate/concurrent commands, audit rollback, structural
   SQL guards and real HTTP forwarding through Global API.
5. Update independent permission/event/pagination gates and owning API contracts;
   regenerate and check runtime references. Run race/coverage, build, vet and lint.
6. Rehearse additive migration/old-migrator compatibility against an isolated
   schema and inspect the subject-page query plan at representative volume.
7. Review/merge with exact CI evidence. Prepare a named production rollout only
   after the release gates are satisfied; retain all history and source checks.
8. Continue guardian/adult delegation, purpose-specific clinical consent, signed
   release, scheduling/payment and provider-confirmed reminders. Full MediConnect
   remains open until those journeys meet their own acceptance criteria.
