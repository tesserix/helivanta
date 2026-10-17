# ADR0031: Verify platform identity before bilateral trading

Status: authority deployed; consumer API, runtime construction and UI implemented
locally; canonical onboarding and bilateral business integration pending.

Owners: PharmaConnect #1135 and #1136, parent #1133. Source prerequisite:
[onboarding-service #10](https://github.com/tesserix/onboarding-service/issues/10),
[PR #11](https://github.com/tesserix/onboarding-service/pull/11).
Canonical enrolment prerequisite:
[onboarding-service #12](https://github.com/tesserix/onboarding-service/issues/12).

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

This change implements the client, dedicated machine-token exchange and a
grant-checked `GET /v1/supply/parties/:party/platform-identity` observation for
owned organisations. The UI requests it explicitly and rejects a response for a
different workspace, party or profile version. It clears old confirmation while
refreshing, on error and on workspace changes. Every successful read appends an
audit event after access/version rechecks; no database lock spans source I/O.
An identity receipt always reports `trading_approved: false`. The relationship
transition and business-gate composition in the diagram remain subsequent work.

The product's source proof, private API receipt and browser schema are generated
from `contracts/organization-identity.json`. The source authority remains the
onboarding API; the generated consumer contract grants no authority of its own.

## Required business boundaries

| Boundary                  | Required behavior before activating trade                                                                                                                             | Owning story |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| Local legal organisation  | Explicit owned-organisation binding and independent onboarding review; private contacts/bank details remain private                                                   | #1135        |
| Both counterparties       | Active canonical identity observations and explicit consent to the same immutable terms revision                                                                      | #1136        |
| Premises and category     | Current evidence-backed licence verification and exact country/category/operation policy; changed or expired evidence invalidates eligibility                         | #1134, #1135 |
| Commercial agreement      | Explicit buyer/seller roles, country, premises, categories, service areas, currency, validity and customer-account terms; edits require both parties to approve again | #1136, #1138 |
| Organisation ownership    | Multiple pharmacies, hospital ownership and corporate grouping confer no clinical or commercial access by themselves                                                  | #1136        |
| Suspension and revocation | Block new quotes/orders; retain authorized historical obligations and attributable suspension/reinstatement history                                                   | #1136, #1139 |
| Cross-border transactions | Deny by default; require separately reviewed trade, tax, licence, logistics and data-transfer authority                                                               | #1134        |
| Order effects             | Recheck current identity and consent at the operation boundary; stable idempotency keys and source versions prevent replayed effects                                  | #1139        |

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

The source prerequisite passed real local PostgreSQL authorization tests and an
isolated live Zitadel fixture test. After the user approved publication, both
repositories are public and all upstream PR/main CI gates passed. The authority
is deployed at `6346f0fb58316f373845f7ac7eb444df08133416` through Kargo/Argo;
both API and console replicas are updated and healthy. Production unauthenticated
identity requests return 401. A positive product-machine production observation
is still pending canonical registration and credential delivery.

The runtime constructor accepts only a complete explicit configuration group:
`ORGANIZATION_IDENTITY_URL`, `ORGANIZATION_IDENTITY_PRODUCT_KEY`,
`ORGANIZATION_IDENTITY_TOKEN_PROJECT_ID` and `ORGANIZATION_IDENTITY_KEY_FILE`.
The token project is the audience accepted by onboarding authentication, not the
Helivanta project returned in the source proof. `ZITADEL_ISSUER_URL` binds the
machine assertion to the IdP. Credential files are bounded to 16 KiB; credentials
use HTTPS, a one-minute RSA assertion, bounded exchanges and expiring access
tokens. Token/key diagnostics are redacted by construction. No login credential
is reused and no default product key is inferred.

Read-only production configuration discovery confirmed that the authoritative
onboarding registry has no product registrations. Resolve this through the
authorised service/operator under #12, preserving existing tenant UUIDs. No
manual SQL registration/backfill or manufactured console session is permitted.
Missing configuration returns an actionable unavailable state after private
object authorization. Partial/invalid configuration fails at API startup.

No IAM expansion, migration, OpenBao writer or consumer production activation is
included here. Before activation, verify actual canonical registration and its
dedicated machine/audience, deliver its OpenBao/ESO projection, and exercise
both organisations locally and in production. Source failure denies new
eligibility-sensitive transitions while authorized historical reads remain
available. No legal, clinical-sharing or bilateral permission follows from
platform identity alone.
