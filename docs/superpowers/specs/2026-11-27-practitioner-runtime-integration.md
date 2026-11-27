# Professional registry runtime integration — #169

The [full verification design](../../design/pharmacist-verification.md) retains the clinical goal. This implementation connects the existing professional source owner, not clinical privileges or medicine approval. The existing registration attestation continues to report `clinical_authority: not_evaluated`. The separate proposed [ADR-0077](https://github.com/tesserix/helivanta/pull/1896) remains undecided; this work does not implement clinical policy publication.

## Verified gaps

- `backend/internal/bootstrap/modules.go` omits `practitioner.New()`, so the API, migration command and generated runtime inventory omit the owner despite its package tests and sixteen migrations.
- `practitioner/module.go` already publishes registration/evidence events and registers `EvidenceScanConsumer`.
- `practitioner/evidence_workflow.go` defines the generation-bound `practitioner-evidence-v1` task queue and activities; no running command registers them.
- The owning processor requires a dedicated `helivanta_practitioner_evidence_worker` identity. Test infrastructure already defines that fixture role. Another module's worker credential must never substitute for it.
- Global API ownership, release-capability coverage, runtime architecture/permission matrices, image contents and deployment configuration must agree with registration of the owner. A module import alone is insufficient.

## Boundaries and implementation

The practitioner owner retains professional records, registration revisions, policies, independent source attestations/reconciliations and private evidence. HR credentials, account links and organizational ownership do not become clinical authority. Consumers must use published contracts and owner guards rather than query these tables.

Register the existing owner in the single shared bootstrap list and its independent architecture assertions. Map its explicit API prefix through Global API's existing platform destination. Mark every source route compiled `unreleased`; do not expose it through an existing pharmacy pilot or provider override. Keep current route permissions and independent steward/source checks intact; do not invent clinical permissions or operating grants in the integration.

Run the existing evidence workflow through a dedicated worker command using its own database identity, private GCS storage, the existing ClamAV scanner and Temporal. Preserve generation-bound input, workflow IDs, retry/heartbeat limits, quarantine on error, schema/role integrity checks and scrubbed diagnostics. Use the established worker deployment pattern, explicit resource/probe settings and existing GitOps/Kargo delivery. Do not start local services or containers to validate it.

The app-facing source remains unavailable until its complete registration/account-link/access UX and acceptance are ready. Connecting this owner does not complete #169's queue, decisions, second checks, clinical context/query loop or atomic supply gate.

## Acceptance and delivery

Observe a failing bootstrap assertion before implementation. Verify the real runtime registry, Global API ownership, compiled release denial, canonical generated route/event/schema inventory and approved administrative permission matrix. Verify missing/wrong worker credentials fail closed; test actual processor role/RLS/generation/quarantine behavior in hosted CI. Build affected Go entrypoints locally without running them and remove only generated binaries afterward.

Before live delivery, present the exact newly executed source migrations, worker workload and any credential/configuration changes for the specific approval required by AGENTS.md. This specification authorizes no new live migration, credential provisioning or clinical activation. Passing pure wiring tests is not live professional-source or clinical acceptance.
