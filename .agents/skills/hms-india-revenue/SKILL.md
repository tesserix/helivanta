---
name: hms-india-revenue
description: Design or review Helivanta hospital tariffs, source charges, invoices, patient liability, cashless insurance, TPA or scheme claims, NHCX and settlement in India. Use for revenue-cycle behavior; statutory tax and payer decisions need current authoritative review.
---

# Indian hospital revenue cycle

Read [billing and insurance](../../../docs/domain/india/billing-and-insurance.md),
the relevant [source entries](../../../docs/domain/india/sources.md#exchange-and-revenue),
[product ownership](../../../docs/api/portfolio.md) and the affected actual API.
Do not equate pharmacy payables with patient billing or create a consolidated
statement as a second ledger.

Identify the legal entity/facility, patient/encounter, payer/scheme/contract,
effective tariff/package/tax policy and stable clinical/pharmacy/bed source-line
identity. Preserve exact INR minor units and quantity/unit semantics. Define
rounding, calculation inputs, version and explainable liability allocation.

Keep estimated, approved, charged, posted, received, allocated, refunded and settled
amounts distinct. Coverage or preauthorization does not prove payment. ABHA,
PM-JAY eligibility and NHCX participation are separate checks. Map each external
decision/callback to its owner and source reference/version; do not infer success
from elapsed time or a submission receipt.

For integrations, specify durable deduplication, replay/conflicting input,
out-of-order decisions, provider-side unknown outcomes and reconciliation before
recharge/refund. Posted financial evidence is corrected through linked authorized
entries, not erased. A financial outage must not fabricate a cancelled clinical act.

Use `IN-RC` cases appropriate to the slice: partial approval/enhancement, co-pay
and room entitlement changes, duplicate source charges, ambiguous payment,
unauthorized discount/refund, denied/query/appealed claim, stale empanelment,
effective-date changes and exact rounding. Validate the actual calculation and
posting, including its explanation and source lineage.

Do not hardcode tax exemptions/rates, scheme packages, coverage limits or insurer
timers from memory. Resolve scope and effective dates with finance/tax/insurance
owners using primary instruments. Pin exchange profiles and actual participant
agreements before implementation; this skill does not authorize live claims,
financial transactions or regulatory submissions.
