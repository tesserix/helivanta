# Full Helivanta HMS platform design

**Date:** 2026-10-13

**Status:** Proposed detailed design; product API direction requested by the user. No runtime changes.

**Evidence:** [Exploration #979](https://github.com/tesserix/helivanta/issues/979).

**Design home:** [Architecture package](../../architecture/README.md).

The requested target is one UI backed by separate product APIs using Istio, Zitadel, PostgreSQL, Valkey and future Qdrant. One tenant can own multiple hospitals. Hospitals may remain isolated, share selected records, or participate in approved group sharing. CRM is included for enquiries, referrals, appointments, outreach and communication history; it is not a medical-record-number feature.

## Decisions

- Tenant is the customer boundary; hospital is an additional mandatory data-access boundary. Shared tenant ownership, common identity and product access never imply shared clinical or CRM access.
- Clinical sharing, identity linking, CRM sharing, financial reporting and workforce visibility are independently governed, directional and revocable policies.
- Product APIs own their databases and transactions. Shared patient identity remains a platform capability with hospital registrations and reviewed links.
- Zitadel provisions tenant organizations and authentication policy. Helivanta owns hospital membership, entitlements and sharing policy; OpenFGA evaluates relationships.
- PostgreSQL provides authoritative state and tenant/hospital RLS, Valkey disposable caching, NATS JetStream outbox-backed events, and Temporal durable multi-step workflows; NATS/JetStream and Temporal are confirmed choices.
- GCS stores tenant/hospital-scoped files; ClamAV scans exact quarantined object generations before authorized release. Both technologies are confirmed by the user.
- Future Qdrant retrieval is private and source-authorized before prompt construction; assistive AI never blocks core care.
- HIPAA and SOC 2 controls include operating evidence, risk management, contracts, retention and recovery, not merely encryption or installed components.

## Companion decisions

[ADR-0007](../../adr/0007-product-api-target-architecture.md) replaces the long-term monolith direction while preserving it as the current implementation baseline. [ADR-0008](../../adr/0008-multi-hospital-tenancy-and-sharing.md) proposes the tenant/hospital/sharing and Zitadel onboarding model, replacing the older one-hospital-per-tenant/no-tenant-org target assumptions. Neither ADR claims migration is complete.

## Reviewable deliverables

Seven detailed design chapters, an index, eight native editable draw.io pages and matching SVG and PNG previews. Capacity/SLO/cost assumptions, failure behavior, migration/rollback constraints, business questions and acceptance gates are documented in the package. Implementation planning follows review of these choices; no application code, live infrastructure, migrations, commits or PRs are part of this design change.
