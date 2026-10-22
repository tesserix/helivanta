# India HMS domain knowledge

[API contracts](../../api/README.md) · researched 2026-10-22 · business specifications for review

Use this knowledge alongside the implemented API references, not as evidence that a planned workflow is deployed. Requirements tagged `IN-*` below are proposed Helivanta acceptance criteria unless explicitly identified as an external requirement in the [source register](sources.md). They are not treatment protocols, legal opinions or accreditation sign-off.

## Choose the domain

| Work                                                          | Knowledge and specification                                                                                                                           | Existing implementation reference                                                                         |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Registration, identity, proxy access, privacy, ABDM           | [Identity, consent and exchange](identity-consent-and-abdm.md)                                                                                        | [Patient/MediCore](../../api/domains/patients-and-clinical.md), [platform](../../api/domains/platform.md) |
| OPD, admission, nursing, beds, transfer, discharge            | [Clinical operations](clinical-operations.md)                                                                                                         | [Bed/admission draft](../../api/domains/beds-and-admissions.md)                                           |
| Prescribing, dispensing, stock and procurement                | [Pharmacy and supply](pharmacy-and-supply.md)                                                                                                         | [PharmaConnect](../../api/domains/pharmacy.md), [SupplyConnect](../../api/domains/supply.md)              |
| Specimens, results, critical communication, radiology         | [Diagnostics](diagnostics.md)                                                                                                                         | [LabConnect](../../api/domains/laboratory.md)                                                             |
| Tariffs, hospital invoices, cashless/TPA, schemes and claims  | [Billing and insurance](billing-and-insurance.md)                                                                                                     | [Product ownership](../../api/portfolio.md), [NHCX source](sources.md#exchange-and-revenue)               |
| Licensing, accreditation, infection control, assets and waste | [Facility and quality](facility-and-quality.md)                                                                                                       | [Remaining product coverage](../../api/portfolio.md)                                                      |
| Staff, payroll and professional credentials                   | [Existing workforce specification](../../api/domains/workforce.md), [India statutory research](../../payroll-india-statutory-reference-2026-10-16.md) | [Workforce API](../../api/workforce.md)                                                                   |
| Emergency calling, dispatch and receiving                     | [Existing India transport research](../../research/2026-10-18-india-emergency-transport.md)                                                           | [EmergencyConnect](../../api/domains/emergency.md)                                                        |

## India is a profile, not a universal legal switch

Before implementing a rule, record country, state/UT, facility and service type, legal entity, applicable licence/accreditation/empanelment, payer, effective dates, authoritative source and accountable reviewer. National legislation, state implementation, professional duties, payer contracts and hospital policy have different authority. A draft notification, expired certificate, old FAQ or a standards preview cannot activate a production policy.

Indian hospital workflows must support assisted registration, original-script names and transliteration, incomplete addresses, shared phones, estimated ages/DOB, unknown patients and intermittent connectivity. Patient care must have a safe workflow when an optional digital identity or external exchange is unavailable. These are product requirements; they do not authorize an agent to invent clinical triage or clinical/legal exceptions.

## Vocabulary that must remain distinct

| Term                                                     | Meaning for Helivanta                                                                                                                                             |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tenant / hospital / legal entity                         | Authorization/customer boundary, care facility and legally accountable business entity respectively; not interchangeable IDs                                      |
| UHID / MRN                                               | Local registration identifiers whose uniqueness and reuse follow the hospital's policy; neither is automatically a national identity                              |
| ABHA / ABHA address                                      | National digital-health identity and associated address concepts; preserve identifier type/system and verification state separately                               |
| Aadhaar                                                  | A separate identity scheme; no automatic care entitlement, portal permission or cross-hospital record grant                                                       |
| HFR / HPR                                                | Health Facility Registry / Healthcare Professionals Registry; registration evidence is distinct from current local privileges                                     |
| HIP / HIU                                                | Health-information provider / user roles in an exchange; not unrestricted system or database credentials                                                          |
| Care consent / exchange permission / outreach permission | Different purposes, actors, evidence and revocation effects; never one universal checkbox                                                                         |
| OPD / IPD / day care                                     | Outpatient, inpatient and defined day-care care contexts; a visit type does not prove admission or bed occupancy                                                  |
| MLC / LAMA / DAMA                                        | Medico-legal case and local terminology for leaving/discharge against medical advice; record the actual hospital-approved disposition rather than assume synonyms |
| Order / result / report                                  | Clinical intent, observed output and authorized interpreted/released document; none substitutes for the other                                                     |
| Prescription / dispense / administration                 | Authorized medication order, pharmacy fulfillment and actual patient administration; three separate attributable facts                                            |
| GRN / challan / supplier invoice                         | Goods receipt, movement/delivery document and payable evidence; mismatches require reconciliation                                                                 |
| TPA / preauthorization / claim / remittance              | Administrator, coverage decision, financial submission and payment evidence; approval is not settlement                                                           |
| PM-JAY / ABHA / NHCX                                     | Public assurance programme, digital-health identity and claims exchange; none implies eligibility or integration with the others                                  |
| NABH / NABL / licence                                    | Hospital/healthcare quality programme, laboratory accreditation body and statutory authorization; track exact issuer, scope and validity                          |
| Clinical discharge / financial close / bed readiness     | Separate milestones. A patient may have left while billing is pending and the bed is still being cleaned                                                          |

## Producing a reviewable domain change

Link applicable `IN-*` requirements, the owning API contract and source IDs in the specification or implementation summary. Define records, states, authorized actors, invariants, recovery and synthetic acceptance cases before changing a consequential workflow. Add exact field schemas at the owning canonical source; tables here are conceptual contracts, not new deployed endpoints.

Changing a regulatory rate, retention period, drug schedule, payer rule or external profile requires current primary-source verification and an accountable review record. [Source-register maintenance](sources.md#keeping-the-knowledge-current) describes the evidence to retain. Existing research is a lead, not automatic approval of a current rule.

## Reusable agent skills

The canonical repository skills are in `.agents/skills/`; matching `.claude/skills/` links expose the same files to Claude. Root `AGENTS.md` explicitly routes agents here even when native skill discovery is unavailable.

- [hms-india](../../../.agents/skills/hms-india/SKILL.md): choose scope, policy authority and domain references.
- [hms-india-clinical](../../../.agents/skills/hms-india-clinical/SKILL.md): patient identity, care transitions, orders and diagnostics.
- [hms-india-revenue](../../../.agents/skills/hms-india-revenue/SKILL.md): tariffs, liability, insurance, claims and settlement.
- [hms-india-regulated-operations](../../../.agents/skills/hms-india-regulated-operations/SKILL.md): pharmacy/supply, facility compliance and quality evidence.
