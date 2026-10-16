# ADR0031: Verify platform identity before bilateral trading

Status: proposed; identity client locally tested; business integration pending.

Owners: PharmaConnect #1135 and #1136, parent #1133. Source prerequisite:
[onboarding-service #10](https://github.com/tesserix/onboarding-service/issues/10),
[PR #11](https://github.com/tesserix/onboarding-service/pull/11).

Editable diagram: [live-platform-identity.drawio](../architecture/live-platform-identity.drawio).

## Context

A Supply party is a tenant-private legal/commercial declaration. Its local UUID,
an entered partner tenant ID, a hospital ownership link, or a cached platform
profile does not prove that another organisation currently exists, belongs to
that tenant, or retains the product entitlement. Existing `GET /v1/orgs/:id`
returns a projection rather than live source verification.

Onboarding-service owns the canonical tenant/product registry and the privileged
Zitadel integration. Helivanta must consume its minimized product-scoped identity
contract instead of reading its database or receiving its administrative token.

## Decision

Before an eligibility-sensitive transition, use a dedicated product machine to
call `GET /v1/orgs/{tenant-uuid}/identity`. Resolve the production product key and
audience from platform registration; the test fixture's `hms` key is not a
production default. The source service establishes the machine/product binding
and live organisation, tenant metadata, project, and exact grant binding.

Helivanta requires schema 1, exact requested tenant and configured product, safe
opaque source IDs, four lossless positive revision strings, a current observation
and active state. A 404 or explicit inactive state denies identity eligibility.
Authentication errors, timeouts, malformed or stale responses, unknown states,
redirects and other source errors are unavailable. Neither result permits a
fallback to a local profile or last-known eligibility.

The client has a two-second total deadline, an 8 KiB response limit, no redirects,
no browser cookie jar, no automatic retry and no identity eligibility cache.
Credentials and provider bodies never enter its errors. An OpenBao-backed
credential belongs only to the dedicated product machine; the existing login
client and onboarding administrator credentials must not be reused.

```mermaid
sequenceDiagram
    actor Admin as Scoped organisation administrator
    participant Pharma as PharmaConnect API
    participant Registry as Onboarding API
    participant Source as Zitadel
    participant DB as Tenant PostgreSQL
    Admin->>Pharma: Request a governed relationship transition
    Pharma->>DB: Authorize local organisation and expected revision
    DB-->>Pharma: Close read transaction
    Pharma->>Registry: Machine-authenticated exact tenant identity request
    Registry->>Registry: Check machine product and current entitlement
    par Live source facts within one deadline
        Registry->>Source: Organisation and tenant metadata binding
    and
        Registry->>Source: Product project and exact organisation grant
    end
    Registry->>Registry: Recheck registry entitlement
    Registry-->>Pharma: Minimal active/inactive observation or safe error
    alt Active identity and all remaining business gates pass
        Pharma->>DB: Recheck local revisions and authorization; append command, audit and outbox atomically
    else Identity or business gate cannot be established
        Pharma-->>Admin: Denied or temporarily unavailable; no relationship activation
    end
```

Only the identity client is implemented in this change. The relationship
transition, database writes, business-gate composition and UI in the diagram are
subsequent work under the existing stories, not released behavior.

## Required business boundaries

| Boundary | Required behavior before activating trade | Owning story |
| --- | --- | --- |
| Local legal organisation | Explicit owned-organisation binding and independent onboarding review; private contacts/bank details remain private | #1135 |
| Both counterparties | Active canonical identity observations and explicit consent to the same immutable terms revision | #1136 |
| Premises and category | Current evidence-backed licence verification and exact country/category/operation policy; changed or expired evidence invalidates eligibility | #1134, #1135 |
| Commercial agreement | Explicit buyer/seller roles, country, premises, categories, service areas, currency, validity and customer-account terms; edits require both parties to approve again | #1136, #1138 |
| Organisation ownership | Multiple pharmacies, hospital ownership and corporate grouping confer no clinical or commercial access by themselves | #1136 |
| Suspension and revocation | Block new quotes/orders; retain authorized historical obligations and attributable suspension/reinstatement history | #1136, #1139 |
| Cross-border transactions | Deny by default; require separately reviewed trade, tax, licence, logistics and data-transfer authority | #1134 |
| Order effects | Recheck current identity and consent at the operation boundary; stable idempotency keys and source versions prevent replayed effects | #1139 |

Network consent must never turn a private counterparty record into a verified
organisation simply because a user entered its identifier. Partner invitations
and acknowledgements need authenticated counterparties, minimized explicit
disclosure, and source-backed identity verification on each side.

## Persistence, messaging and recovery constraints

Relationship history belongs in SupplyConnect; do not duplicate the platform
organisation master or another module's licence/finance authority. Tenant-owned
records retain forced RLS and composite tenant references. Cross-tenant
acknowledgements need declared, allowlisted contracts and destination-owned
projections with source tenant/record provenance. Never broaden the application
role or let a browser choose the event destination independently of an authorized
relationship command.

PostgreSQL remains authoritative. Write business commands, audit and outbox in one
transaction; publish through the existing NATS JetStream outbox. Temporal owns
durable consent deadlines and reconciliation/retry waits. A successful broker
publish alone does not prove remote consent. Version checks must reject delayed
acknowledgements after amendment, suspension, expiry or revocation. Valkey may
limit lookup requests but cannot authorize from cached eligibility.

No transaction or database lock may be held across identity-service I/O. Repeat
local authorization and expected-version checks in the final write transaction.
The source observation is a bounded point-in-time fact, not an atomic lock across
services. A revocation racing after observation is handled by operation-boundary
checks and reconciliation; do not claim distributed serializability.

## Verification and rollout

The client tests use real TLS HTTP fixtures for machine-only headers, cookie
isolation, uncached reads, wrong-tenant/product proofs, stale/future timestamps,
malformed and oversized bodies, revision precision, suspension, cancellation,
bounded unresponsive sources and redirect credential protection.

The source prerequisite additionally passed real local PostgreSQL authorization
tests and an isolated live Zitadel fixture test. Its hosted CI is currently
blocked by GitHub account billing; it remains private pending the user's
visibility/billing decision. Helivanta remains public.

No product runtime configuration, IAM expansion, migration or new user route is
included here. Before consumer activation: release the source endpoint through
its required checks, verify the actual registered product machine/audience and
OpenBao projection, exercise both organisations locally, and obtain named
production approval for the resulting GitOps rollout. If source verification is
unavailable after rollout, deny new eligibility-sensitive transitions with an
actionable retry state and preserve already-authorized historical reads.
