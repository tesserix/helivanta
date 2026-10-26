# Medical records implementation sequence

Implements the full scope of #72 and the owning backend dependencies of #73.
See the [design](../specs/2026-10-26-medical-records.md); no implementation is
claimed by this plan. Worktree starts from main `fa31004`, independently of the
practitioner verification draft, whose CI must finish without further source churn.

1. Verify actor permissions, patient reference and current hospital/encounter
   boundaries with patientmaster and shared IAM. Specify typed owner requests,
   state/version/revocation semantics and audit without copying patient data or
   creating another grant ledger. First integration test must fail on a missing
   boundary and then pass for real owner decisions, including revoked scope.
2. Define canonical document type/policy/command models, bounded validation,
   UUID/version/null/date semantics, response errors and schema generators.
   Implement versioned immutable configuration and qualified publication gates.
   Configuration access never grants patient-content access.
3. Implement module-owned tenant/hospital/patient catalogue and immutable upload
   intent. Exercise same-ID replay, changed intent, different patient/encounter
   context, invalid type/policy, stale configuration and current-grant outage.
4. Wire real private storage/quarantine, generation/hash/size/media verification,
   scanner worker and durable outbox/Temporal receipt recovery for each supported
   format. Prove malformed files, wrong generation, malicious content and outages
   cannot be finalized/released. Add PDF/text/DICOM-reference support explicitly;
   image-only fixture success cannot stand in for these formats.
5. Implement finalization, immutable supersession, scoped search and optional
   regional OCR with failure/retry/replay evidence. Capture attributable sensitive
   reads, downloads and decision history without clinical contents in events.
6. Integrate current owner release and recipient gates, authenticated session-bound
   content access, post-I/O reauthorization and retained withdrawal. Test exact
   original bytes through Global API and wrong session/tenant/hospital/patient,
   revoked consent/authority, unreleased reports and dependency failures.
7. Implement reviewed retention/holds and durable disposition, including expiry,
   failed compensation/retry, operator reconciliation and retained audit evidence.
   Verify consequential rules against current primary sources and actual tenant
   service/state before approving policy; never seed a universal legal duration.
8. Validate canonical/FHIR consumers, contractdocs/schema generators, appropriate
   race/integration/architecture suites, media fuzz tests and realistic query plans
   and object throughput. Finish runtime/scoped IAM wiring and owning GitOps
   manifests only after these controls work; preserve public repository visibility.
9. Merge using authorized admin bypass only after exact-head validation, deploy
   the named matching migration/API/worker configuration through GitOps, verify
   rollout/digests and complete synthetic signed-in helivanta.app journeys. Record
   all remaining gates and leave parent stories open until their full criteria pass.

The initial development slice is reference/configuration correctness. It does not
substitute for complete uploads, disclosure, retention, deployment or patient app
acceptance. The project goal remains all MediConnect stories working end to end.
