# ADR-0008: Multi-hospital tenants with explicit sharing and Zitadel onboarding

**Date:** 2026-10-13

**Status:** Proposed detailed design for the user's requested multi-hospital/CRM model.

**Design:** [Tenancy and identity](../architecture/02-tenancy-and-identity.md).

## Context

The earlier topology describes a tenant as a hospital and keeps hospitals outside Zitadel. The user now requires each tenant to own multiple hospitals, with optional or selective data sharing, and Zitadel-backed tenant onboarding. CRM must follow these boundaries without implicitly exposing medical records.

## Decision

Model tenant, hospital and facility separately. A tenant is the customer/group; each hospital is a mandatory clinical/CRM data-access boundary. Isolated access is the default. Directional, versioned agreements can enable selected or group-wide eligible data, bounded by action, resource category, patient/cohort, purpose, lawful basis, sensitivity and validity. Identity linking and clinical/CRM sharing are separate capabilities. Common ownership is never sufficient authorization.

Use a Zitadel organization per tenant by default, with explicit optional mappings for hospital-specific identity policies. Keep canonical tenant/hospital IDs, membership, entitlements and sharing policy in Helivanta PostgreSQL; OpenFGA is the relationship decision engine. Prove external-clinician authentication assurance rather than assuming target-org scopes work for guest identities. Onboarding is durable and activates access only after every required gate passes.

Enforce tenant AND hospital/grant scope in PostgreSQL RLS, service policy, cache keys, event consumers, files, reporting and future Qdrant. Privileged tenant administrators do not receive clinical access automatically. Patient registrations and MRNs stay hospital-specific, with reviewed identity links and no cross-hospital discovery in isolated mode.

## Alternatives and consequences

One tenant per hospital cannot represent the requested group without an artificial cross-tenant layer. Tenant-wide sharing by default leaks sibling-hospital data. One schema/database per hospital can provide stronger infrastructure separation but multiplies deployment, migration and recovery costs; choose dedicated placement where contracted, while preserving identical policy semantics. Zitadel-only roles cannot express clinical relationships and record-level sharing.

The chosen model requires additional hospital ownership columns, compound constraints, policy/grant projections, revocation epochs, controlled identity matching and group-specific negative tests. Existing records need verified hospital assignment before multi-hospital activation. Withdrawing sharing stops future access; it cannot revoke information already lawfully disclosed. Historical clinical retention and source provenance survive policy changes.
