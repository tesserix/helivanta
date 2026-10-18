# ADR 0032: Immutable pharmacy batch safety concerns

Status: Accepted for implementation; production migration and rollout require named approval.

## Context

Stories #164, #166 and #167 require safety, recall and expiry governance. Current pharmacy products and outlets are pending review; every stock location is quarantine-only and dispensable stock is always zero. The licensing and bilateral trading foundations are not a positive release authority. Clinical dispensing currently has no batch allocation. An unreviewed concern must not imply a verified recall, release decision, stock decrement, or a full patient-level trace.

## Decision

Add the outlet-private `pharmacy_batch_safety_incidents` journal through additive migration `0007_pharmacy`. Authorised staff record quality concerns, packaging damage, temperature excursions or suspected recalls against an existing immutable batch, optionally an owned location. A location identifies where a concern was observed; it does not narrow the concern to that location or declare other stock safe. History is ordered by database recording time, not the staff-supplied occurrence time.

```mermaid
sequenceDiagram
    actor Staff
    participant UI as PharmaConnect stock UI
    participant API as Pharmacy API
    participant Auth as Permission authority
    participant DB as PostgreSQL / FORCE RLS
    participant Outbox as Transactional outbox
    participant NATS as NATS JetStream
    Staff->>UI: Select exact product/lot and record concern
    UI->>API: PUT stable UUIDv7 + bounded incident
    API->>Auth: Check stock.quarantine
    Auth-->>API: Allow / deny / unavailable
    API->>DB: Begin tenant + actor + pharmacy + outlet transaction
    DB->>DB: Recheck live editor grant and lock incident identity
    alt Exact replay by same actor
        DB-->>API: Original immutable incident
    else New incident
        DB->>DB: Owned batch/location + database clock checks
        DB->>DB: Stamp recorded time; append incident + audit
        DB->>Outbox: Append identifier-only BatchSafetyRecorded
        DB-->>API: Commit all or roll back all
    end
    API-->>UI: Immutable incident (no-store)
    Outbox->>NATS: Existing retrying outbox worker publishes
```

`pharmacy.stock.quarantine` authorises recording a concern only. Pharmacy owner, pharmacist and inventory roles may receive it; a current explicit editor outlet grant is independently required. Readers need `pharmacy.stock.read` and a live outlet grant. Tenant, pharmacy, outlet and batch remain explicit; corporate or hospital ownership never grants data access. Existing transaction helpers and FORCE RLS revalidate live grants. Composite foreign keys prohibit cross-outlet batch/location references. Incident actor must match both pharmacy and audit session actors. UPDATE, DELETE and TRUNCATE are blocked by immutable triggers. Database owners remain trusted administrative principals; these controls do not provide external tamper-proof storage.

PUT identities are tenant/outlet-scoped UUIDv7 values. A transaction-scoped advisory lock serialises concurrent same-ID requests. Normalised details and authenticated actor must match for replay; changed input conflicts with 409. No natural-key deduplication is imposed on distinct safety observations. API occurrence timestamps use RFC3339 with an explicit offset, year 1 or later after UTC conversion, and at most six fractional digits. The original text is validated before Go parsing can truncate precision. Future occurrence times fail against PostgreSQL's clock. A BEFORE INSERT trigger stamps recording time regardless of supplied database values; finite occurrence time must precede it.

Temperature is an integer number of thousandths of a degree Celsius, present only for a temperature excursion. Bounds are representation limits (-273.150 to 1000.000°C), not approved storage thresholds or an automatic determination of product safety. No unverified temperature feed, extraction, arbitrary evidence URL or customer/patient identifier is collected. The narrative may contain sensitive information, so event payloads carry IDs only; access to audit snapshots follows existing restricted audit policy.

Reads require an owned batch and use `(created_at,id)` keyset pagination with the existing page envelope. They are live recorded history, not a frozen cross-request snapshot. The UI uses shared generated validation, permission-bound actions, exact batch labels, explicit customer-zone times with UTC offsets and a stable UUID/payload for ambiguous retries. Definite validation failures may be corrected with the same identity. Scope switches unmount prior rows and form state. Clients must retain request identity when retrying; changing identity creates a new observation.

No new cache is introduced for clinical stock, safety history or access checks. Valkey's existing session role is unchanged. NATS uses the existing transactional outbox and JetStream delivery infrastructure; there is no new consumer projection. No Temporal saga is needed for a single database transaction. Future reminders, review escalation and recall orchestration need their own reviewed workflow contract. No attachments are added; future evidence must use scoped GCS/ClamAV document handling.

## Consequences and delivery

This implements the bounded journal scope of story #1213; release acceptance remains pending, not the lifecycle/recall/expiry parents. Positive release, independent review/resolution, propagation into future reservation/FEFO/dispense gates, supplier/GRN/cost links, patient trace, verified recall action, expiry policy, stocktake and disposal remain separate work. Existing stock remains quarantined and quantities unchanged. The journal is a durable concern record, not a new enforceable batch-state machine.

Migration creates a new empty table, indexes, function and triggers; no existing record is rewritten. `lock_timeout=5s`, `statement_timeout=60s`. Approve the specific production migration before release. Preserve journal/audit/outbox data during an application rollback; do not drop the table to roll back code. Older code ignores the new table and all prior quarantine constraints remain. Run the repository migrator twice on the synthetic scratch database to verify idempotent bookkeeping, then apply through the normal owning GitOps release workflow after approval.

## Configured customer time and capacity

The outlet's persisted IANA `timezone` is the pharmacy customer's local-time setting. Onboarding captures it once; the selected outlet supplies it on every load and across catalogue, stock, safety, invoice and settlement history. A sibling outlet may use a different zone. The browser's own zone is not the customer setting. Customer-zone wall times are converted to UTC for APIs/storage; original ISO instants remain in `time` attributes/tooltips for audit. Date-only expiry and invoice dates are not shifted. Invalid configuration is rejected by the new safety/register surfaces. Missing or ambiguous daylight-saving wall times are rejected with an explicit error, rather than assigned an arbitrary instant; these cases require an unambiguous observation or an explicit-offset API submission. Existing hospital/clinical and cross-product zone settings are not replaced by pharmacy metadata.

Planning assumptions, not measured capacity: peak 20 history reads and 2 concern writes per second regionally (10:1), typical 25-row history around 32 KiB, bounded 200-row responses around 256 KiB, and a maximum 32 KiB request decoder. Assume 100,000 incident rows at 12 months and 300,000 at 36 months; at an illustrative 1 KiB/row that is 100/300 MiB before indexes, audit and outbox copies. Proposed objectives are 99.9% monthly availability and p99 ≤ 500 ms, subject to representative load tests. PostgreSQL is critical: failure returns an explicit error; a failed publication insert rolls the whole transaction back. The existing asynchronous relay tolerates broker outages and retries after commit, with existing outbox visibility. No new deployment, worker, cache or vector store is introduced, so incremental cost is database/audit/outbox storage and broker traffic. A small synthetic query-plan measurement is not a performance certification.

[Editable draw.io flow](../architecture/diagrams/13-pharmacy-batch-safety.drawio) accompanies the sequence diagram above.
