# Platform identity, tenancy and shared contracts

[Contract index](../README.md) · current implementation with target gaps

## Identity and scope

IAM owns membership grants/revocation, session activity/renewal/sign-out, current permission/membership queries and tenant switching. The verified Helivanta session selects tenant; IDs in request data or headers cannot override it. `GET /v1/iam/me/tenants` returns caller memberships; readable tenant names remain tracked in #1264. Switching re-mints the session and clients must invalidate/rekey scoped state.

The current login boundary uses Zitadel and Helivanta session minting. Earlier README/backlog descriptions of hosted login/GIP/Keycloak may be historical. Current wiring in `backend/internal/bootstrap/unauthenticated.go`, IAM login handlers and `apps/shell` owns what is served. Session renewal/activity, absolute/idle lifetime, revocation watermarks and sign-out are security contracts, not optional frontend conventions. See [session lifetime](../../architecture/session-lifetime-and-remembered-login.md).

Tenant, hospital, pharmacy outlet, supply premise, emergency base and receiving facility have different scopes. A tenant can contain multiple hospitals; a standalone pharmacy is not a hospital. Forced RLS and handler object grants implement narrower boundaries in the existing domains, but the legacy visit/lab/dispense slice remains tenant-scoped. Do not claim universal hospital isolation until that slice is migrated and tested. Group-sharing architecture is proposed and does not make all same-tenant records mutually visible.

## Shared capability contracts

| Capability                    | Current source                                                             | Contract/review requirement                                                                                                              |
| ----------------------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Identity/organization linkage | IAM and `pkg/organizationidentity`; `contracts/organization-identity.json` | Verify external identity authority and current linkage; unavailable authority is not an allow                                            |
| Branding/workspace            | `workspace`; [workspace API](../workspace.md)                              | Published login branding differs from authenticated branding management; display metadata cannot confer access                           |
| Country policy                | `workspace/country_policy.go`; `contracts/trading-policy.json`             | Submit/read/review/access/version lifecycle, cited evidence and reviewer authority; no hardcoded country exception replacing policy      |
| Patient identity              | `patientmaster`; [clinical spec](patients-and-clinical.md)                 | Hospital grants, consent/matching and access audit; ID alone does not grant care access                                                  |
| Private files                 | `pkg/attachments`, workforce/supply file owners                            | Registered owner/scope, size/type, exact generation/checksum, quarantine scan, authorized release and download, revoke/retry lifecycle   |
| Audit                         | `pkg/audit`, owner audit tables and migration triggers                     | Actor/tenant/object/action/time and attributable changes; no unrestricted PHI export or mutating historical evidence                     |
| Events                        | `pkg/events`, module `/contract` packages                                  | Atomic outbox/inbox, declared subjects, bounded delivery and scoped provenance; [integration specification](../integration-contracts.md) |
| Pagination/insights           | `pkg/pagination`, platform list/insight helpers                            | Cursor bounds and calendar/timezone/window semantics; reports state freshness and missing data                                           |

File workflows require GCS, exact-content ClamAV verdicts and a matching durable Temporal execution. Storage failure leaves uploads incomplete/quarantined; scanner unavailability cannot yield clean status; revoked/changed content cannot reuse an old verdict. Protected release/download is reauthorized, and a path prefix or signed URL is not the full security model. See [file storage/scanning architecture](../../architecture/07-file-storage-and-scanning.md) and each current owner API.

## Future external/service boundaries

Machine delegation, audience/scopes, Istio workload identity, region/country routing, product entitlements, DPA activation, cross-hospital sharing and offboarding/retention require reviewed contracts before API extraction. A browser session API is not automatically an external partner interface. Preserve current resource IDs and a single writer during extraction; route/version rollout must include reconciliation and recovery for writes accepted by the new owner.

Adapters for FHIR/ABDM, HL7 analysers, DICOM/PACS, TPA/NHCX, payment, accounting/ERP, SMS/email/WhatsApp and government emergency systems need explicit tenant mapping, credential ownership, authenticity/replay checks, minimum disclosure, idempotency, timeout reconciliation and approved country policy. No connector is certified merely because its issue or route prefix is present.

Threat boundary: patient/staff/commercial personal data, financial/stock state and credentials are protected from unauthenticated callers, another tenant, overprivileged insiders and compromised adapters. Validate and authorize every boundary crossing, minimize broker/CRM payloads and logs, and fail closed for clinical/security authority. Record availability effects separately from permission denial.
