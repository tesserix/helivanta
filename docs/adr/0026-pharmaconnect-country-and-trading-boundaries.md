# ADR 0026: India country and trading boundaries

> **2026-10-23, #1287:** Pakistan was removed; India is the only supported country.

Date: 2026-10-16. Status: accepted design direction; staged implementation. Epic #1133.

## Context and scale

The user confirmed Release 1 is a procurement network: each seller remains contracting party and invoice owner; external invoices/payments are recorded and reconciled first. Marketplace operation and provider processing are optional later capabilities with their own activation gates. Pharmacies can remain independent or optionally hospital-linked. Distributors, manufacturers and importers are first-class business organisations, not fictitious hospitals or user roles inside an unrelated pharmacy tenant.

Planning assumptions, not measured limits: 100 organisations / 300 operating locations, 100 peak API requests/s, 4:1 reads:writes, 32 KiB synchronous bodies and 100 order lines; larger imports use staged file processing. At 1,000 orders/day and 20 lines/order, year one has 365,000 orders and 7.3m lines. At 2x annual growth, three years accumulate 2.555m orders / 51.1m lines. Budget roughly 100–200 GiB including indexed records and audit/event history before retention tuning. Proposed SLOs: 99.9% availability, p99 eligible search 500ms, stock mutation 500ms and order submission 700ms; durable acknowledgement can be pending. Proposed RPO ≤5min and RTO ≤60min require restore/failover evidence. Initial capacity/backup/worker budget envelope USD 300–1,000/month is a planning allowance, not a cloud quotation; residency, traffic and shared infrastructure allocation must be priced before rollout.

## Decision

Preserve ADR 0007's target product APIs and the current modular runtime until extraction gates pass. Platform owns canonical tenant/org identity, country profiles, identity/roles, policy approval and document services. Pharma owns pharmacy/trading state, distributor listings/fulfilment and pharmacy operational records. Reuse SupplyConnect supplier/procurement masters and Finance posting/tax/close contracts; never introduce shared writable tables or a second patient/drug/supplier/GL master.

Access scope is verified tenant -> explicit organisation -> legal registration / country -> operating location. A tenant can administer several owned pharmacies or hospitals, but a separate trading party is never readable because it shares a group, owner, hospital affiliation, territory or country. Manufacturer/importer -> super stockist -> regional distributor -> wholesaler -> pharmacy relationships are separately approved bilateral links. Disclosures use minimized destination-owned projections, declared directed event contracts and origin/source provenance, not RLS widening. Historical contractual obligations survive relationship revocation; new commitments recheck current eligibility.

Maintain independent country/jurisdiction/premises/category/operation/effective-version policy. The first country implementation exposes shared, generated India metadata and validates new outlet country/timezone pairs. Canonical source is `contracts/country-profiles.json`; generated Go/TypeScript output must agree. India defaults INR / Asia/Kolkata. This metadata has `review_required` regulatory status. It does not implement tax rules, approve a licence, activate trading, assert data residency or decide controlled-medicine eligibility. Existing statutory sources and reviewers are recorded by the subsequent policy/licence stories; unknown rules deny restricted transitions.

Price/quote/order/invoice snapshots retain the approved profile/policy version, source identities, units, currency and contracting/invoice/payment responsibility. Switching an optional commercial model must not change an accepted order. No cross-country currency netting or automatic cross-border trade. Patient-level information is absent from B2B purchasing unless a separately approved workflow needs it.

PostgreSQL owns quantities, approvals, terms, credit and financial state. Every business write/audit/outbox is atomic. NATS JetStream delivers at least once; inbox/source keys deduplicate. Temporal owns human approval waits and multi-party reservation/fulfilment/credit/recall/adapter sagas, with bounded activities, deterministic replay, versioning, semantic compensation and alerted stuck states. Single-DB writes do not need Temporal. Valkey is optional for versioned eligible-search caches and bounded limits, never authoritative stock, balances or stale eligibility. GCS object prefixes include tenant + domain/entity + record/date; reuse authenticated document authorization, quarantine, content validation and ClamAV before authorized download. Qdrant/AI remains advisory future work.

## Trust and failure behavior

Assets: negotiated prices, trade/credit balances, licences, commercial contracts, evidence and any separately authorised patient data. Threats: another tenant, an overprivileged employee, a compromised provider or an unauthenticated caller. Validate authentication, entitlement, object scope, role, transition, country policy and current relationship at each boundary. Auditors are scoped read-only; platform support needs reviewed purpose-bound delegation rather than implicit tenant access.

When PostgreSQL or current authorization/policy is unavailable, commitments fail closed. When NATS is unavailable, committed outbox intent remains pending and observable. Temporal/provider outages keep explicit pending/stuck state and retry idempotently; no false payment/dispatch success. A Valkey cache outage falls back to Postgres; limiter behavior must follow its security classification. GCS/ClamAV unavailability leaves evidence quarantined and any dependent approval blocked. Stale seller stock or quotes require refresh/reapproval; do not accept a cached eligibility decision after revocation. Notifications fail independently of committed business state and retain retries/dead letters.

## Migration, recovery and alternatives

All new organisation/country/trade records require additive, forward-only migrations and explicit tenant/location composite references. No destructive rewrite of existing pharmacy outlet ownership or historical financial records. Local metadata/configuration changes require no production schema action. Production migration/rollout uses the owning GitOps repository, a named approved target, backups and tested forward recovery; reverting application code retains immutable records. A country release is not legal sign-off or a claim of SOC 2/HIPAA certification.

Reject one global policy with per-country flags: it cannot preserve independent approval/version history. Reject a new service per actor: it creates coordination without a new data owner. Reject direct access to another party's inventory/finance tables: a trading relationship is not tenancy delegation. The modular runtime plus explicit contracts is the simplest initial implementation at these assumptions.

Open activation gates: approved legal/licence/product/tax/retention/residency sources in each jurisdiction; provider/vendor credentials and exact first-release ERP/POS/courier/finance adapters; optional marketplace/payment-provider responsibilities; measured capacity and recovery tests. These do not block metadata, schemas, synthetic acceptance or safe local development.
