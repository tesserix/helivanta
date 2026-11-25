# Interstate counter sales (#1689)

Status: IGST and the all-India UTGST extension are merged and deployed.
Live checkout/credit/export acceptance remains open. This is not a
production tax-policy approval.

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

### Nationwide geographic scope

The intended counter capability is India-wide, not Maharashtra-specific.
Supplier registration and recipient selection use the canonical India GST
jurisdiction catalogue: all 28 states and 8 union territories, plus the supported
`97` Other Territory code. Retired codes `25` and `28`, foreign codes, and special
registration codes that are not supported outlet locations remain invalid.
Maharashtra/Odisha are acceptance examples, not an activation allowlist. The
temporary Yes Hospital pilot is an authorization boundary for testing; it does
not limit the geographic tax calculation or authorize other tenants to use
unreleased retail features.

The predecessor refused sellers in UTGST jurisdictions. A
recipient list or a calculation test does not establish checkout support there.
The deployed extension adds explicit UTGST rather than removing that guard while
continuing to print SGST. The same reviewed product rate applies regardless of the selected state; changing
destination chooses the appropriate component split and does not select a new
medicine tax rate. Same-jurisdiction invoices retain the existing CGST plus
SGST treatment, with the extension using a separate UTGST component where
applicable. Different supported jurisdictions
use IGST. Local licensing, registration validity and product review remain
separate prerequisites; selecting a state cannot satisfy those requirements.

The extension adds default-zero `utgst_minor` to immutable sale/credit headers,
goods and charge lines through migration `0073_pharmacy_union_territory_gst`.
It preserves original components on partial credits, adds report/CSV fields and
requires explicit `utgst:<rate_ppm>` Tally mappings. API responses gain additive
fields; SDK readers tolerate their omission during mixed rollout. No input
accepts client-calculated tax. Database destination, rounding, source, balance,
stock, tender and refund checks remain authoritative. No other owner, route,
permission, cache or external dependency is introduced.

The existing transaction limits and portfolio SLO/capacity assumptions apply;
six default-zero columns add bounded row storage, with no new index or service
cost. Migration lock/statement timeouts bound DDL waiting. Old records remain
zero UTGST. After UTGST writes, application recovery is forward-only: rollback
to a reader that omits UTGST would misstate financial evidence. Hosted scratch
migration/ledger tests and protected rollout approval are required before any
live UTGST posting.

Jurisdiction kinds reuse the canonical India profile. The SQL migration retains
the corresponding immutable jurisdiction snapshot for its financial guards;
public-handler tests cover every seller's local/interstate quote. UTGST Act
sections 1, 2 and 7 were checked against the [Tripura High Court-hosted statutory
text](https://thc.nic.in/Central%20Governmental%20Acts/Union%20Territory%20Goods%20and%20Services%20Tax%20Act%2C%202017.pdf)
on 2026-11-25; the IndiaCode source timed out. This confirms component scope,
not a medicine rate or a facility's authority. Accountable finance/tax review
still applies.

Different supplier and recipient states use IGST; same-state sales retain the
existing CGST/SGST calculation. Rates remain the product classification in force
on the outlet date; no rate is introduced here. Independently taxed service
charges on interstate invoices are refused pending their own section 12 analysis;
principal-supply charges follow the goods only under the existing same-rate guard.

Sources checked 2026-11-25 (the CBIC Act page is the original text; the
2024 circular supplies the amended unregistered-recipient rule):

- IGST Act, sections 7, 8 and 10: https://cbic-gst.gov.in/hindi/IGST-bill-e.html
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

### Deployment evidence (2026-11-25)

- [PR #1865](https://github.com/tesserix/helivanta/pull/1865) merged as
  `f6a36e9635a4e1226366e04d6693416c0afc763f`; all PR checks passed.
- [Main image build](https://github.com/tesserix/helivanta/actions/runs/45586527597)
  built, scanned, signed and published both release images.
- The explicitly approved [production release](https://github.com/tesserix/helivanta/actions/runs/45587574985)
  verified those signatures and advanced `release/prod` to that revision.
- Kargo Freight `a4a5af546b33fcd274e7c91dc749ce80cceb977a`, promotion
  `prod.01m891bxa6bf42d99tktgn80eb.a4a5af5`, succeeded. The migration hook
  completed with `migrations applied`; all seven application deployments reached
  their desired two updated, ready replicas on the new image digests.
- The refreshed owner browser session loaded the scoped QA counter, tenant and
  owner names, synthetic invoice profile and nationwide recipient choices.

This establishes deployment and a UI smoke check, not completed transaction
acceptance. The dedicated synthetic licence has clean scanned evidence but still
awaits independent review; the product schedule also awaits independent review.
Stock receipt/release, posted local/interstate sales, original-component credits,
report/CSV reconciliation and balanced Tally export remain to be verified live.
Issue #1689 stays open. The existing tenant pilot expires at
`2026-11-26T06:00:00Z`; this release does not extend it or unlock other tenants.

### Transaction acceptance still required

`TestCounterTaxDestinationsCoverAllIndianJurisdictions` verifies all 1,369
seller/recipient combinations and omitted-destination defaults against the
canonical catalogue. It independently checks the complete supported code set,
single rounded IGST and unchanged same-jurisdiction components. This is focused
in-process calculation evidence, not a claim that every jurisdiction pair was
posted through the live checkout.

Test Maharashtra to Odisha and the reverse; same-state/omitted destination;
invalid jurisdiction; mixed rates, nil/exempt and discounts; NPPA boundary;
quote/post parity; idempotent and conflicting retries; original-rate partial and
full credits (including concurrent over-return); totals, report/CSV and balanced
Tally export; tenant/outlet denial and database tamper rejection. Hosted
integration/E2E tests use isolated synthetic fixtures, never real medicine or
financial records. Production migration/rollout requires approval for the exact
revision. Counter/Reports release gates must not be weakened to claim acceptance.
