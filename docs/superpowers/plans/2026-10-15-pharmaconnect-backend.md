# Pharma Connect implementation plan

Owner: Sam123ben. Parent #979; initial implementation #159. Spec: ../specs/2026-10-15-pharmaconnect-backend.md.

1. Inventory and assign all 23 labelled stories; read architecture and legacy surfaces. Record evidence and dependencies without customer data.
2. First red: create outlet-private product through the real HTTP harness; expect full persisted identity, idempotent retry and isolated read. Observe absent-route failure.
3. Add append-only schema for pharmacy/outlet directory and grants, catalogue heads and immutable revisions; forced tenant/pharmacy/outlet RLS, composite keys, audit, no implicit pharmacy access.
4. Add strict bounded PUT/GET/list/history, UUIDv7 IDs, SKU uniqueness, version and lifecycle guards. Publish catalogue change via transactional outbox.
5. Expand adversarial tests: other tenant/pharmacy/outlet, reader/permission denial, raw app-role RLS, duplicate SKU, stale/concurrent writes, rollback, revision immutability and malformed/oversized/regulated fields. Seed entirely synthetic products in tests through the real API.
6. Run package integration/race, architecture checks, lint, vet/build and coverage gates. Record exact commands and actual limitations.
7. Continue with governed regulatory catalogue and import, then batch/stock/expiry, then verification/fulfilment, accounting/returns/recall and reports as documented in the story matrix. Do not close the parent or #159 based on its identity slice.
8. Release only tested reviewable changes through the existing authorised GitOps process; synthetic live acceptance follows rollout. UI follows the user's requested API-first sequencing.

Ownership correction (user clarification): ADR-0024 supersedes the hospital-mandatory draft. Test standalone/no-hospital, chain outlets, multiple hospital-owned pharmacies, wrong-parent paths, explicit access and pending affiliation declarations. No production changes have occurred.

## Added operational finance slice

Local implementation now includes immutable bills/expense lines, unique stock-receipt acquisition cost links, recorded partial external payments, credits, exact payment reversals, protected outstanding balances and a batch cost audit. UI `/pharmacy/accounts` uses shared validated selects and exact currency inputs, automatically focuses newly opened forms and saved documents, and stacks financial rows on mobile. See [ADR 0025](../../adr/0025-pharmacy-cost-and-payable-audit.md) and [verification evidence](../../architecture/pharmaconnect-validation-2026-10-15.md).

This is a completed local foundation slice, not completion of the full pharmacy epic. Next acceptance remains governed catalogue/regulation and licences, released stock/reservations/FEFO, prescription verification/fulfilment, procurement/Finance posting contracts, sales/customer liabilities and refunds, statutory reporting, evidence-document integration and production verification. No issue is closed on the strength of these foundation tests alone.
