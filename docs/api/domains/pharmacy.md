# PharmaConnect integration specification

[Contract index](../README.md) · descriptive current slices plus outstanding contracts

## Scope and authority

The hierarchy is tenant → pharmacy organization → outlet, with optional hospital affiliations. Independent pharmacies do not need a fake hospital. Route permission and a current explicit outlet grant are separate. An affiliation is neither patient consent nor prescription/stock authority. See [ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md), [country/trading boundaries](../../adr/0026-pharmaconnect-country-and-trading-boundaries.md) and [existing API detail](../../architecture/pharmaconnect-api.md).

The [runtime reference](../reference/README.md#pharmacy) lists every implemented path. The outlet root is `/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`. Organization/outlet administration and the legacy `/medications`/`dispenses` surfaces are separate route families; do not equate the legacy dispense task with the outlet stock ledger.

## Current contract families

| Family                                        | Owning records / schema                                                                                   | Rules and consumer boundary                                                                                                                          |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registry and grants                           | Pharmacy organizations, outlets, optional hospital affiliations, outlet access; `contracts/pharmacy.json` | UUIDv7 path records; verified session tenant; country/timezone explicit; administrator access management does not imply operational grant            |
| Private catalogue                             | Product fields, ingredients, packs, unit of issue, MRP/currency, status/version                           | Exact strengths/quantities and pack conversions; identifiers and lifecycle/version in catalogue events, not a global shared product table            |
| Batches and locations                         | Lot, manufacturing/expiry dates, storage/quarantine location and rack/shelf address                       | Dates interpreted in outlet timezone; a lot is not a clinical authorization to dispense                                                              |
| Stock ledger                                  | Immutable movement, receipt/correction evidence, quantity, location and batch                             | Quarantine receiving is not clinically usable stock; read current safety/expiry state before fulfillment                                             |
| Batch safety journal                          | Immutable concern records and context references                                                          | Recording an unreviewed concern does not silently implement regulatory recall/release decisions                                                      |
| Stock register, reconciliation and CSV export | Scoped period/register rows, expiry view, auditable export                                                | Preserve period identity and filters; report freshness and excluded/missing scope; no floating-point quantities or cross-outlet leakage              |
| Operational accounts                          | Purchase/expense payables, external settlement, credit/reversal references                                | Immutable attributable entries, versioned policies, currency and exact minor units; recording external settlement is not charging a payment provider |
| Legacy dispense queue                         | Visit-linked pending/dispensed task                                                                       | Guarded one-time completion; retry after completion returns 409; no signed prescription validation or stock deduction established by this handler    |

Canonical request fields and constraints live in [contracts/pharmacy.json](../../../contracts/pharmacy.json), generated `input_types.go` and `pharmacy-contract.ts`. Queries and response projections live in their owning handlers. Existing [API documentation](../../architecture/pharmaconnect-api.md), [stock-register delivery](../../delivery/2026-10-17-pharmacy-inventory.md), [batch-safety acceptance](../../delivery/2026-10-18-batch-safety-acceptance.md) and [stock export](../../delivery/2026-10-18-pharmacy-stock-export.md) provide detailed slice evidence.

Use client resource IDs and exact `expected_version` semantics of each handler; supplying a valid-looking UUID does not make every PUT replaceable or every POST retry-safe. The optional expected-tenant header returns `409 session_changed` if session context drifted. Never retry an in-flight action under a newly selected tenant.

## Integration requirements

Published catalogue, stock, payable, settlement and safety events mostly carry scoped references only. `dispense_recorded.v1` carries dispense/visit IDs. The current visit-intake consumer still receives patient name from MediCore and opens a pending task automatically; it is not a prescription order.

A clinical dispense contract must require an authorized current prescription/version, patient/hospital linkage, outlet authority, pack/unit conversion, available safe batch quantities, substitution policy and audit. Local stock reservation and dispense posting need durable idempotency and concurrency constraints. Billing uses a stable source-line ID. After physical dispense, billing failure is `charge_pending` recovery; deleting the dispense is not compensation.

Supply transfer requires a source dispatch, destination receipt, quantities/units/batches and immutable discrepancy record; only the receiving outlet owner updates its stock. Bilateral commercial sharing requires current country policy, licences, identity and approved relationship, not common ownership. Payment adapters must distinguish settlement recording from provider-side capture/refund and reconcile ambiguous responses.

## Remaining work and acceptance

Trace requirements to [#1133](https://github.com/tesserix/helivanta/issues/1133), [#159](https://github.com/tesserix/helivanta/issues/159), [#162](https://github.com/tesserix/helivanta/issues/162), [#166](https://github.com/tesserix/helivanta/issues/166), [#167](https://github.com/tesserix/helivanta/issues/167), [#987](https://github.com/tesserix/helivanta/issues/987), [#1060](https://github.com/tesserix/helivanta/issues/1060) and the [reconciled backlog](../../delivery/2026-10-18-pharmaconnect-backlog.md). Do not label these whole stories complete because a foundation exists.

Approve full sales/returns/recalls/expiry release, signed prescription fulfillment, GST/tax policy, price schemes and regulated reporting per country before defining stable external contracts. Required tests include negative/overflow quantity, concurrent receipt/correction, duplicate command with changed content, expired/blocked batch, cross-outlet grants, stale tenant, local midnight expiry, export scope, settlement reversal and consumer retry after commit. Database failure denies a stock/financial mutation; broker lag leaves outbox work pending; a report must expose unavailable data rather than show a false zero.
