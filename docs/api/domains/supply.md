# SupplyConnect integration specification

[Contract index](../README.md) · current foundation and future procurement boundary

SupplyConnect owns tenant-scoped supply parties, contacts/addresses/tax registrations, premises and bins, licence declarations/evidence/reviews, corporate structure, territories, expiring role grants, platform-identity links and operational delivery/access metadata. These are supplier/distributor business records, not patient demographics or a generic customer CRM.

The [runtime inventory](../reference/README.md#supply) enumerates `/v1/supply` paths and permissions. `contracts/supply.json` generates Go input types and frontend Zod schemas; `organization-identity.json`, `country-profiles.json` and `trading-policy.json` define shared identity and reviewed country-policy inputs. See [ADR-0027](../../adr/0027-supply-party-records-and-access.md), [ADR-0028](../../adr/0028-supply-verification-and-evidence.md), [ADR-0031](../../adr/0031-live-platform-identity-for-bilateral-trading.md) and [scoped access](../../architecture/distributor-scoped-access.md).

## Contract families

| Family                         | Contract scope                                                                                      | Important restriction                                                                                                             |
| ------------------------------ | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| Party registration and history | Party ID, country/type, business identity, contacts, addresses, tax registrations, expected version | Exact fields/normalization come from canonical schema; history preserves versions                                                 |
| Premises, branches and bins    | Party/premise/bin hierarchy, location type, operational address, version                            | A bin is not an inventory balance or an approved regulated-storage location by itself                                             |
| Licences and review            | Declared regulator/licence, premises/scope, validity dates, evidence and reviewed decision          | A submitted document is not a verified licence; reviewer authority and current policy matter                                      |
| Evidence files                 | Registered file identity, upload generation/checksum, scan attempt/status, protected download       | Quarantine until exact content is clean and release is authorized; revoked/stale attempts cannot release content                  |
| Access                         | Subject, role, organization/premise scope, allowed duration and revision                            | Owner/admin, warehouse, picker/packer/driver and support grants have different scope; broad role membership alone is insufficient |
| Corporate and territories      | Parent/control relationships, representatives, service area and revisions                           | Territory metadata is not bilateral trading approval or cross-tenant disclosure permission                                        |
| Delivery tasks and support     | Named task/command, operator scope, evidence, expiry and version                                    | Record only the implemented operational lifecycle; do not equate task completion with invoice/stock settlement                    |
| Live platform identity         | Approved supply-party linkage to platform organization identity                                     | Verify current authority; an obsolete snapshot or arbitrary external tenant UUID cannot authorize trade                           |

Expected-tenant preconditions detect workspace drift and return `409 session_changed`. Resource/command UUIDv7 validation, content-reuse checks, immutable history and version conflict semantics are handler-specific. Do not replace them with an invented universal PUT-upsert rule.

## Procurement and pharmacy handoff still to specify

Future quote → approved rate → PO → dispatch/challan → GRN → supplier invoice → settlement must reference a single commercial source at each stage, with exact ordered/received/accepted/returned quantities, units, prices, currency, tax policy and source-line identities. Partial receipt, over/short supply, damaged/expired batches, duplicate document numbers and backdated corrections need explicit business outcomes.

When stock moves to a pharmacy, SupplyConnect posts only source dispatch; PharmaConnect posts only its destination receipt. Broker delay leaves `in_transit`/receipt-pending reconciliation, not a second automatic shipment. Returns use new linked ledger facts. No cross-module SQL transaction or direct stock update is permitted.

Bilateral trading requires approved relationship, current organisation identity, country-specific authority, valid premises/licences and explicit permitted data. Do not infer a customer/supplier connection from a phone number, tax identifier or territory match. Separate private trade from any optional marketplace/payment service.

Published events are minimized party/premise/licence/bin/access/corporate/territory/task references. The evidence-scan request is consumed through the configured Temporal bridge, with a deterministic workflow ID bound to tenant/party/licence/file/scan attempt. A third-party identity outage must deny an authority-dependent trade command or leave review pending; it must not fall back to a stale allow. GCS/scanner outage leaves evidence quarantined and visible as unavailable/pending.

## Traceability and tests

The [India/Pakistan requirements](../../requirements/pharmaconnect-india-pakistan.md), [legacy procurement requirements](../../requirements/pharmaconnect-legacy-procurement.md), [licence evidence](../../supply-licence-evidence.md) and [delivery backlog](../../delivery/2026-10-18-pharmaconnect-backlog.md) retain detailed field/state coverage. Relevant issues: [#1135](https://github.com/tesserix/helivanta/issues/1135), [#1136](https://github.com/tesserix/helivanta/issues/1136), [#1137](https://github.com/tesserix/helivanta/issues/1137), [#1000](https://github.com/tesserix/helivanta/issues/1000), [#1002](https://github.com/tesserix/helivanta/issues/1002), [#1005](https://github.com/tesserix/helivanta/issues/1005), [#1006](https://github.com/tesserix/helivanta/issues/1006), [#1147](https://github.com/tesserix/helivanta/issues/1147).

Contract acceptance must exercise cross-party/premise denial, expired/revoked support grants, concurrent revisions, identical versus changed-content retries, missing/stale platform identity, licence expiry at premise-local boundaries, scan replay/wrong-generation verdicts and safe download after revocation. Full procurement OpenAPI and cross-product ledger reconciliation remain review work; the registry alone does not establish their implementation.
