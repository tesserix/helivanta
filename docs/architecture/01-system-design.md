# System and product boundaries

[Architecture index](README.md) · Proposed target · 2026-10-13

## Requirements and sizing assumptions

The platform must preserve legacy hospital workflows while isolating customers and enabling independent product releases. A tenant may own multiple hospitals whose data is isolated, selectively shared, or shared under an approved group policy. A single sign-in and workspace should serve staff working across subscribed products and authorized hospitals. Clinical writes, financial postings and stock movements must remain attributable and recoverable. The system must support India-first deployment without embedding India-only assumptions in identity or data contracts; another jurisdiction gets an explicitly approved residency cell.

**Planning envelope, not measured load:** 50 tenants at month 12, 150 at month 36; 20 concurrently active staff per tenant; one API request per active staff member every five seconds. That is 200 RPS during normal busy use and a 5× burst budget of **1,000 RPS** at month 12, rising to **3,000 RPS** at month 36. Assume 80:20 reads:writes and typical JSON payloads of 2–20 KB. A 1,000-RPS, 10-KB response mix produces about 10 MB/s before overhead. Attachments bypass JSON APIs through controlled uploads; PACS image volume is a separate sizing exercise.

These are aggregate **per-tenant** staff/encounter assumptions across its hospitals, not per-hospital multipliers. For scope testing, use groups with 1–10 hospitals and a mixed-sharing group of at least three. Hospital counts, central contact-centre load and busiest-hospital skew need customer data; load tests must include one group consuming 30% of total traffic to verify fairness and isolation.

For storage, assume 500 encounters/tenant/day and 40 KB of structured records per encounter: 50 × 500 × 365 × 40 KB ≈ **365 GB/year** raw. Budget 3× for indexes, history and bloat: **1.1 TB** at month 12. With 50/100/150 tenants over years one/two/three, cumulative raw data is about 2.19 TB, or **6.6 TB** at the same multiplier. At 2 MB of documents per encounter, document storage is about **18.3 TB** in year one and **109.5 TB** cumulatively over that growth profile, excluding diagnostic images, backups and replication. Do not size audit or imaging from the relational estimate.

At 200 RPS for 12 hours/day and 10% of that for the remaining 12 hours, roughly 9.5 million requests/day are possible. If half produce a 600-byte audit entry, audit raw volume is about **2.9 GB/day**, before indexes and archive replicas. Measure actual audited-resource cardinality: bulk reads may cost more than one entry per request.

| Proposed objective            | Target and qualification                                                                                                             |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Interactive core availability | 99.95% monthly for authenticated clinical API success, including auth/policy/data dependencies; about 21.6 minutes at 30 days        |
| Common API read latency       | p99 ≤500 ms end-to-end at the gateway under the validated peak workload                                                              |
| Common command acceptance     | p99 ≤800 ms; longer work returns an operation reference and explicit pending state                                                   |
| UI navigation                 | p95 usable content ≤2 seconds on the agreed hospital network profile                                                                 |
| Routine event propagation     | p99 ≤5 seconds; queues show pending/stale state beyond that                                                                          |
| Critical-result handling      | Durable escalation with acknowledgement timer; clinical owner defines and validates the actual clinical SLA                          |
| Security-sensitive changes    | Suspension/revocation checked against current authority on every new authorized operation; no stale allow during an authority outage |

A sample read budget is 50 ms edge/BFF + 80 ms identity/policy + 250 ms API/DB + 120 ms network/serialization. These are allocation budgets, not a claim that component percentiles mathematically prove the end-to-end percentile. Measure complete traces and synthetic transactions.

## One workspace, multiple product APIs

![System overview](diagrams/01-system-overview.png)

This overview groups capability dependencies for readability. Shared platform boxes do not grant a platform API access to every product database: product APIs connect directly to their own stores with their own credentials, and call shared capabilities through authorized contracts.

The shell owns sign-in, tenant/hospital/facility selection, navigation, design system, accessibility, locale, notifications and permission-aware menus. Domain screens remain owned by product teams. Existing Next.js zones are a valid implementation of **one UI** provided they share origin, navigation, session handling and UI contracts. This proposal does not force a frontend rewrite or resolve every open zone-routing implementation issue.

Use a same-origin BFF for browser API requests. It owns the session boundary and delegates requests to separately deployable internal APIs over Istio. Browser-visible routes are stable product prefixes; internal DNS names and credentials never enter the frontend bundle. Initial route families below are target contracts, not declarations that these endpoints exist today.

| API / owner          | Route family               | Authoritative records and capabilities                                                                                                                                            |
| -------------------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Platform control API | `/api/v1/platform/*`       | Tenants, facilities, products, memberships, entitlement versions, onboarding and operational status; no general clinical reads                                                    |
| Patient identity API | `/api/v1/patients/*`       | Tenant-scoped patient identity, identifiers, duplicate review, merge/unmerge provenance, consent artefacts; usable by every subscribed clinical product                           |
| MediCore API         | `/api/v1/medicore/*`       | Registration workflow, encounters, admissions, wards/beds, clinical record, theatre, radiology workflow, diet, hospital billing/insurance, hospital finance and workforce modules |
| PharmaConnect API    | `/api/v1/pharmacy/*`       | Prescription fulfilment, pharmacy stock ledger, batch/expiry allocation, dispense, pharmacy sale/refund/return and pharmacy configuration                                         |
| LabConnect API       | `/api/v1/lab/*`            | Lab service orders, specimen chain of custody, analyser workflow, technical/clinical validation, result versions, critical alerts and dispatch                                    |
| SupplyConnect API    | `/api/v1/supply/*`         | Supplier catalogue, requisitions, PO, GRN, central-store stock ledger, transfers and procurement approvals                                                                        |
| DoctorConnect API    | `/api/v1/doctor/*`         | Clinician workspace preferences, task orchestration and care-team experience; commands canonical clinical records through MediCore, never a second encounter database             |
| MediConnect API      | `/api/v1/patient-portal/*` | Patient/delegate portal relationships, communication preferences, self-service requests and released-record views; staff sessions cannot become patient identities implicitly     |
| CRM API              | `/api/v1/crm/*`            | Hospital-scoped enquiries, contacts, referrals, outreach permissions, campaigns and communication history; delegates booking and clinical work to the owning products             |
| EmergencyConnect API | `/api/v1/emergency/*`      | Dispatch, ambulance/crew tasks and pre-arrival handoff; hospital casualty encounter stays in MediCore                                                                             |
| CareConnect API      | `/api/v1/care/*`           | Home-care plans, visits, monitored observations and escalation; clinical handoffs reference the shared patient identity                                                           |
| FacilityConnect API  | `/api/v1/facility/*`       | Asset registry, maintenance, custody and gate-pass workflows                                                                                                                      |
| SafetyConnect API    | `/api/v1/safety/*`         | Incidents, drills, inspections, corrective actions and restricted safety records                                                                                                  |
| AdminConnect API     | `/api/v1/admin/*`          | Administrative workspace, governed report catalogue and support case workflow; invokes platform commands, does not independently own tenancy or clinical ledgers                  |

These are product boundaries, not an instruction to deploy all products at once. Each ships as a complete vertical slice with its own authorization, audit and recovery tests. MediCore's finance and HR remain modules to preserve the exploration backlog's ownership. A later extraction requires its own decision and data-owner migration, not a menu rename.

Platform capabilities also include session/security-token service, policy evaluation, audit ingestion, document storage, notification delivery, integration workers and reporting projections. They can be grouped into a small number of deployables by common privilege and failure domain. In particular, identity provisioning, privileged system workers and clinical serving workloads must not share privileged credentials.

## Ownership rules

- One patient identity service, with hospital-scoped registrations and separately governed group identity links. A platform-level UUID does not mean shared visibility. Isolated hospitals cannot discover each other's patients; matching or linking across hospitals requires explicit policy. Matching across unrelated tenants requires a separate lawful exchange workflow.
- MediCore owns the clinical prescription and encounter; PharmaConnect owns fulfilment and dispensing. Amendments create new prescription versions. Dispensing never edits the original prescription.
- LabConnect owns laboratory result versions and sign-off. MediCore holds a released-result projection with source version and timestamp.
- PharmaConnect owns stock at pharmacy locations; SupplyConnect owns central-store stock. A transfer has two linked ledger postings and an in-transit state, never two services updating the same balance row.
- Hospital invoices remain in MediCore; pharmacy invoices remain in PharmaConnect. A consolidated patient statement is a projection of attributable source entries, not a second ledger. Internal charges specify settlement rules to prevent double billing.
- Product subscriptions gate access independently of professional roles. Enabling a product does not grant every staff member its permissions.
- Platform support cannot read PHI by virtue of operating tenants. Approved, expiring support grants and clinical emergency access are distinct mechanisms.

## API and integration contracts

Use OpenAPI for synchronous APIs and versioned event schemas for asynchronous contracts. Cursor pagination has a bounded maximum page size. Errors use `{code, message, request_id}` without clinical payloads or internal diagnostics. Use UTC `timestamptz` values and retain the facility timezone for display, cut-offs and business dates. Monetary amounts use exact decimal strings or integer minor units with currency; quantities and unit conversions require explicit precision.

Every retriable command has an idempotency key scoped to tenant, product and operation, with a request fingerprint. A repeated key with different content is rejected. Version/ETag checks reject stale updates; patient merges and signed clinical records never use last-write-wins. Webhooks verify authenticity, timestamp/replay window and destination mapping before reaching domain logic.

External boundaries include payment providers, insurer/TPA systems, FHIR/ABDM interfaces where applicable, HL7 analysers, DICOM/PACS, accounting exports and messaging providers. Dedicated adapters translate to internal contracts, keep per-tenant credentials in OpenBao, quarantine unmatched messages and retain reconciliation status. An IP address or device-supplied patient ID is not proof of tenant identity. Regional egress and provider contracts apply to every adapter.

## Alternatives and scope

Keeping the modular monolith would cost less operationally, but does not deliver the user's requested independent product APIs. The target therefore changes that decision while preserving module boundaries and using a controlled extraction. A service per hospital workflow or per table adds network transactions without a distinct data owner and is rejected. Shared writable tables between product APIs are rejected because they erase ownership and independent migration safety.

AI is a future capability; there is no autonomous diagnosis, prescribing, billing or stock action in this design. Billing logic, clinical scoring rules and statutory calculations need domain validation using synthetic cases; legacy screen presence does not establish correctness.
