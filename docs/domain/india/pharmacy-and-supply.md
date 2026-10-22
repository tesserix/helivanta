# Pharmacy, medicines and hospital supply

[India knowledge index](README.md) · proposed domain requirements; [PharmaConnect](../../api/domains/pharmacy.md) and [SupplyConnect](../../api/domains/supply.md) remain current API references

## Medicines and fulfillment

| Requirement                          | Contract invariant                                                                                                                                                                     | Review/acceptance                                                                                                                       |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| IN-PH-01 Product identity            | Ingredient, strength/unit, dosage form, route, pack, unit of issue, manufacturer/marketer and identifiers are distinct. Keep batch/lot, expiry, supplier and price evidence separately | No matching only on brand name; changing pack/strength cannot silently reinterpret stock or a prescription                              |
| IN-PH-02 Clinical authority          | Preserve signed prescription/version, patient/encounter and authorized outlet scope; clinical amendment and substitution require their own policy and actors                           | A medication catalogue item, pending task or affiliated hospital grants no prescription authority                                       |
| IN-PH-03 Quantity and availability   | Exact quantity plus units/pack conversion; distinguish physical, quarantined, released, reserved, blocked/recalled, expired and available stock                                        | Sale cannot use quarantine/unsafe/expired stock; simultaneous reservations cannot oversell; fractions and returns preserve unit meaning |
| IN-PH-04 Dispense evidence           | Partial fulfillment links source prescription line, product, batch, quantity and pharmacist decision; repeats reuse durable command identity                                           | Timeout after commit does not dispense twice; amendment after partial fulfillment preserves the prior act and routes review             |
| IN-PH-05 Administration and reversal | Dispense is not administration. Returns, destruction, recall and financial credit are separate attributed records                                                                      | Returned stock does not automatically become usable; physical action is not undone by deleting a sale or charge                         |

FEFO can order otherwise eligible stock; it cannot override quarantine, recall, storage or clinical restrictions. Record outlet-local expiry evaluation and actual temperature/storage excursion evidence where required. A safety incident is an unreviewed concern until the authorized decision is recorded. Do not treat missing safety information as a release.

## Regulated pharmacy operations

**IN-PH-06:** Store effective regulatory classification and verified licence/premise scope with its source and reviewer. Schedule H, H1 and X and NDPS controls are not interchangeable. The CDSCO Drugs Rules text includes a separate H1 supply register with specified prescriber/patient/drug/quantity information and a three-year record period. Confirm current amendments and applicability before implementing the exact retention/register controls. No single hardcoded drug list is authoritative indefinitely. [Drug-rule and notification sources](sources.md#medicines-and-supply).

**IN-PH-07:** Distinguish pack MRP, applicable notified price ceiling, procurement cost, sale price, tax, discount and payer liability. Use current formulation/pack/effective-date authority; do not infer legal selling limits from an old NPPA FAQ or reuse a manufacturer's order for a different product. [NPPA's current-order source](sources.md#medicines-and-supply). This specification does not prescribe a tax rate or pricing formula.

Required reviewers are the responsible pharmacist, supply/finance owner and relevant compliance authority. Patient dispensing registers require controlled disclosure and access audit; they do not belong in unrestricted commercial analytics. Regulatory submission formats, controlled-drug registers and licence renewal actions require explicit reviewed contracts; a generic CSV export is not proof of statutory compliance.

## Procurement and stock transfer

**IN-PH-08:** Model request/indent, quotation/rate, purchase order, delivery/challan, received quantity, accepted/rejected quantity, supplier invoice and settlement as linked evidence. Track ordered, dispatched, received, accepted, returned and payable quantities separately. A GRN does not assert supplier invoice correctness or payment.

**IN-PH-09:** SupplyConnect posts its source dispatch and PharmaConnect posts its destination receipt. Retain in-transit state, lot/expiry, source-line identity, exact units and discrepancy resolution. Partial receipt or a timeout cannot create a second shipment or duplicate stock. No cross-module balance write or shared ledger is permitted.

**IN-PH-10:** Review supplier/premise identity, current licence, bilateral trading permission and product/country restrictions at the consequential command boundary. A tax registration, hospital affiliation, broad territory or historic licence scan is not present authority. Unknown/unavailable verification leaves review pending or denies the regulated action according to approved policy.

Test cross-outlet/premise access, stale tenant, expired/revoked licence, changed-content retries, partial receipt, short supply, duplicate invoice, pack conversion/overflow, concurrent stock changes, local-midnight expiry, recall during reservation, incorrect patient linkage, pricing-policy changes and ambiguous external settlement. Reuse the existing [India/Pakistan requirements](../../requirements/pharmaconnect-india-pakistan.md) and [legacy procurement requirements](../../requirements/pharmaconnect-legacy-procurement.md); do not duplicate their canonical models.
