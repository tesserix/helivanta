# Supplier grant consent and effective access

Unreleased candidate. Shared IAM owns supplier grants and their hospital consent
projections. Source products keep stock, shipments, invoices and field-release
policy. Workforce keeps Staff, employment, accounts and postings. Hospital
publication or membership alone never grants supplier access. The existing
[planning drafts](partner-grants.md) remain non-authorizing.

Canonical records are in `contracts/supplier-access.json`; run
`node scripts/generate-supplier-access.mjs` and `--check`. Staff capability
catalogs remain canonical in `contracts/staff-access.json` and its immutable
migration history. This implementation adds IAM migrations 0039–0040.

## Ownership and consent

The source prepares a grant against an exact current bilateral relationship and
its terms digest. Scope comes from that relationship, including exact source
product, organization and location, hospital and distinct recipient tenant.
A grant targets either one named account or one hospital-owned group and one
closed template. Validity cannot exceed the commercial terms or 366 days.

An independent current source administrator offers the grant. A current hospital
Staff reviewer accepts the same immutable input after checking the recipient.
Named recipients require exact reviewed Staff/posting references and current
account/generation eligibility; no Staff directory is copied to the supplier.
Groups require the exact published version, membership policy, purpose and
validity. A reviewer cannot approve access for their own account or Staff identity,
or approve a group they are already nominated to receive through.

The directed transactional protocol records source offer, hospital acceptance and
source acknowledgement. Acknowledgement retains the source decision epoch.
`approved` records consent; it is not proof that a particular user currently has
access. Either party may suspend. Late or repeated exchanges cannot reactivate a
terminal grant. Source and hospital retain their own projections and append-only
history with original source provenance. Private recipient basis remains solely
in the hospital's IAM data.

## Browser/API contracts

All paths below are relative to `/v1/iam` and travel through Global API.

| Method    | Path                                                                     | Authority                                                                                                                                            |
| --------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| PUT / GET | `/supplier-grants/{id}`                                                  | Current source IAM capability and exact private source administration; prepare additionally rechecks current bilateral relationship and source facts |
| PUT / GET | `/supplier-grants/{id}/commands/{command}`                               | Independent source review or current source read; actor-bound immutable decision recovery                                                            |
| GET       | `/supplier-sources/{product}/{organization}/locations/{location}/grants` | Exact private source read; signed bounded keyset page                                                                                                |
| GET       | `/hospitals/{hospital}/supplier-grant-offers`                            | Current hospital-scoped reviewer and Staff identity; bounded keyset page                                                                             |
| GET       | `/hospitals/{hospital}/supplier-grant-offers/{id}`                       | Same exact hospital scope and current reviewer                                                                                                       |
| PUT / GET | `/hospitals/{hospital}/supplier-grant-offers/{id}/commands/{command}`    | Current hospital reviewer; acceptance checks current recipient source and relationship, reductions do not require trading eligibility                |

Writes require client UUIDv7 identities, exact expected version, a bounded reason,
and matching expected tenant/subject headers. Bodies reject unknown or duplicate
JSON keys and exceed neither 16 KiB nor the closed canonical fields. Changed
command content or another actor cannot reuse a command identity. Recovery
reauthorizes the current caller and returns the retained decision, including its
reason and resulting version. Sensitive reads are audited; unavailable authority
returns an explicit error and never a fabricated empty directory or approval.

Validation errors use the stable `invalid_request` code and a safe public message.
Wrapped provider and storage details, recipient identities and private evidence
are never copied into error responses.

## Effective disclosure boundary

The closed templates are catalogue view, shipment read and invoice view. Each
supplies one record-scoped permission. Hospital viewer capabilities only allow
requesting a current check; they do not grant access to any supplier by themselves.

The consuming authorizer must check current hospital Staff/account/posting,
current group membership where applicable, exact bilateral grant and commercial
relationship, current source eligibility, and an acknowledged content-bound
OpenFGA projection. Source tenant membership and tenant-wide permissions must not
be created for a hospital user. Projections include recipient eligibility and
membership epochs so an old tuple is insufficient after source changes.

Stock mutations, quality release, prescribing, patient records, supplier Staff
administration and financial decisions are outside these disclosure templates.
After a grant or relationship ends, retained hospital obligations remain governed
by their own financial/receipt owner permissions; historical consent is not a
replacement user grant.

The development candidate includes the consuming authorizer, shipment
projections and their SDK/UI. Hosted Linux run
[44992780809](https://github.com/tesserix/helivanta/actions/runs/44992780809)
passed positive admission through the actual IAM, Staff, graph, database and
stock owners for both warehouse and pharmacy sources. The same composed journey
covered split dispatch, hospital receiving, independent clearance/release,
directed source replies and receipt-backed buyer payables. External identity,
workload and object-storage protocols remain synthetic test boundaries; this
does not prove live onboarding or deployed browser acceptance.

Hosted correction run
[44999841841](https://github.com/tesserix/helivanta/actions/runs/44999841841)
also passed the full composed root. For both stock owners, an independently
reviewed recount retained original receipt and physical-release evidence,
reopened receiving, required explicit reclosure and updated current financial
choices and directed source replies without changing accepted/released stock.
These results precede the combined current-main integration; that integration
requires its own passing checks.

Catalogue and source-issued invoice consumers, platform supplier returns and
their source credits remain unfinished. Buyer-entered payables and credits do
not constitute source-issued documents. Batches with many distinct source
shipments also need bounded retained safety checks before the full workflow can
be declared complete. The presence of closed disclosure templates does not
establish those APIs or financial records. Shared-environment release and
authenticated browser acceptance remain separate gates.

## Effective shipment disclosure candidate (ADR-0076)

The current authorizer is now consumed by the development candidate in
`internal/modules/supply/partner_shipments.go`. All shipment reads use
`PartnerScopes.WithPartnerScope`, not the hospital-local eligibility port. They
operate on SupplyConnect's retained owner-published dispatch projection for
PharmaConnect outlet and SupplyConnect warehouse sources; neither reads another
module's stock tables nor moves stock.

Candidate paths under `/supply/hospitals/:hospital/partner-grants/:product/:grant/terms/:terms`:
`GET /shipment-source`, `GET /shipment-orders` and
`GET /shipment-orders/:order/shipments`.
The source singleton returns current organization/location names under the same
compound authority even before the first published dispatch. It commits its own
disclosure audit before returning metadata; it does not expose an empty or
fabricated order, catalogue, invoice or stock balance.
The two collections use sealed resource pagination capped at 50. All three reads
require `partner.shipment.read`,
with current expected account, exact hospital/grant/product, bilateral relationship,
current Staff/group/graph and source guards on every page. Only orders with
published dispatch facts become choices. A source SELECT purpose binds exact
source tenant/org/location and buyer tenant/hospital/org/outlet/relationship;
private command/history tables and all writes remain denied in that purpose.
A failed source audit or current authority check withholds the response.

`PartnerShipmentOrder` and `PartnerShipment` are canonical minimal projection
models in `contracts/supplier-access.json`. Their field bounds inherit the actual
source-owned order/fulfilment contracts. They include human references and frozen
physical description/unit/pack/quantity/lot/expiry/dispatch evidence, excluding
actors, Staff, prices, patient data and private source documents. Client parsing
rejects extra fields, mismatched account/grant/relationship/order/source,
duplicated rows and inconsistent/looping continuation. It keeps the hospital
account; it does not log into the source tenant or gain operational authority.

Additive migrations 0019/0020 establish the isolated purpose and concurrent order
keyset index. The migration first verifies the previous Supply operating-choice
inventory before refreshing it for the intended new restrictive policies. The new
purpose inventories its policies/function and fails closed for missing/bypassed
RLS, privilege drift, unsupported new tables or an absent/unready index. Original
migrations and ordinary source/buyer/exchange purposes are unchanged.

This remains dark under `supplier_collaboration`; eligibility/provider settings
are unchanged. Storage-purpose fixtures are not real IAM admission evidence.
Hosted native/positive real-grant tests, a reachable named recipient/source chooser
and browser UI, performance evidence, protected integration and approved live
acceptance remain gates in [ADR-0076](../adr/0076-effective-partner-shipment-disclosure.md).
Catalogue and source-issued invoice consumers remain distinct unfinished work.

Shipment sharing is restricted to the exact source/hospital-approved terms hash.
The `:terms` path segment is that canonical SHA-256, supplied by an authorised
chooser rather than entered by a user. It binds the sealed cursor; every query
also checks the owner order's original terms hash against the final current grant.
A changed agreement requires a refreshed choice/page. It does not broaden access
to another agreement's history; hospital-local retained obligations use their own
existing authority. Public projections include that `terms_sha256` for exact SDK
checking, without the private source document or its actors.

The candidate shipment order and shipment projections include required
`source_organization_name` and `source_location_name` fields. These come from the
current physical owner's presentation guard under the already-held compound
source grant, not from the request, actor ID or cross-module table reads. IAM
passes the verified seller endpoint descriptor through its internal grant; the
SupplyConnect consumer resolves the names and can only shorten that grant's
remaining deadline. A missing owner projection or invalid label withholds the
response. The canonical presentation bounds are 1–200 Unicode characters, with
no surrounding whitespace or control characters. The SDK preserves the names,
refuses missing/unsafe labels and refuses mixed source labels within a page.
The underlying v1 machine source-check reference remains unchanged; presentation
facts do not grant trading, staff, financial or physical stock authority.
