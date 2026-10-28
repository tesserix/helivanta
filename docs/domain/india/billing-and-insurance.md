# Hospital revenue, insurance and claims

[India knowledge index](README.md) · proposed business specification; no new billing API or live payment integration

MediCore owns hospital clinical charges/invoices; PharmaConnect owns pharmacy operational accounts and its future sales ledger; insurer/scheme systems own their decisions. A consolidated statement is an attributable projection, not a second writable ledger. Existing operational payables are not a complete patient revenue-cycle implementation.

## Patient and payer liability

| Requirement                  | Contract                                                                                                                                      | Important distinction                                                                                                                    |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| IN-RC-01 Effective tariff    | Service/item identity, care context, facility/legal entity, payer/contract, effective dates, price/currency, tax decision and applied version | A future tariff change cannot silently reprice a posted invoice; tariff, package and insurer-approved amount differ                      |
| IN-RC-02 Charge lineage      | Stable clinical/pharmacy/bed source-line ID, performed/ordered evidence as policy requires, quantity/unit and reversal reference              | A repeated event cannot double-charge. Order cancellation does not reverse an already-performed service without an authorized adjustment |
| IN-RC-03 Liability split     | Gross charge, discounts, package inclusion, insurer/scheme/employer entitlement, patient share and adjustment evidence                        | Estimated, approved, billed, paid and outstanding are separate amounts; no automatic zero liability on preauthorization                  |
| IN-RC-04 Invoice and receipt | Posted invoice/version, accountable legal entity, exact INR paise, numbering policy, tax/classification evidence, receipts and allocations    | Payment receipt and invoice posting are distinct; payment allocation, credit note, write-off and refund preserve original entries        |
| IN-RC-05 Reconciliation      | Provider reference, stable external transaction identity, settlement allocation, fees/reversals and accounting period                         | Timeout means unknown outcome; reconcile before recharging/refunding. A recorded external settlement is not a provider-side capture      |

Billing policies must explicitly decide bed occupancy boundaries, local day cut-offs, leave/transfer periods, consultant fees, follow-up entitlement, consumables, partial packages, cancellations and unbundled extras. Reuse the [bed interval contract](../../api/domains/beds-and-admissions.md). Show itemized explanations and estimate revisions; do not invent a daily room-charge rule or infer chargeability solely from a board status.

## Cashless insurance, TPA and government schemes

**IN-RC-06:** Preserve policy/member identifiers with issuer, eligibility evidence, payer/TPA/plan, network/empanelment, coverage period, authorization reference, approved scope/amount and decision version. Admission, emergency care, eligibility, preauthorization, enhancement, final authorization, claim submission, adjudication and remittance are separate lifecycle facts. A claim acknowledgement is not an approval, and approval is not payment.

**IN-RC-07:** Treat co-pay, deductible, room entitlement, sub-limits, exclusions and package inclusions as versioned contract terms with authorized review, not one global formula. State/scheme/package versions, hospital empanelment and beneficiary verification vary. PM-JAY, ABHA and NHCX are different systems; an ABHA link proves neither scheme entitlement nor a cashless guarantee. Do not copy package rates or annual limits from memory.

**IN-RC-08:** Track exact received/submitted/queried/answered/decided timestamps, source acknowledgements and service-clock policy. The Ministry of Finance's March 2026 response reports IRDAI timelines of one hour for cashless preauthorization and three hours for final authorization. These are insurer-side regulatory context, not a Helivanta API SLA or an automatic approval timer. Review current circular scope, clock triggers and amendments before activating breach rules. [Sources and direct-circular access limit](sources.md#exchange-and-revenue).

**IN-RC-09:** NHCX is an exchange contract, not Helivanta's ledger or an adjudicator. Pin the agreed message/profile version, participant identifiers, credentials, callbacks, deduplication and status/communication mapping. Preserve claim/query/cancellation/reprocess lineage, minimized protected attachments and timeout reconciliation. Sandbox enrollment or a structurally valid FHIR message is not production authorization or proof of payment.

## Tax and financial governance

**IN-RC-10:** Do not label every healthcare-related line exempt from GST. Review service category, inpatient/outpatient and supply context, applicable exemption/notification, legal entity, place of supply, effective dates, invoicing/credit-note rules and input-tax treatment with finance/tax owners. Room, pharmacy, non-clinical and bundled services need their own classification decisions. No tax rates or room thresholds are authorized by this specification. See the [CBIC source entry](sources.md#exchange-and-revenue); one circular is not a complete current tax policy.

**IN-RC-11:** Preserve financial period state, attribution, correction reason and source references. Require appropriate separation for discounts, write-offs, refunds and policy changes. Payment credentials and provider secrets stay outside logs and documents; never store payment-card security data as generic patient attachments. A financial workflow failure creates pending reconciliation, not a false clinical cancellation or silent deletion.

Review with hospital finance, insurance desk, authorized tax/legal reviewers, clinical/pharmacy source owners and each actual payer/provider adapter consumer. Test exact rounding/overflow; package/room entitlement changes; partial approval and enhancement; duplicate/reordered callbacks; unauthorized discount/refund; ambiguous payment result; duplicate charge event; backdated tariff; claim query/denial/appeal; stale empanelment; mismatched patient/member; and discharge when an insurer/accounting service is unavailable.
