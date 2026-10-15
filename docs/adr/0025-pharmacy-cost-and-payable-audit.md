# ADR 0025: Pharmacy purchase costs, expenses and payable audit

Status: accepted for local implementation; no production rollout approval implied.

## Context and targets

MRP is a selling-price ceiling, not acquisition cost. Auditors need the received drugs, supplier document, attributable expense, original tax/cost breakdown, recorded payments, credits, reversals and remaining liability. Standalone and hospital-owned pharmacies need the same controls without shared hospital ledgers. This extends #162/#166/#172/#1060; broader procurement and finance acceptance remains open.

Planning assumptions, not measured capacity: 100 outlets, 20 peak requests/s, 4:1 read/write, maximum request 32 KiB and 100 document lines. At 5,000 writes/day across the initial installation, approximately 1.8m entries/year and 5.5m in 36 months. Target availability 99.9%, p99 API 500ms, 2x annual growth. One indexed PostgreSQL is sufficient. Estimated incremental storage under 20 GiB in year one including audit/index overhead; validate against measured row sizes before production cost forecasting.

## Decision

Keep an operational payable subledger inside Pharmacy, not a duplicate general ledger or payment processor. A payable captures a supplier/payee reference and name snapshot, original document reference, dates, currency and bounded ordered lines. A line is either an inventory acquisition linked to exactly one immutable receipt, or a classified operating expense. Net amounts are after discount; explicit tax, recoverable tax and landed charges are snapshots, not calculated jurisdictional tax advice. Purchase cost = net + tax - recoverable tax + landed charges. Document liability = net + tax + landed charges. No floats or inferred tax rates.

Document identity is immutable; unique supplier/document and receipt-cost links prevent duplicate purchase liabilities. Payment records identify who paid, whom (the document payee), how, external reference, effective date and authenticated recorder. They record an external settlement only and never initiate a transfer. Partial payments leave the balance open. Credits and exact, once-only payment reversals are separate immutable records with reasons. Separate read, document-write, payment-record and adjustment permissions; initially tenant administrators only, plus an explicit outlet grant. Pharmacist receiving rights do not confer financial access.

Balances come from one PostgreSQL statement over immutable source rows. Database advisory locking and guard triggers serialize allocations per document, prohibit overpayment/over-credit and reject invalid reversals even with direct app-role SQL. Audit triggers and minimized outbox events commit with business writes. Events contain opaque IDs only. NATS JetStream delivery is at least once; downstream Finance must use document/entry IDs as unique posting keys. No cross-module table access. A future approved Finance contract owns chart of accounts, posting approval, period close, tax filing and bank reconciliation; recorded entries are not claimed to be posted or bank verified.

Inventory audit paginates each batch once, with SKU, lot, expiry, quantities, acquisition cost and uncosted receipts. Never present MRP as stock value. Original acquisition totals remain available; current valuation is unavailable when costs are incomplete or signed stock corrections need valuation review. Mixed currencies are never summed. Receipt cost links require the same tenant, outlet, positive receipt and product currency. Quarantine remains unavailable to dispense.

## Boundaries and failures

Assets: confidential supplier prices, balances and payment attribution. Threats: other tenants/outlets, unauthorized clinical staff and insiders altering history or duplicating payments. Verified tenant -> route permission -> live outlet grant -> forced RLS -> composite FKs/immutable triggers guard every operation. Identity and actor come from the session. Evidence attachments will reuse tenant-scoped GCS/quarantine/ClamAV after document authorization integration; no arbitrary URLs or raw file uploads here.

PostgreSQL is critical: writes fail without commit; unavailable access checks fail closed with 503. NATS outage retains committed outbox events for retry. Valkey is unnecessary for authoritative balances. Temporal is reserved for later external posting/approval/provider workflows with retries and compensation, not a single-database recording transaction. No new infrastructure cost is introduced.

## Migration and recovery

Migration 0006 is additive and forward-only, with bounded lock/statement timeouts. Test against disposable PostgreSQL with the non-BYPASSRLS app role and reapply idempotently. Financial history cannot be deleted to roll back; revert application code while retaining the tables and pause new writes through the normal release process. A production migration/rollout requires named approval and GitOps delivery. No production data is imported.

## Alternatives and remaining gates

A spreadsheet/free-text expense total cannot enforce receipt linkage or duplicate/overpayment protection. A separate finance microservice is unnecessary at this volume and would create a distributed transaction before a posting contract exists. Full ERP procurement, sales/COGS, approved FIFO or weighted-average valuation, multi-currency exchange, statutory invoicing, bank reconciliation and retention policy activation remain explicit later work; do not label this operational register a certified accounting ledger.
