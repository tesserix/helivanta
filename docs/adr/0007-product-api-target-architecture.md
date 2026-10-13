# ADR-0007: One UI with independently deployable product APIs

**Date:** 2026-10-13

**Status:** Proposed implementation design; independent product API direction requested by the user.

**Supersedes:** ADR-0005's long-term runtime direction, upon adoption; ADR-0005 remains an accurate historical decision and the monolith remains the current implementation.

**Design:** [Full HMS architecture](../architecture/README.md).

## Context

The user explicitly wants one Helivanta UI with separate APIs for MediCore, PharmaConnect, LabConnect and other products, connected using Istio and proper authentication/authorization. The repository currently has a Go modular monolith with platform-owned patientmaster, enforced module boundaries and existing session/RLS/event foundations. Legacy discovery maps workflows to product owners and must not create duplicate masters or shared writable tables.

## Decision

Use product-sized deployables with owned PostgreSQL databases and independent credentials. Retain modules within each product and shared in-repository security/tenant/event packages. The shared UI uses a same-origin BFF and session service; product APIs verify audience-scoped delegation, current tenant/hospital policy and object permissions. Istio secures workload transport and allowed call paths. It does not replace clinical authorization.

Extract one product at a time after hospital-scoped isolation, audit, contracts, tracing and failure recovery are proven. NATS with JetStream carries durable local-outbox events; multi-step workflows use Temporal orchestration. Both technology choices are confirmed by the user. Keep a single writer for each aggregate throughout migration. The design is not an instruction to deploy every portfolio API simultaneously.

## Alternatives and consequences

Keeping the monolith costs less and remains a valid current baseline, but does not fulfill the requested target. Service-per-feature is rejected because it multiplies coordination without distinct ownership. Shared tables across APIs defeat release and access boundaries. The chosen design adds operational cost, dependency failure modes and eventual consistency, requiring explicit pending states, idempotency, policy availability, observability and tested recovery before extraction.

No runtime or infrastructure change is authorized by this ADR alone. A rollback after the new writer accepts changes requires reconciliation/reverse sync; flipping routing to stale data is not a rollback plan.
