# Interstate counter sales (#1689)

Status: implementation in progress; not a production tax-policy approval.

PharmaConnect owns sales, immutable invoice tax snapshots, stock movements,
credit notes and accounting/report projections. Shell consumes the existing v1
Global API routes; no new service, permission or cross-owner database access is
introduced. Both hospital-owned and independent outlets retain verified tenant,
pharmacy and outlet scope, object authorization, audit and existing release gates.

## Supported supply and tax evidence

The existing counter has no registered-recipient GSTIN field. This change covers
ordinary domestic supplies to unregistered recipients, not B2B, SEZ, exports,
imports, cess, reverse charge or special place-of-supply arrangements. Optional
`place_of_supply` on quote and sale is a supported Indian GST jurisdiction code.
It records the recipient state on the invoice; where billing and delivery differ,
the operator selects the delivery state. Omission retains the supplier state.
A registered patient's or admission's clinical address is not assumed to be tax
evidence. An admission-account sale cannot override the outlet state.

Different supplier and recipient states use IGST; same-state sales retain the
existing CGST/SGST calculation. Rates remain the product classification in force
on the outlet date; no rate is introduced here. Independently taxed service
charges on interstate invoices are refused pending their own section 12 analysis;
principal-supply charges follow the goods only under the existing same-rate guard.

Sources checked 2026-11-25:

- IGST Act, sections 7, 8 and 10: https://www.indiacode.nic.in/handle/123456789/2251
- CBIC Circular 209/3/2024-GST, section 10(1)(ca), including state-name evidence
  and delivery-address precedence: https://www.gstcouncil.gov.in/sites/default/files/2024-09/circular-no-209-03-2024.pdf
- CGST Act section 34 and invoice/credit-note rules remain subject to finance/tax
  review; this implements component preservation, not statutory filing approval.

## Money, consistency and recovery

All values remain integer minor units. IGST inside gross is rounded half up once:
`round(gross * rate_ppm / (1000000 + rate_ppm))`. Taxable is the residual.
Do not double a rounded CGST half. Discounts and NPPA ceilings use this same
split before and after discount. Existing intrastate rounding and old invoices
are unchanged. Each return reverses each original component independently using
the cumulative pro-rata difference; successive returns therefore sum exactly to
the original line, even for odd paise or changed current rates/registrations.

The existing bounded transaction (50 basket products, at most 200 batch lines,
5 charges/tenders), lock ordering, append-only records, document numbering,
stock movement checks and outbox remain authoritative. Destination participates
in command identity; a changed retry conflicts, while an identical retry reads
its recorded invoice. No new synchronous dependency, polling or cache is added.
Reuse the architecture sizing/SLO envelope; no new measured latency claim.

## Storage and consumers

Add a forward migration with default-zero IGST columns on documents, goods lines
and charge/credit lines. Replace amount, component, source and deferred balance
guards to include IGST without relaxing RLS, immutable/sealed-row or stock guards.
Preserve original tax on returns. Add explicit IGST to invoice/credit printing,
sales worklist, daily/item reports and CSV; Tally uses an explicit IGST ledger
head and refuses unmapped heads rather than discarding tax.

Expansion is additive. Existing stored rows read IGST zero; no financial history
is repriced. Lock/statement timeouts abort the migration transaction atomically.
After interstate writes, recovery is forward-only; rolling back to an application
that ignores IGST is unsafe. No extra infrastructure cost or index is expected;
release must verify migrations, scoped API behavior and actual UI before closure.

## Acceptance evidence to collect

Test Maharashtra to Odisha and the reverse; same-state/omitted destination;
invalid jurisdiction; mixed rates, nil/exempt and discounts; NPPA boundary;
quote/post parity; idempotent and conflicting retries; original-rate partial and
full credits (including concurrent over-return); totals, report/CSV and balanced
Tally export; tenant/outlet denial and database tamper rejection. Hosted
integration/E2E tests use isolated synthetic fixtures, never real medicine or
financial records. Production migration/rollout requires approval for the exact
revision. Counter/Reports release gates must not be weakened to claim acceptance.
