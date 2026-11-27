# Professional registration runtime integration

Status: professional-source runtime deployed on 2026-11-26 UTC; all professional HTTP routes remain compiled unreleased. Refs #169 and ADR-0043. See the [deployment evidence](../deployment/releases/2026-11-27-professional-source-runtime.md).

## Ownership and access

`practitioner` remains the sole tenant-local owner of professional identity,
registration and qualification revisions, accepted steward appointments, reviewed
source policies, registration attestations/reconciliation and private evidence.
Workforce employment credentials and pharmacy role labels confer no registration
or clinical privilege. Existing `clinical_authority: not_evaluated` is unchanged.
No clinical policy publication or ORG-D5 decision is implemented here.

The API and migration command share `bootstrap.Modules`; the architecture tests
retain an independent module list. Global API maps `/v1/practitioners` to its
platform destination. All 68 registered routes map to compiled `unreleased`,
including self-service steward routes. Provider or tenant pilot flags cannot
expose them. Existing permissions, accepted-steward checks, tenant RLS, evidence
parent/revision scope and immutable command/recovery contracts remain required.
No client tenant header or owner role replaces these checks.

## Evidence processing

The API image also contains `/app/practitioner-evidence-worker`. It registers the
existing `practitioner-evidence-v1` Temporal task queue, workflow and activity;
generation-bound workflow identity, scan retry receipts, quarantine integrity,
ClamAV validation, promotion and audit remain owned by the existing processor.

`PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL` is mandatory and has no fallback to
API or other worker credentials. Startup requires exactly the existing
`helivanta_practitioner_evidence_worker` role, without inheritance, role
memberships, superuser, RLS bypass, database creation or role creation.
The worker receives only its own credential and private-pipeline configuration.
It uses the existing private quarantine/released buckets and scanner; its workload
identity may get/create only `practitioner` object-owner paths. It has no bucket
listing, deletion, patient/HR/supplier file access or public ingress.

The owning `tesserix-k8s` chart keeps processing disabled, database LOGIN off and
credential projection off by default. A dedicated OpenBao reader, scoped network
policies, resource limits, probes, PDB, alerts and runbook accompany activation.
No credential is committed or provisioned by source generation.

## Migration and release gates

Runtime registration made all `0001_practitioner` through `0016_practitioner`
migrations eligible for execution for the first time in the shared migrator;
the initial deployment has now recorded every one of them.
These are existing, forward-only migrations, not evidence of prior deployment.
They add the professional registry, evidence/processing/retry/qualification
history, source policy/verification/reconciliation and withdrawal storage.
Inspect the exact ordered migrations in the generated inventory and module
source before approving execution; table-level and role tests must pass in CI.

The approved initial rollout recorded all sixteen runtime migrations. Future migration changes require their own specific approval before merge.
Before activation, approve provisioning the dedicated credential in OpenBao,
its CNPG login, three object-scoped storage grants and two worker replicas.
Use the owning GitOps repositories and Kargo's signed API artifact; never patch
images or reuse another worker's database secret. An older binary is not a
schema rollback. Retain additive schema/history if rolling application code back.

Acceptance requires real Postgres/RLS and wrong-role tests, generation-bound
Temporal recovery tests, contract/release parity, focused local builds, hosted
CI and the exact deployed artifact. Clinical verification additionally needs
the complete decision/second-person/prescriber loop and atomic active-sale
gate tracked in #169; this integration does not satisfy those acceptance items.

## Local evidence

Bootstrap and routing assertions were observed failing before registration and
owner mapping, then passed. Dedicated-worker tests cover absent/blank own DSN,
other-worker fallback refusal, exact configured DSN and missing pipeline inputs.
Focused race tests, architecture/permission checks, contract generation, release
checks and local entrypoint builds run without starting an app or database.
Container-backed integration tests belong to hosted CI, not local Colima.

## Current delivery boundary

The API, Global API and shell run the signed `d2129d53` runtime with two ready
replicas each. Read-only PostgreSQL metadata confirms all sixteen migrations,
forced RLS on nineteen practitioner tables and the dedicated role without LOGIN,
inheritance, elevated attributes or memberships. The three private practitioner
object-path storage grants were applied through Atlantis before infrastructure
merge. No tenant or clinical access was granted by these checks.

The separate credential-initialization rollout preserves existing secrets with
create-only/CAS-0 writes and does not enable database LOGIN or processing.
The dedicated seed job completed successfully; its credential remains private.
Professional worker activation and live file acceptance remain pending.
Existing evidence workers report unready while the current upstream ClamAV
signatures exceed the 36-hour readiness threshold. Kargo promotion success is
not healthy file-processing acceptance; retain the 48-hour release prohibition.
Clinical-policy authority (ADR-0077), person/account linkage and clinical
privileges, decisions/second check/prescriber workflow and the active-sale gate
remain unfinished in #169.
