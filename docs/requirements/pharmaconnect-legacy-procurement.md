# Procurement reference map — read-only legacy review

Reviewed 2026-10-16 through the user's logged-in MedNet session. Epic #1133 and related exploration #979. This is field/workflow evidence, not permission to copy customer data or proof of legacy calculations. No legacy Save, Verify & Approve, upload, stock transfer or payment action was executed. Only labels/navigation are retained here; no vendor names, contacts, accounts or transaction row values enter fixtures.

## Complete procurement navigation

All entries in the user's highlighted sidebar are in scope. Reuse the owning SupplyConnect work and connect its APIs/contracts into the shared PharmaConnect experience.

| Legacy menu                 | Modern responsibility                                                                            | Owning story / dependency     |
| --------------------------- | ------------------------------------------------------------------------------------------------ | ----------------------------- |
| Rate Contract               | Effective supplier/region/customer prices, validity, approval and original quote snapshots       | #1000 #1138                   |
| Quotation                   | Supplier quotation capture/version, comparable units/packs/tax/charges and approval              | #1000 #159                    |
| Purchase Requisition        | Department/location demand, spend review, approval and source references                         | #1001 #160                    |
| Rate Comparator             | Eligible comparable offers with price/quantity/lead-time/service/licence/credit context          | #1000 #1138 #160              |
| Purchase Order              | Authorized draft/approval/placement, revisions, partial fulfilment and backorders                | #1001 #160 #1139              |
| Challan                     | Delivery/challan reference, actual quantities/custody and later invoice matching                 | #1002 #161                    |
| Purchase                    | Direct/ordered/challan/B2B receipt, batches/expiry/condition, invoice/tax/cost snapshots         | #1002 #162 #166 #1140         |
| Vendor Return               | Original receipt/batch return eligibility, collection and stock/credit reconciliation            | #1003 #163                    |
| B2B Txn                     | Explicit counterparty trading relationships and directed transaction exchange                    | #1136 #1139 #1147             |
| Transactions                | Immutable source-linked purchase/return/payment/reversal history                                 | #1002 #1003 #172 #1142        |
| GRN Register                | Goods-receipt register with source/vendor/lot/location/status and reconciled totals              | #1002 #1009 #1060             |
| Reorder Inventory           | Min/max/safety stock, preferred supplier and suggested pack/MOQ quantities                       | #1004 #1144                   |
| Manage Reorder              | Review/approve drafts, open inbound awareness, spend caps and bounded opt-in automation          | #1004 #1144 #1145             |
| Manage Inventory            | Warehouse/outlet stock states, costs, counts, corrections and traceable transfers                | #1004 #1008 #1009 #1010 #1140 |
| Setup: Items                | Canonical product/pack/item and manufacturer/category references, barcode and storage            | #1005 #159 #166               |
| Setup: Vendors              | Legal supplier identities, country/premises licences, contacts, approved payment/credit settings | #1005 #1135                   |
| Setup: Retailers            | Canonical retailer/customer identities and approved trading relationships                        | #1005 #1136                   |
| Setup: Indent Print Setting | Governed document layouts and source-linked indent evidence                                      | #1006 #988                    |
| Setup: Purchase Dept        | Procurement department, approvals and accounting/location mapping                                | #1006 #1001                   |
| Setup: Item Master Config.  | Governed required fields/classifications; tenant config cannot weaken regulation                 | #1005 #1006 #159 #171         |
| Setup: Selling Price Factor | Effective pricing/margin factors and reviewed invoice/rounding policy                            | #1006 #988 #1060              |
| Procurement Settings        | Country/org/location-aware source rules and approval settings with change audit                  | #1006 #1080 #455              |

The current review observed the full menu and the receiving/vendor entry forms below. Other menu entries retain their existing exploration evidence and require complete synthetic behavior/calculation acceptance in their owning stories; seeing a link does not prove its workflow works.

## Vendor registration fields observed

`VendorSetup.htm`: vendor name, alias, corporate group, vendor type; Vendor Details and Brand Mapping tabs. Address, registration date, contact person, primary/alternate phone, optional fax/landline, website, primary/alternate email, country, state, city and postal code. Registration/payment section includes GSTIN, PAN, registration information, licence number, two bank/branch/routing/payment-mode/account-reference groups, composition-scheme flag and default credit period in days. Payment-mode choices shown include cheque, RTGS/NEFT and money order/DD. Save, Save and Next, Verify & Approve, reset, file import/upload and status/search/list controls are present. The Brand Mapping tab was opened read-only: it shows separate item-name and manufacturer-name searches, selectable catalogue items, a mapped-item panel and a Save action. No item was selected and nothing was saved; lookup and persistence behavior remain unverified. Model this as an explicit, permission-scoped seller-to-catalogue relationship with audit history. Brand mapping does not establish a valid licence, product registration, service area, price or stock availability.

Modern implementation must use repeatable structured contacts/addresses/registrations/payment destinations rather than arbitrary fixed Bank1/Bank2 columns. India-specific GSTIN/PAN/IFSC/composition settings must not become another country's defaults; country-specific identifiers/routing/tax fields come from reviewed configuration. Restrict and mask payment destinations, encrypt where required, audit/approve changes and never treat stored bank text as verified bank ownership. A bank instruction is not an executed payment. Document upload/import reuses tenant-scoped quarantine/ClamAV, validation preview and idempotent source rows. Vendor status, legal verification, premises licence and trading authority are separate states.

## Purchase / receiving fields observed

`ProcurementTxn.htm?headerType=GRN&screenName=GRN`: vendor, accounting department, transaction date, purchase department, invoice date/number, GRN number, e-invoice reference, cash-purchase and donation-purchase flags, notes and receiving store. Source modes: Direct, Ordered, Challan and B2B Purchase.

Item columns: item, batch, expiry, quantity, case/pack, MRP per case, purchase rate per case, amount, line discount percentage/amount, vendor discount percentage/amount, taxes and net rate. A Tax Before Discount option is visible. A service section has service, description, amount, discount, taxes and net rate. Payment terms include percentage, duration and term; header breakup shows bill amount, discounts/vendor discounts and payable amount. Save / Save & Print / Verify & Approve / Transfer Stock and Attach Purchase Copy controls are visible.

Modern acceptance must preserve original units/conversions, source order/challan/receipt/invoice references, ordered/received/free/short/damaged quantities, supplier terms and effective tax/discount/rounding snapshots. Discount/tax ordering is an approved country policy, not a universal switch copied from legacy. Receiving, quality release, financial verification and stock transfer are separately authorized transitions. Services and operational expenses remain distinguishable from received drug cost layers. Repeat requests/imports cannot duplicate stock, liability, posting or a bank transfer.

The existing local payable register stores net-after-discount, explicit tax/recoverable tax/landed charges and receipt cost linkage. It does not claim that all observed PO/challan/e-invoice, service, staged term, statutory tax, vendor-master or bank verification workflows are finished.

## Independent review follow-up — 16 October 2026

Reopened the authenticated Procurement -> Setup -> Vendors form read-only. Save,
Save And Next, Verify & Approve, licence number, registration, country/location,
contact, tax and credit controls remain visible. No legacy approval, save or
customer-record mutation was performed. The new review workflow deliberately
separates saved declarations, scanned evidence, independent authority review and
subsequent trading eligibility. It records exact revisions, actor, reason and
authority-check reference rather than treating a generic Active label as proof.
Only synthetic TEST identifiers and reserved example addresses are used in tests.

## Distributor structure and access comparison — 17 October 2026

The authenticated Mednet session was also reopened on 17 October. Its module menu
again showed Fixed Asset, Pharmacy, Procurement and Store, with separate Finance,
Accounts and Access Control. Changing the Procurement selector label did not
leave the current EMR frame during that check; no detailed transaction workflow
is claimed as freshly exercised. The original EMR selection was restored, with
no saves, approvals, customer-record reads or copied customer values.

This comparison uses the recorded read-only observations above. It does not
assert that an unobserved legacy feature is absent. No real vendor, customer,
staff, bank or transaction values are used in development fixtures.

| Recorded legacy evidence                                                                                                                                                         | Modern design and acceptance boundary                                                                                                                                                                                                                                                                       | Owning story                  |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| Vendor Setup includes corporate group, vendor type and registration fields.                                                                                                      | Keep the owned parent-company relationship separate from legal registrations, commercial counterparties and tenant boundaries. Linking a company never grants access to its parent, subsidiary, hospital or another tenant.                                                                                 | #1137 #1135                   |
| Vendor Setup includes contact/address fields and two fixed bank-information groups.                                                                                              | Use structured contacts, country-aware addresses and tax identifiers. Payment destinations remain separately protected finance data; the operational warehouse, driver and sales roles do not acquire bank access.                                                                                          | #1005 #1135 #1142             |
| Country/state/city fields, receiving-store selection and inventory menus are visible.                                                                                            | Branch, warehouse/depot and bin identity are explicit. Operational grants apply to the exact owned location; country consistency and current authority are checked on the server. Territories and assigned representatives are modern requirements; their legacy screens and enforcement were not observed. | #1137 #1004 #1140             |
| Challan states Draft, Final, Partial and Completed were observed; receiving exposes Direct, Ordered, Challan and B2B source modes.                                               | A manual delivery task records bounded logistics planning and an assigned driver's progress. It is not a challan, goods receipt, purchase order, stock movement, proof of payment or invoice, and cannot complete those workflows implicitly.                                                               | #1137 #1002 #1139 #1140 #1141 |
| Save and Verify & Approve controls appear on vendor and receiving forms. Procurement settings include finance-review and automation-related controls in the earlier exploration. | Saving a declaration, changing access, approving evidence and authorising a commercial transaction remain different actions. Require current object authority, reasoned immutable history and independent support approval; do not infer the legacy approval matrix or permission rules from button labels. | #1137 #1006 #532 #1139        |

The legacy approval-setting values, approval transitions, sales-territory screens
and enforcement against direct requests were not exercised. Their owning stories
need synthetic end-to-end evidence before closure. Access controls and manual
logistics alone do not establish complete B2B procurement or SOC 2/HIPAA
certification. Commercial purchase-approval configuration remains with #532;
country regulatory policy remains separately reviewed rather than overridden by
an organisation's access settings.
