# Interstate counter sales (#1689)

Status: IGST and the all-India UTGST extension are merged and deployed.
Live API checkout, credit, report/CSV and Tally acceptance passed; final
posted-document browser verification remains open. This is not a
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

## Acceptance evidence

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

The deployment evidence is separate from the live transaction results below.
The existing tenant pilot expires at `2026-11-26T06:00:00Z`; this release does
not extend it or unlock other tenants.

### Live synthetic API acceptance (2026-11-25)

All commands used the deployed Global API, normal OIDC authorization-code/PKCE
sign-in and verified tenant/outlet scope. No business table, identity projection
or session was written directly. A dedicated QA account was created without
roles and completed normal private email-verification, password-setup and
TOTP-enrollment flows. Ordinary membership and temporary review/outlet access
were granted explicitly and separately.
Its independently authenticated subject reviewed the owner's synthetic licence
and schedule declarations. The licence review retained the clean scanned passive
test image. These test declarations assert no real licensing or clinical authority.

The owner received 40 explicitly synthetic units into quarantine. Release before
review returned `409 not_releasable`; balances remained quarantine 40/sellable 0.
After independent review, release and identical replay left quarantine 0/sellable 40. The following invoices each sold 10 units for INR 125.00 at the synthetic
12% classification. Amounts in the table are integer paise.

| Synthetic invoice | Seller → recipient                  | Taxable | CGST | SGST | IGST | UTGST |
| ----------------- | ----------------------------------- | ------: | ---: | ---: | ---: | ----: |
| `T168/2627/1`     | Maharashtra → omitted (Maharashtra) |   11160 |  670 |  670 |    0 |     0 |
| `T168/2627/2`     | Maharashtra → Odisha                |   11161 |    0 |    0 | 1339 |     0 |
| `T168/2627/3`     | Chandigarh → Chandigarh             |   11160 |  670 |    0 |    0 |   670 |
| `T168/2627/4`     | Chandigarh → Odisha                 |   11161 |    0 |    0 | 1339 |     0 |

Every quote matched the posted invoice; identical command replay returned the
same document and changed-destination replay returned 409. Unsupported recipient
code `99` returned 409. Six credit notes reversed all four sales: full credits
for invoices 1/4 and 3+7 unit partial/final credits for invoices 2/3. The original
Maharashtra taxes remained unchanged after the synthetic registration correction
to Chandigarh. Each original taxable/tax component reversed exactly; an additional
return after full credit returned 409 without moving stock. The original synthetic
Maharashtra registration was restored, and all 40 units ended in quarantine with
zero sellable stock.

Daily/item JSON and CSV agreed: four sales, six credits, 40 units each direction,
INR 500.00 sales and credits, INR 446.42 taxable each direction, CGST 13.40,
SGST 6.70, IGST 26.78 and UTGST 6.70 each direction; net amount/units were zero.
Tally refused an unmapped export and separately refused one missing only UTGST.
Explicit synthetic ledger mappings produced 10 individually balanced vouchers,
matching debit/credit totals for every component. Repeated XML was identical.
No accounting-system import, statutory filing, real medicine or real funds were involved.

The reviewer received 404 at another outlet. Both temporary review-role and
outlet grants were removed after acceptance; ordinary membership was retained,
review permission disappeared and outlet access was denied. The seven pilot
capabilities were re-enabled through audited Console at the user's request and
verified in the SDK; authorization and absolute tenant-policy expiry still apply.

The posted invoice/credit/report browser check is still pending because native
Chrome use was interrupted by the user; extension attachment was unavailable
without a desktop-app network policy. Earlier deployed owner-counter smoke and
hosted browser tests passed. This does not substitute for the remaining check;
issue #1689 stays open until it is recorded.

### Automated acceptance coverage

`TestCounterTaxDestinationsCoverAllIndianJurisdictions` verifies all 1,369
seller/recipient combinations and omitted-destination defaults against the
canonical catalogue. It independently checks the complete supported code set,
single rounded IGST and unchanged same-jurisdiction components. This is focused
in-process calculation evidence, not a claim that every jurisdiction pair was
posted through the live checkout.

Hosted tests cover Maharashtra to Odisha and the reverse; same-state/omitted destination;
invalid jurisdiction; mixed rates, nil/exempt and discounts; NPPA boundary;
quote/post parity; idempotent and conflicting retries; original-rate partial and
full credits (including concurrent over-return); totals, report/CSV and balanced
Tally export; tenant/outlet denial and database tamper rejection. Hosted
integration/E2E tests use isolated synthetic fixtures, never real medicine or
financial records. Production migration/rollout requires approval for the exact
revision. Counter/Reports release gates must not be weakened to claim acceptance.
