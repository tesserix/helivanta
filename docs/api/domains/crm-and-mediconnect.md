# CRM, MediConnect and engagement boundaries

[Contract index](../README.md) · **engagement contracts remain draft; MediConnect accounts and unverified link-request intake are implemented**

See the [account API](../mediconnect.md) for implemented create/read/preferences and unverified link-request
contracts. No CRM, patient linking, booking or report-disclosure API is implied.

## Three different customer relationships

| Boundary                                  | Authoritative owner                                                                                                                                          | Permitted purpose/data                                                                                                             |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| Commercial CRM                            | External CRM for leads, contacts, pipeline, forecasts and commercial terms; platform for technical discovery, feasibility, disposition, DPA/tenant lifecycle | Commercial hospital/customer relationship. No clinical chart, symptoms, precise patient location or trip traces                    |
| Hospital patient engagement / MediConnect | Hospital-scoped engagement records and platform identity/consent; clinical owners retain charts/orders/results                                               | Verified patient/dependent portal, appointments, referrals, approved outreach and patient support under purpose-specific authority |
| Emergency operational coordination        | EmergencyConnect canonical case and accountable operator                                                                                                     | Callback/contact attempts, caller-unreachable, related calls, complaints, lost property and escalation with case-scoped access     |

Issue [#808](https://github.com/tesserix/helivanta/issues/808) explicitly places lead capture/qualification/pipeline in an external CRM, while the [architecture](../../architecture/03-data-and-workflows.md) includes hospital engagement contacts/enquiries/referrals. These are compatible only when their entities and lawful purposes remain distinct. The CRM vendor is not selected here. `MedCora` is an older naming error; the clinical product is MediCore, and MediConnect is patient-facing.

## Proposed commercial contract

Stable linkage is `(crm_connection_id, external_opportunity_id)` ↔ platform `prospect_id`, and eventually tenant/onboarding references. External IDs are opaque strings scoped to a configured CRM connection, never tenant authority. Do not match by contact name/email or trust a webhook-supplied `tenant_id`.

| Fact                                                                     | System of record                   | Direction / conflict rule                                                                                |
| ------------------------------------------------------------------------ | ---------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Contact identity, pipeline stage, forecast, commercial terms             | CRM                                | CRM → platform allowed projection; platform edits must go through the owner                              |
| Technical discovery, feasibility, volumes, delta capability, disposition | Platform                           | Platform → CRM selected summary; CRM cannot overwrite technical evidence                                 |
| Implementation estimate and go-live range                                | Platform                           | Platform → quote workflow with source revision; never silently replace an executed commitment            |
| Contract execution evidence / DPA lifecycle                              | Approved contract/compliance owner | Evidence flows into platform verification. Commercial stage such as closed-won is not executed-DPA proof |
| Tenant lifecycle / activation / processing gate                          | Platform                           | Platform → CRM status; CRM outage cannot revoke or invent lifecycle authority                            |

Resolve the apparent wording mismatch in #808: it names platform DPA execution state as authoritative while also asking execution state to flow platform-ward. Separate external evidence ingestion from the platform's verified lifecycle decision. [#451](https://github.com/tesserix/helivanta/issues/451) owns DPA lifecycle; [#811](https://github.com/tesserix/helivanta/issues/811) owns MSA/order execution and activation gates.

Proposed processing: authenticate adapter/webhook signature and timestamp → map configured connection to allowed scope → validate typed allowlisted event → persist inbox/deduplication and intended projection atomically → apply owner/version rules → enqueue response/projection updates. Acknowledge after durable receipt. Distinct states: received, applied, rejected, reconciliation required. Repeated delivery with the same ID/content reuses its recorded result; reused ID with changed content is rejected and investigated. Out-of-order source versions cannot overwrite newer values.

No provider-specific endpoint, signature algorithm, deduplication retention, poll/webhook cadence or source revision format is ratified yet. Choose those with the vendor and record an ADR. The adapter must support bounded retry, dead-letter handling, reconciliation cursor and operator ownership. On CRM outage, persist platform work and show sync-pending; do not block already-authorized onboarding. Missing verified DPA/activation authority still blocks admitting users/data as required by #811.

Technical discovery [#807](https://github.com/tesserix/helivanta/issues/807) feeds the implementation estimate and handover [#813](https://github.com/tesserix/helivanta/issues/813); reuse that record rather than copy it into a second handover document. Record named owners, commitments, variance and agreed scope. These workflows remain unimplemented contracts, not additional fields on `supply_parties`.

## Proposed patient and hospital engagement contracts

The [MediConnect delivery review](../../delivery/mediconnect.md) tracks current
source gaps, assigned issues and the email/WhatsApp OTP release scope (SMS
disabled). The [onboarding proposal](../../superpowers/specs/2026-10-24-mediconnect-onboarding.md)
is a draft, not an implemented patient API.

MediConnect must bind authenticated patient/dependent accounts to reviewed hospital registrations. A shared household phone, CRM contact ID or guardian assertion does not confer chart access. Distinguish legal guardian, booking on behalf, emergency contact and permission to view records, each with scope/expiry/revocation evidence.

Appointments, released reports, prescriptions, bills and referral status are projections/commands of their owning products. Portal response contracts must include source IDs/version, release/visibility state and freshness. A withdrawn/amended result or revoked proxy grant must invalidate downloads and offline copies according to approved policy. FHIR, ABHA linking and consent manager flows are future adapter contracts, not current `/v1` guarantees.

Hospital A → B referral must establish a sharing agreement and minimal purpose-bound disclosure, explicit receiving acceptance and current consent/policy. B owns its new clinical work. A may see the permitted referral acknowledgement, not B's subsequent chart. Cancelling coordination cannot reverse a completed encounter.

Marketing consent is separate from care/emergency processing. Before sending a campaign, recheck current suppression, consent, hospital scope and channel preferences; queue receipt or historic consent is not send authority. Emergency processing never enrolls a caller in marketing. Operational status/complaint views remain case-scoped and expose no clinical notes by default.

## Review and acceptance

Owners: platform/integration engineering, commercial operations, implementation/customer success, hospital operations, privacy/security and the relevant clinical domain. Decisions needed: CRM vendor, exact field ownership and IDs, webhook security, source versions/cadence/retention, DPA authority, patient/proxy identity, permitted disclosure and reconciliation SLO.

Trace to #808, #807, #811, #813, #1224, [#75](https://github.com/tesserix/helivanta/issues/75), [#60](https://github.com/tesserix/helivanta/issues/60), [#62](https://github.com/tesserix/helivanta/issues/62), [#552](https://github.com/tesserix/helivanta/issues/552) and [#556](https://github.com/tesserix/helivanta/issues/556). Required cases include forged/wrong-connection webhook, replay/changed payload, out-of-order update, timeout after commit, outage/recovery, duplicate opportunity links, conflict on ownership, PHI rejection, revoked proxy/referral and marketing suppression immediately before send.
