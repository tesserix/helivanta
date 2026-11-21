# PharmaConnect integration specification

[Contract index](../README.md) · descriptive current slices plus outstanding contracts

## What the domain covers

The `pharmacy` module is pharmacy management, not a hospital add-on. It runs a
standalone retail chemist with no hospital at all, and a hospital's own
pharmacy, which is the same outlet with an optional affiliation. Selling,
stock, registers, tax and payables work the same for both.

How a patient pays differs, and the module does not own that difference:

- **At the counter** (a retail chemist, or a hospital pharmacy's OTC counter),
  the pharmacy asks for the payment itself, with `purpose=counter_sale`
  ([payment requests](#payment-requests-1386-c-4a)).
- **For an inpatient,** medicines are charged to the hospital bill and paid at
  discharge. Hospital billing asks for that payment; the pharmacy reports what
  was dispensed against the encounter and does not collect it.

## Scope and authority

The hierarchy is tenant → pharmacy organization → outlet, with optional hospital affiliations. Independent pharmacies do not need a fake hospital. Route permission and a current explicit outlet grant are separate. An affiliation, even a verified one, is neither patient consent nor prescription/stock authority. See [ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md), [country/trading boundaries](../../adr/0026-pharmaconnect-country-and-trading-boundaries.md) and [existing API detail](../../architecture/pharmaconnect-api.md).

The [runtime reference](../reference/README.md#pharmacy) lists every implemented path. The outlet root is `/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`. Organization/outlet administration and the legacy `/medications`/`dispenses` surfaces are separate route families; do not equate the legacy dispense task with the outlet stock ledger.

## Current contract families

| Family                                        | Owning records / schema                                                                                                                                                          | Rules and consumer boundary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registry and grants                           | Pharmacy organizations, outlets, optional hospital affiliations, outlet access; `contracts/pharmacy.json`                                                                        | UUIDv7 path records; verified session tenant; country/timezone explicit; administrator access management does not imply operational grant; `GET …/outlets/{outlet}/access-grants` pages an outlet's grants (subject, level) under `pharmacy.access.manage` and, like grant and revoke, needs no grant at that outlet (#1718)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| Private catalogue                             | Product fields, ingredients, packs, unit of issue, MRP/currency, status/version                                                                                                  | Exact strengths/quantities and pack conversions; identifiers and lifecycle/version in catalogue events, not a global shared product table                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Batches and locations                         | Lot, manufacturing/expiry dates, storage/quarantine location and rack/shelf address                                                                                              | Dates interpreted in outlet timezone; a lot is not a clinical authorization to dispense                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Stock ledger                                  | Immutable movement, receipt/correction evidence, quantity, location and batch                                                                                                    | Quarantine receiving is not clinically usable stock; read current safety/expiry state before fulfillment                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Batch safety journal                          | Immutable concern records and context references                                                                                                                                 | Recording an unreviewed concern does not silently implement regulatory recall/release decisions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Stock register, reconciliation and CSV export | Scoped period/register rows, expiry view, auditable export                                                                                                                       | Preserve period identity and filters; report freshness and excluded/missing scope; no floating-point quantities or cross-outlet leakage                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Operational accounts                          | Purchase/expense payables, external settlement, credit/reversal references                                                                                                       | Immutable attributable entries, versioned policies, currency and exact minor units; recording external settlement is not charging a payment provider                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Tax setup (#1283)                             | Outlet tax registrations and product tax classifications, both append-only and effective-dated; `contracts/pharmacy.json`, scheme structure in `contracts/country-profiles.json` | The country profile describes how tax works (India: GSTIN, HSN, CGST+SGST/UTGST or IGST) but carries no rate; the hospital records each product's rate. `pharmacy.tax.manage` (owner) only. Nothing in force before the outlet's today; a same-date correction needs a reason. Both are recorded by `PUT …/outlets/{outlet}/tax-registrations/{uuidv7}` and `PUT …/catalogue/products/{id}/tax-classifications/{uuidv7}`, the id chosen by the client: an identical resend by the same person returns the recorded row (200) without re-checking its date, and any other use of the id, including another outlet or product, is `409 conflict` with nothing written (#1715). A country with no scheme is `409 tax_scheme_unavailable`. `GET …/tax-registrations/in-force` answers the registration in force today; products carry their classification in force. Not covered: sales, GSTN verification, approval of classifications, composition scheme, cess, e-invoicing |
| Overview (#576)                               | Counts only, owned by the lists they count                                                                                                                                       | `GET …/outlets/{outlet}/overview` (`pharmacy.catalogue.read`) returns, for the outlet's date: stock lines expired / within 30 / within 90 days (`pharmacy.stock.read`), safety concerns recorded in the last 30 days (`pharmacy.stock.read`), bills with an amount outstanding overdue / due within 7 days (`pharmacy.accounts.read`), unclassified products, and whether a tax registration is in force (`pharmacy.tax.manage`). Each count uses the same scope as its list: `?due=overdue                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | week`on payables,`?recorded=30d`on safety incidents (outlet-wide; otherwise`batch_id`is required), the stock register's`expiry`/`balance`, the catalogue's `tax=unclassified`. A section the caller may not see is absent, never zero. Waiting patients come from the counted `/dispenses/waiting`. Not covered: verification queue, fulfilment blockers, reorder levels, Schedule H1/X register, recalls |
| Paged outlet lists (#576)                     | Counted pages over the same filters as the uncounted lists                                                                                                                       | Counted (`page.total`), newest first, each guarded by the outlet grant inside its snapshot: `GET …/accounts/payables/counted` (`?due=overdue\|week`), `…/accounts/inventory-audit/counted` (`pharmacy.accounts.read`); `…/stock/safety-incidents/recent` (last 30 days) (`pharmacy.stock.read`); `…/catalogue/products/counted` (`q`, `status`, `tax=unclassified`, which is a 400 where the outlet has no tax scheme; `pharmacy.catalogue.read`). The uncounted routes are unchanged.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Legacy dispense queue                         | Retired visit-linked task (#1680); history only                                                                                                                                  | Retired: `POST /v1/pharmacy/dispenses/{id}/dispense` answers `410 dispense_retired` after the session/tenant checks and writes nothing, because it recorded a hand-over with no product, stock, schedule, prescription or register check. Dispense to an OPD patient with a counter sale citing their encounter. New visits no longer open rows. History stays readable as recorded: `GET /v1/pharmacy/dispenses/waiting` (oldest first, `?q=` patient-name search) and `/dispensed` (newest by `dispensed_at`), both counted (#1295); `GET /dispenses?status=` is unchanged. Pending rows were never completed and stay pending.                                                                                                                                                                                                                                                                                                                                          |

Canonical request fields and constraints live in [contracts/pharmacy.json](../../../contracts/pharmacy.json), generated `input_types.go` and `pharmacy-contract.ts`. Queries and response projections live in their owning handlers. Existing [API documentation](../../architecture/pharmaconnect-api.md), [stock-register delivery](../../delivery/2026-10-17-pharmacy-inventory.md), [batch-safety acceptance](../../delivery/2026-10-18-batch-safety-acceptance.md) and [stock export](../../delivery/2026-10-18-pharmacy-stock-export.md) provide detailed slice evidence.

Use client resource IDs and exact `expected_version` semantics of each handler; supplying a valid-looking UUID does not make every PUT replaceable or every POST retry-safe. The optional expected-tenant header returns `409 session_changed` if session context drifted. Never retry an in-flight action under a newly selected tenant.

### Outlet drug licences (#1386 A1)

An outlet may sell only while it holds an approved licence that is valid on the outlet's own date ([counter sale design](../../superpowers/specs/2026-10-27-counter-sale-design.md)).

| Route                                      | Behaviour                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/outlets/{outlet}/licences/{uuidv7}` | Records a licence. Requires `pharmacy.licence.manage` and the outlet editor grant. Fields: `licence_kind retail\|wholesale`, `form_label` as printed, number, issuing authority, `valid_from`/`valid_until`, and the pharmacist in charge with their registration. Idempotent; reusing an id with different details is 409 |
| `POST …/licences/{id}/review`              | Records `{decision: approved\|rejected, reason}`. Requires `pharmacy.licence.review`. The reviewer must not be the person who entered the licence: the database refuses it, and the API returns 409 `four_eyes`. A licence gets one decision; correct a mistake by entering a new licence                                  |
| `GET …/licences`                           | Cursor page of licences with their decision                                                                                                                                                                                                                                                                                |
| `GET …/licence-status`                     | `{in_force, today, licence, reason}`, judged on the outlet's date                                                                                                                                                                                                                                                          |

Later slices call `pharmacy_outlet_licence_in_force(tenant, outlet, day)` inside their own transactions. Private evidence and independent review behavior are described below.

### Declared schedule class (#1386 A2)

Until the drug master is loaded, each product's Drugs Rules class (`none|G|H|H1|X`) and NDPS flag are declared by the outlet, together with their source. A second person must approve the declaration.

| Route                                             | Behaviour                                                                                                                                                                                                                                    |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/catalogue/products/{id}/schedule/{uuidv7}` | Declares `{regulatory_class, ndps, source}`. Requires `pharmacy.schedule.declare`. Idempotent. The database records `lowers` when the declaration is less strict than the class last approved                                                |
| `POST …/schedule/{declaration}/review`            | Records `{decision, reason}`. Requires `pharmacy.schedule.review`. The reviewer must not be the declarer (database-enforced; 409 `four_eyes`). Approving a lower class answers 409 `platform_review_required`; rejecting one is allowed      |
| `POST …/schedule/{declaration}/countersign`       | Records `{reason}` for a lowering. Requires `pharmacy.schedule.countersign` (pharmacy owner). Not by the declarer (409 `four_eyes`), only for a lowering (409 `not_a_lowering`), not once decided. Idempotent for the same person and reason |
| `GET …/catalogue/products/{id}/schedule`          | Returns `{in_force, origin: declared_by_outlet, reason, history}`. History items carry `lowers`, the countersign and `decided_by_platform`                                                                                                   |

Only the **latest** declaration counts, and only once it is approved. If the latest one is still awaiting review or was rejected, the product has no class, so an older approval cannot bypass a stricter reclassification. Later slices call `pharmacy_product_schedule_in_force(tenant, outlet, product)`.

**Lowering a class (#1681).** Inside the tenant a class can only be made stricter. Strictness is `none < G < H < H1 < X`, and dropping NDPS is lower even when the class rises. A declaration less strict than the approved class lowers it, and no tenant member or pair can approve it: the review trigger refuses it (`pharmacy_schedule_platform_review`), including a declaration filed before the stricter one was approved. The pharmacy owner countersigns the request, then Helivanta decides it with `cmd/pharmacy-schedule-review list | decide -declaration ID -by NAME (-approve|-reject) -reason TEXT`, connected through `SCHEDULE_REVIEW_DATABASE_URL` as `helivanta_drug_approver`. The database refuses that role's decision on anything but a countersigned lowering, or by its declarer or countersigner, and marks it `by_platform` from the connecting role. The role reads only the schedule rows and the name of a product with a lowering. While a lowering waits or after it is rejected, the stricter class stays in force; a waiting raise still blocks the product. A product's first declaration has nothing to compare with and is still decided inside the tenant until the drug master link (#1683). Production must provision `helivanta_drug_approver` before the command can be used there.

In the shell, the open catalogue product's **Schedule class** section declares and reviews it (#1636). It is offered to an outlet editor holding the matching grant. Class and NDPS start empty and must both be chosen. A retried declaration reuses its id. Only the newest pending declaration is offered for review, and a lost race rereads the state. A lowering is never offered for outlet review: the section says it waits for Helivanta, offers the owner a countersign, and shows Helivanta's decision in the history.

### Item categories (#988 slice 1)

An outlet's item categories and subcategories, and each product's category. See the [design](../../superpowers/specs/2026-11-04-pharmacy-item-categories-design.md).

| Route                                                 | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/catalogue/categories/{uuidv7}`                 | Takes `{code, name, parent_id?}`. Creates a category, or a subcategory of a top-level category; the same id with a new name renames it. A code is capitals, digits, `-` or `_` (up to 20), unique among its siblings. Code and level are fixed (`409`); a third level or an unknown parent is `400`; a retired category takes no new subcategory (`409`). Requires `pharmacy.category.declare` with the outlet `editor` grant |
| `POST …/catalogue/categories/{id}/retire`             | Retires it, once; it is never deleted. A retired category keeps the products already in it and can take no new one. Same permission                                                                                                                                                                                                                                                                                           |
| `GET …/catalogue/categories`                          | Paged, oldest first, retired ones included. Requires `pharmacy.catalogue.read`                                                                                                                                                                                                                                                                                                                                                |
| `PUT …/catalogue/products/{id}/category/{uuidv7}`     | Declares the product's category: `{category_id, subcategory_id?, effective_from, reason}`. The category is a live top-level one and the subcategory a live one of it (database-enforced, `409`); `effective_from` is after the outlet's today, tomorrow at the earliest (`400`); the reason is 5 to 300 characters. A replay is idempotent; the same id with other details is `409`. Requires `pharmacy.category.declare`     |
| `POST …/catalogue/products/{id}/category/{id}/review` | Approves or rejects it with a reason, once. The reviewer is never the declarer (`409 four_eyes`); only the product's newest declaration can be approved (`409`, superseded); a category or subcategory retired since cannot be approved (`409`). All database-enforced; a rejection is always allowed. Requires `pharmacy.category.review`                                                                                    |
| `GET …/catalogue/products/{id}/category`              | `{in_force, history}`: the category in force today and the 50 newest declarations with their reviews. Requires `pharmacy.catalogue.read`                                                                                                                                                                                                                                                                                      |

- **Historically stable.** A category takes effect from tomorrow at the earliest, and an approval is in force from the later of `effective_from` and the outlet's next day (`in_force_from`): today's sales are already made, so no approval re-files a sale, the approval day's own included (tested). On any day the category in force is the approved declaration with the latest `in_force_from` up to that day (`pharmacy_product_category_on`), the later approval winning a tie.
- **Who.** `pharmacy.category.declare` and `pharmacy.category.review` are held by the pharmacist and the pharmacy owner, as schedule classes are. Declarations, reviews and categories are audited and insert-only, except a category's name and its retirement.
- **Retirement** is checked with the category row locked (`FOR SHARE`), at declaration and at approval, so a product never lands in a category retired under it (tested concurrently).
- **Screen.** The catalogue page lists the categories and subcategories, with adding and retiring (confirmed first) for those who may declare; renaming is by the API only; a product's pane shows its category in force, its history, a declaration form defaulting to tomorrow and, for a reviewer, the newest declaration if it awaits review; an older pending one is shown as superseded.
- **Not covered:** margin discount rules and their precedence, patient categories, pharmacy templates and indent groupings (later #988 slices); categories shared across outlets; bulk import. Migration `0033_pharmacy`.

### NPPA ceiling prices (#1371)

Under the Drugs (Prices Control) Order, 2013, the NPPA notifies a ceiling price per unit of dosage, excluding taxes, for scheduled formulations; no one sells to a consumer above the lower of the label and the price list (para 26). Until the drug master carries ceilings, the outlet declares each product's price control with its NPPA order, and a second person approves it ([spec](../../superpowers/specs/2026-11-03-nppa-ceiling-price-design.md)).

| Route                                                  | Behaviour                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/catalogue/products/{id}/price-control/{uuidv7}` | Declares `{price_control: controlled, ceiling_minor, order_reference, order_date, effective_from, source}`, or `{price_control: not_controlled, effective_from, source}`. `ceiling_minor` is paise per unit of issue, before GST. Requires `pharmacy.price_control.declare`. Idempotent |
| `POST …/price-control/{declaration}/review`            | Records `{decision, reason}`. Requires `pharmacy.price_control.review`. The reviewer must not be the declarer (database-enforced; 409 `four_eyes`)                                                                                                                                      |
| `GET …/catalogue/products/{id}/price-control`          | Returns `{state: undeclared\|blocked\|in_force, today, in_force, reason, history}`                                                                                                                                                                                                      |

- **In force on a day:** none if the product has no declaration (`undeclared`: sold at its MRP alone). Otherwise the latest declaration must be approved, and the approved declaration with the latest start on or before the day is in force; if not, the product is `blocked` and the counter refuses it (409 `not_sellable`), so a pending correction cannot be bypassed. A declaration replaces every earlier one starting on or after its own start, so a correction is never overridden by an older entry dated after it. Only the latest declaration can be reviewed (409 otherwise). A price-control change that lands while a sale is being posted refuses it with 409 `not_sellable`.
- **At the counter:** a controlled line's list amount is the most whose value before GST, as the counter splits it, is within `ceiling_minor × units`, if that is below the MRP share. The line carries `price_declaration_id`, `ceiling_minor` and `price_limit` (`mrp` or `ceiling`); a discount runs on that list amount.
- **Database guarantees:** a sale-line trigger refuses a blocked product (`pharmacy_sale_line_price_unknown`), a line naming another declaration or ceiling than the one in force on the sale date (`pharmacy_sale_line_price_source`), and a value before GST above the ceiling (`pharmacy_sale_line_ceiling`). Declarations and reviews are append-only and audited.
- **Activation:** before an outlet relies on a ceiling, its responsible pharmacist and compliance reviewer confirm the reading of the order, that the catalogue's unit of issue is the order's unit of dosage, and whether old-label stock may be sold during the manufacturer's 45 days (it may not, until they decide).
- **Not covered:** importing NPPA notifications; ceilings in the drug master; retail prices of new drugs; refunds of past overcharges.

### Service charges (#1388)

A delivery or handling charge on a medicine bill is its own invoice line, never part of a medicine's price, discount, MRP or NPPA check. Each outlet configures charges in versions that a second person approves; how a charge is taxed is the tenant accountant's recorded decision ([spec](../../superpowers/specs/2026-11-03-pharmacy-service-charges-design.md)).

| Route                                                       | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/service-charges/{charge}/versions/{uuidv7}`          | Declares `{code, name, channel: counter\|ward_delivery, amount_minor, status: available\|withdrawn, tax_treatment, sac_code?, supply_nature?, rate_ppm?, exemption_basis?, tax_basis, effective_from}`. The first version creates the charge with its code (unique in the outlet, never changed); later versions repeat it. `own_service` needs its SAC, supply nature and rate; `principal_supply` (taxed as the medicines, CGST Act s.8(a)) carries none. Amount is tax inclusive. Requires `pharmacy.service_charge.declare`. Idempotent; at most 50 charges an outlet, never deleted |
| `POST …/service-charges/{charge}/versions/{version}/review` | Records `{decision, reason}`. Requires `pharmacy.service_charge.review`. Not by the declarer (409 `four_eyes`), only the latest version, once                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `GET …/service-charges`                                     | Every charge with `in_force` (the version billed today, or null) and `latest`. Requires `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `GET …/service-charges/{charge}`                            | One charge with its last 50 versions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

- **In force:** the approved version with the latest `effective_from` on or before the day, not replaced by a newer approved one starting on or before its own. While the latest version awaits review or was rejected, nothing is billed: a withdrawal stops the charge at once.
- **At the counter:** `POST …/sales/quote` takes `charges: [{charge_id}]` and `PUT …/sales/{id}` takes `charges: [{charge_id, version_id}]`, the version the quote priced (at most 5, distinct; a different version in force by then is 409 `not_sellable`); a quote names `channel` (default `counter`), a sale billed to an admission is `ward_delivery`. Each becomes a charge line copying the version, its tax inside the amount as for a medicine line. A `principal_supply` charge takes the medicines' supply nature and rate and is refused (409 `not_sellable`) when they differ. Totals and tenders include charges; a sale still needs a medicine line. Sales return `charges` with each line's `credited_minor`. `SalePosted` and `AdmissionChargePosted` keep their shapes; their totals include charges.
- **Credit notes:** `PUT …/credit-notes/{id}` takes `charges: [{sale_charge}]` beside `lines`, at least one of either. A charge is credited whole, once (409 `not_returnable`), with no stock movement; `CreditNoteIssued.lines` is then empty.
- **Daily report:** each day adds `service_charges_minor`, `service_charges_taxable_minor`, `service_charge_credits_minor` and `service_charge_credits_taxable_minor`; sales and credits stay invoice totals.
- **Database guarantees:** a charge line must be the version in force on the sale date, available, for the sale's channel, with every value copied (`pharmacy_sale_charge_source`); its tax is the counter's split (`pharmacy_sale_charge_tax`); at commit a principal-supply charge matches every medicine line's rate (`pharmacy_sale_charge_principal`) and the sale balance includes charges. A credited charge equals its sale's charge line (`pharmacy_credit_note_charge_source`) and is credited once (`pharmacy_credit_note_charge_once`).
- **Activation:** the accountant decides each charge's treatment and records its basis. Whether a separate delivery fee on a medicine sold at MRP is allowed at all is open for compliance review.
- **Not covered:** home delivery (#161); percentage, slab or automatic charges; splitting a principal-supply charge across medicine rates; GSTR exports.

### Sellable stock (#1386 A3)

Every location is either `quarantine` or `sellable`, fixed when it is created. Receipts and corrections only ever land in quarantine. Stock reaches a sellable location only through a release.

| Route                           | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/stock/batches/{id}`      | Each new batch now carries the `mrp_minor` printed on the lot (exact paise, at least 1) and an optional `purchase_cost_minor`. A batch's MRP never changes. Batches received earlier have no MRP and can never be released                                                                                                                                                                                                                                                                                                                                                                                              |
| `PUT …/stock/locations/{id}`    | Accepts an optional `stock_status: quarantine\|sellable`. The default is `quarantine`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `PUT …/stock/releases/{uuidv7}` | Moves `{batch_id, from_location_id, to_location_id, quantity_units, reason}`. Requires `pharmacy.stock.release`. It writes a negative movement from quarantine and a positive one into sellable, in one transaction, under the overdraft guard. It returns 409 `not_releasable` with the reason unless every check passes: the move is quarantine to sellable, the batch expires after the outlet's today, it has an MRP, it has no safety incident, the product is active and tax-classified, and the product has an approved schedule class other than X or NDPS. The release records the class it was released under |
| `GET …/stock/balance`           | Reports the location's real `stock_status` and `dispensable_units`: what a sale could take from this location and batch now, by the sale's own predicate `pharmacy_sellable_buckets` (sellable location, MRP, unexpired at the outlet, no safety incident, less supplier holds), else 0. Product and outlet gates (status, class, tax, NPPA, licence, GSTIN) are not applied; `GET …/sales/availability` and the sale apply them (#1708)                                                                                                                                                                                |

### Receiving under a reviewed operating duty (#19 slice 3b-ii)

A `pharmacy.inventory_receiver.v1` duty (ADR-0064) lets one account receive stock at one exact outlet of an independent or chain pharmacy. It adds no tenant role and no outlet `editor` grant. The routes sit under `/v1/pharmacy/pharmacies/{pharmacy}/operating-locations/{outlet}/stock` and are public at the router; each request is admitted only by IAM's `WithOperatingScope`. Full contract: [spec](../../superpowers/specs/2026-11-08-pharmacy-receiver-duty.md).

| Route                                                                             | Behaviour                                                                                                                                                      |
| --------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/products`                                                                  | Active catalogue identity needed to name a lot. It omits MRP, schedule, tax and price controls                                                                 |
| `GET …/locations`, `…/batches`, `…/batches/{id}`, `…/receipts`, `…/receipts/{id}` | This outlet's locations, lots and receipt history only                                                                                                         |
| `PUT …/batches/{uuidv7}`                                                          | Records or replays a lot with its printed MRP. An identical replay answers 200 with the stored lot; a different one is 409                                     |
| `PUT …/receipts/{uuidv7}`                                                         | Records or replays a `receipt` movement into a quarantine location. Corrections and releases are refused, and stock still needs an editor's release to be sold |

Every refusal of scope or duty is 404, never 403. A revoked credential is 401 `session_changed`, and a mismatched `X-Helivanta-Expected-Subject` is 409 `session_changed`. Unavailable authority is 503 `authorization_unavailable`. If the duty evidence row cannot be written, the answer is 503 `audit_unavailable` and nothing is saved. Every read and write appends a `pharmacy_operating_stock_audit` row with the duty, snapshot version and authority epoch. The shell panel is a deep link at `/pharmacy/pharmacies/{pharmacy}/operating-locations/{outlet}/receiving`; there is no receiver self-discovery yet.

### Counter sale (#1386 B-1)

An OTC sale at the counter: one command takes sellable stock earliest expiry first, prices it at the batch MRP, computes GST inside that price, and posts an invoice numbered in the outlet's gap-free series for the Indian financial year.

| Route                                  | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/invoice-profile/{uuidv7}`       | Records `{legal_name, address, invoice_prefix}`, the seller details an invoice prints. The prefix is 1 to 4 capital letters or digits. Requires `pharmacy.tax.manage`. The latest profile is in force; invoices keep a snapshot                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `GET …/invoice-profile`                | Returns `{profile}`, or `null` when none is recorded. Requires `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `GET …/sales/availability?product_id=` | Returns `{sellable_units, nearest_expiry}`, using the same predicate as allocation, `pharmacy_sellable_buckets`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `POST …/sales/quote`                   | Prices `{lines}` exactly as a sale would, running the same checks, and returns `{currency, total_minor, taxable_minor, cgst_minor, sgst_minor, lines}` without posting anything. Requires `pharmacy.sale.create`. If stock moves before posting, the sale refuses tenders that no longer match                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `PUT …/sales/{uuidv7}`                 | Posts `{lines: [{product_id, quantity_units}], tenders: [{kind: cash\|upi\|card\|upi_verified, amount_minor, tendered_minor?, reference?, payment_request_id?}]}`. A `upi_verified` tender names a counter payment that is `paid`, of this outlet, for exactly the tender's amount and the sale's currency, and on no other sale (enforced by a foreign key, a unique index and an insert trigger); otherwise 409 `not_sellable`. `upi` stays the reference-only tender. Requires `pharmacy.sale.create` and the outlet `editor` grant. Returns 201 with the invoice, or 200 for a replay of the same intent; a different intent is 409. Every refusal is 409 `not_sellable` with its reason, and nothing is kept |
| `GET …/sales/{id}`, `GET …/sales`      | Return the invoice (seller snapshot, lines, tenders) and the paged list. Require `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |

**What a sale requires.** The outlet must have:

- a reviewed **retail** drug licence valid today (wholesale does not count);
- a GSTIN in force, registered in a state. A union territory, which levies UTGST, is refused
  until that tax head is modelled;
- an invoice profile.

Each product must:

- be active;
- be priced in the outlet's currency;
- be tax-classified on the sale date;
- have an approved class `none` that is not NDPS.

Schedule G, H and H1 are sold only against a recorded prescription line (B-2b, below). They are never sold as OTC. X and NDPS are refused until their controls exist.

**Allocation and stock.**

- A sale draws from sellable buckets whose batch has an MRP, expires after the outlet's date, and has no safety incident.
- Buckets are used earliest expiry first. Each batch used becomes its own invoice line, up to 200 lines.
- Short stock refuses the sale and names each short product.
- Bucket locks are taken in one global order before balances are read, so two tills cannot sell the same last unit.

**Pricing and GST.** Per line:

- `amount = floor(mrp × units ÷ pack)`, so a line never exceeds MRP. The database checks it.
- CGST and SGST are equal halves of the tax inside the amount, rounded half up.
- The taxable value is the remainder.
- The place of supply is the outlet's GSTIN state.
- The total is the exact sum of the lines, with no round-off.
- For a product with an NPPA ceiling in force, the list amount is the lower of that and the ceiling plus GST (NPPA ceiling prices, below). `nppa_ceiling_checked` is true only when every line's product had a price-control declaration in force.

**Tenders.**

- At most one cash tender, carrying the amount handed over; change is computed from it.
- UPI and card tenders carry a reference.
- The tenders must sum to the total.

**Database guarantees.**

- The invoice number is `PREFIX/FY/N`, at most 16 characters. The series only steps by one, inside the sale's transaction.
- Invoices, lines and tenders are immutable, and nothing can be added to a committed sale.
- At commit, a deferred check proves the sale equals its lines, is paid by its tenders, and has one stock movement per line.
- A `sale` movement must take exactly its line's units off a sellable location.
- Each line is checked against its source: the batch is the product's and sellable on the
  sale date, and its lot, expiry and MRP are the batch's. The class is the approved
  declaration in force, OTC and not NDPS. So the never-above-MRP check compares against
  the batch's own MRP.
- A line that prices to nothing (a few units of a cheap pack) refuses the sale.
- On the counter screen, a failure other than a refusal keeps the sale's id, so a retry
  replays it and never posts a second invoice.

**Events and scope.**

- `sale_posted.v1` carries ids, the invoice number, currency, total, and each line's product, batch and units. It carries no buyer or prescription data.
- `sale_posted.v1` carries no buyer or prescription data. Prescription records and their scanned pages are B-2a; selling against them and walk-in buyers are B-2b; registered patients are B-2c; discounts are B-3 (below).

### Payment requests (#1386 C-4a)

The pharmacy asks the [payments core](payments.md) to collect money and
records what it is told; it never reads a payments table. `purpose` says
what the money is for — `counter_sale` today — so later pharmacy flows add a
purpose rather than a new event.

| Route                                | Behaviour                                                                                                                                                                                                                                                                       |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/counter-payments/:id`         | Outlet editor with `pharmacy.sale.create`. Body `{amount_minor}`; currency from the outlet's country. Creates the request (`201`) and publishes `payment_requested.v1` in the same transaction; the same id with the same amount returns it (`200`), with another amount `409`. |
| `GET …/counter-payments/:id`         | The request's state for the counter: `requested`, `awaiting_payment` (with the QR image URL and expiry), `cancelling`, `paid` (provider payment id, RRN, `late`), `failed` (reason), `expired` or `cancelled`.                                                                  |
| `POST …/counter-payments/:id/cancel` | Only from `requested` or `awaiting_payment` (`409` otherwise). Moves to `cancelling` and publishes `payment_cancelled.v1`; payments answers cancelled, or paid if the money landed first.                                                                                       |

Outcome events move a request with status-guarded updates: a redelivered or
reordered event changes nothing it should not, `paid` is never undone, and a
payment reported after expiry or cancel makes the request `paid` with
`late=true` rather than going unrecorded. A paid report whose amount or
currency differs from the request is ignored. Outcome consumers hold no
outlet grant; each sees and writes only the request its event names
(`app.pharmacy_payment_outcome`).

`GET …/counter-payments` (a page, `pharmacy.sale.create`) lists the outlet's paid requests on no sale yet, newest first, so money taken for a sale that then failed to post can still be put on a sale of the same amount (#1386 C-4b).

### Unused verified-UPI recovery (#1459)

An open or selected paid counter payment holds the customer, tender and basket
controls. Finish the sale, or record the full refund already completed through
the provider dashboard. Recording a refund here never sends money or asserts
provider verification. Automatic provider refunds remain outside this slice.

- `PUT …/counter-payments/{id}/refund` requires `pharmacy.sale.return` and an
  outlet editor grant. The body is `{reference, reason}` from canonical
  `UnusedPaymentRefundFields`. The server derives the whole amount and currency
  from the original paid request. The immutable receipt includes the request id,
  amount, currency, reference, reason, recorder and time. First recording is 201;
  an identical retry is 200; changed content, an unpaid request or an already-used
  payment is 409 `payment_not_unused`. Missing/out-of-scope requests are 404.
- `GET …/counter-payments/{id}/refund` requires the same route permission and a
  current outlet grant; it returns the retained receipt, or 404 when absent.
- The paid request stays paid. Refunded requests are excluded from unused-money
  recovery and refused as sale tenders. Both database insert guards lock the
  original request before reading the competing ledger, so a sale and refund
  recording cannot both consume it. No stock or revenue posting is invented.
- Recovery lists retain bounded paging and permit recording refunds for other
  amounts, so a changed basket does not hide the original unused payment.

Migration `0028_pharmacy`. The same transaction publishes
`unused_payment_refund_recorded.v1` with outlet/payment ids, amount and currency
only; it is a record of external action, not provider confirmation. No new consumer. Existing captured-payment
contracts and historical sales are unchanged. This is an attributable record of
an externally completed refund, not a provider refund command or legal policy.

### The Schedule H1 register (#1386 C-2, #1062)

Drugs Rules 1945, Rule 65(3)(h): an H1 supply is recorded in a separate register at the time of supply. The entry gives the prescriber's name and address, the patient's name, the drug and the quantity. The register is kept for three years and is open for inspection.

| Route                                 | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/registers/h1?from=&to=`        | The paged register, newest first, for a period of outlet dates of at most 366 days. Each entry has the supply date, invoice and line, the prescriber's name, registration and address, the patient's name and address, the prescription date, the drug (SKU, brand and generic name), batch and expiry, units supplied, units since returned on credit notes, and the selling pharmacist. Requires `pharmacy.prescription.read` (pharmacist) |
| `GET …/registers/h1/export?from=&to=` | The same entries as a CSV, oldest first. Formula-like cells are neutralised. It is refused above 1000 entries rather than truncated, and returns its row count and SHA-256                                                                                                                                                                                                                                                                   |

- **A read, never a copy.** Each entry is an H1 sale line, written in its sale's transaction and never changed. A return is shown against its supply, never by removing it. Nothing in the register can be edited.
- **Kept.** Sales, prescriptions and credit notes are insert-only and never purged. A future retention job must keep H1 entries for at least three years.
- **Audited.** Every page read writes an `audit_events` row (`register_read`) naming the period. Every export writes one (`register_export`) and logs the row count and digest. The pharmacy owner cannot read the register: it shows patient and prescriber details.
- **Not covered.** Schedule X and NDPS registers (#165, #171), and printing as a bound register.

### Prescription register (#1062)

Drugs Rules 1945, Rule 65(3)(1): every supply of a drug, other than Schedule X, on a prescription is recorded at the time of supply in a prescription register. The entry's serial number is written on the prescription. Each entry gives the serial, the date, the prescriber's and the patient's name and address, the drug and quantity, and for Schedule C, H and H1 the manufacturer, batch and expiry, and the pharmacist ([spec](../../superpowers/specs/2026-11-03-prescription-register-design.md)).

| Route                                                                    | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET …/registers/prescriptions?from=&to=&class=&product_id=&patient_id=` | The paged register, newest first, of every sale line citing a prescription (Schedule G, H and H1), for a period of at most 366 outlet days. Optional filters: `class` (`G`, `H`, `H1`), a product and a registered patient. Each entry has the serial (`invoice · line N`), date, class, the prescriber's name, registration and address, the patient's name, address and registered id, the drug, its manufacturer, batch and expiry, units supplied and since returned, and the pharmacist. An address the prescription did not record (allowed for Schedule G) is `null`. Requires `pharmacy.prescription.read` |
| `GET …/registers/prescriptions/export?…`                                 | The same entries as a CSV, oldest first; a missing address reads `not recorded`. Refused above 1000 entries rather than truncated; returns its row count and SHA-256                                                                                                                                                                                                                                                                                                                                                                                                                                               |

- **A read, never a copy,** like the H1 register: nothing is stored for it, so it cannot drift. The serial comes from the gap-free invoice series, and the counter's endorsement note names it.
- **Audited.** Every read and export writes an `audit_events` row (`register_read`, `register_export`, resource `pharmacy_prescription_register`) in its own transaction, with the filters in the structured audit log.
- **Activation:** the outlet's pharmacist and compliance reviewer confirm the outlet keeps the register (or has opted for cash memos with the Licensing Authority), that invoice-and-line is an acceptable serial, and how Schedule G supplies without addresses are treated.
- **Not covered:** Schedule X and NDPS registers (#165, #171); Schedule C; searching by patient name; printing as a bound register.

### The daily sales report (#1386 C-3, #1060)

| Route                           | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET …/reports/daily?from=&to=` | One row per outlet date and currency with a sale or credit note, oldest first, for a period of at most 366 days. Each row has the sales' count, total, taxable value, CGST, SGST, discount and units; the credit notes' count, total, taxable value, CGST, SGST and units; the net; tenders and refunds by kind, with verified UPI (`upi_verified_minor`) apart from reference-only UPI. Requires `pharmacy.sale.read` (pharmacist, owner) |

- **A read of posted records.** Sales and credit notes are insert-only, so the report is summed afresh and cannot drift. A credit note counts on its own date, not its sale's.
- **Never across currencies.** A row is one day and one currency.
- **Cash is what was applied to the sale.** Change handed back is not income.
- **No row for a quiet day.** A day with no sale or credit note is absent, not shown as zeros.
- **Not audited.** It holds totals only, with no patient, prescriber or buyer detail.
- **Not covered.** Shift or till close, purchase cost and margin, and patient-wise reports. Sales by item and each receipt and refund are the reads below; the report downloads as CSV (see [CSV downloads](#csv-downloads-of-the-reports-1060)).

### Sales by item, and the receipts and refunds register (#1060)

| Route                                                                                                                             | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| --------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET …/reports/items?from=&to=&product_id=&category_id=&subcategory_id=&lot_number=&prescriber_registration=&prescriber_council=` | Paged, oldest product first: one row per product with a sale line or a credit-note line in the period (at most 366 outlet days). Units, amount charged after discount, discount, taxable value, CGST, SGST and the lowest and highest MRP per pack sold; units and amounts returned; net units and amount. `product_id` narrows to one product; `category_id` and `subcategory_id` to an item category, `lot_number` to one lot and the prescriber pair to one prescriber (see below). Requires `pharmacy.sale.read`, and `pharmacy.prescription.read` with a prescriber |
| `GET …/reports/receipts?from=&to=&kind=`                                                                                          | Paged, oldest first: every tender on a sale (`receipt`) and refund on a credit note (`refund`) with its date, document number, kind, payment reference and amount. `kind` is `cash`, `upi`, `upi_verified`, `card` or `admission_account`. Requires `pharmacy.sale.read`                                                                                                                                                                                                                                                                                                 |

- **Reads of posted records,** like the daily report: each document counts on its own outlet date, a return on the credit note's.
- **They reconcile.** Items plus the daily report's service charges equal its sales and credits, including credited charges; receipts per day and kind equal its tenders and refunds. The tests prove this for cash, UPI and card receipts and cash refunds; verified-UPI and admission-account rows use the same joins but are not separately exercised.
- **One row per product never mixes currencies or packs:** a stocked product cannot change either.
- **Admission-account rows** settle the document but are not collected or paid out; the UI says so.
- **Not audited**, except sales by item filtered by prescriber (see below): no patient, prescriber or buyer appears.
- **Not covered:** average purchase cost and margin, wholesale, indents, UPI payments captured but never applied to a sale and their manual refunds (#1478). Both download as CSV (below). Patient-wise sales and the sales stock report follow.

### Patient-wise sales and the sales stock report (#1060)

| Route                                                              | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/reports/patient-sales?patient_id=&from=&to=`                | Paged, oldest first: every line of a sale to that registered patient dated in the period (at most 366 outlet days): date, invoice and line, the admission's bed code when billed to an admission, product, batch lot and expiry, units, MRP, discount, amount, and units returned on credit notes dated up to `to`. `patient_id` is required (400 without it). Requires `pharmacy.sale.read`. Every page is audited |
| `GET …/reports/stock-sales?from=&to=&category_id=&subcategory_id=` | Paged, oldest product first: every product that has ever held stock, with units sold and returned in the period (by sale and credit-note date, as sales by item counts them) units on hand now, and units a sale may take today. `category_id` and `subcategory_id` narrow to an item category (see below). Requires `pharmacy.stock.read`                                                                          |

- **One patient at a time.** The pharmacy holds only the patient's id, so the screen finds the patient through the audited MRN lookup first and reads nothing before. That lookup needs `patient.pharmacy-lookup`, which only pharmacists hold; the screen tells anyone else so. Walk-in buyers are not patients and are not listed.
- **Audited in the read.** Each patient-wise page writes an `audit_events` row (`pharmacy_patient_sales`, `report_read`, with the period and patient id) in the read's transaction, so no data leaves without its record.
- **Stock is now, whatever the period.** On hand is every movement of the product. Sellable uses the counter's own rule (`pharmacy_sellable_buckets`): a sellable shelf, an MRP, unexpired on the outlet's date, and no safety incident. So a recalled or expired batch on the shelf is on hand but not sellable, as are returns in quarantine. The panel shows only to staff holding `pharmacy.stock.read`.
- **Not covered:** service charges on a patient's sale (medicine lines only; the daily report has the charges); purchase amount (442) and the last vendor (755), since purchase cost waits for approved formulas and a receipt records no supplier; a list across patients; stock as of a past date (the stock register has it); batch-level stock in this report. Both download as CSV (below).

### Sales by item by lot and by prescriber (#1060)

[Design](../../superpowers/specs/2026-11-04-pharmacy-lot-prescriber-report-filters-design.md).

| Route                                 | Behaviour                                                                                                                                                                                                                                                                                                                                                         |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/reports/prescribers?from=&to=` | `{data, more}`: the distinct prescribers (registration with council, under the name last recorded) on prescriptions cited by a sale line dated in the period (at most 366 outlet days), ordered by name, at most 200, `more` beyond. Needs `pharmacy.prescription.read` and `pharmacy.sale.read` (`403` otherwise). Audited (`pharmacy_prescribers`, `list_read`) |

- **Lot.** `lot_number` (1 to 100 characters, trimmed) matches in any case: a recall notice may print it in another, and missing a recalled sale is worse than merging two lots differing only in case. A return counts by its sale line's lot. Rows stay one per product, so a lot number shared by two products shows both. Not audited.
- **Prescriber.** A prescriber is `prescriber_registration` with `prescriber_council`, exactly as recorded; one alone is `400`. Only prescription lines match, a return by its sale line; OTC lines never do. It needs `pharmacy.prescription.read` as well as sale read (`403`, before anything is read).
- **Audited in the read.** A prescriber-filtered page writes an `audit_events` row (`pharmacy_item_sales`, `report_read`, field names `from, to, prescriber_registration, prescriber_council`) in its transaction, the values going to the audit log: no row, no data (tested with a failing audit store).
- **Screen.** Sales by item has a lot number field and, for prescription readers, a prescriber picker from the period's list; all filters combine.
- **Not covered:** lot or prescriber filters on other reports; a prescriber master or links to staff records (#83); more than 200 prescribers in a period, for which the screen asks for a shorter period.

### Reports by item category (#988 slice 2)

Sales by item and sales and stock take `category_id`, and with it
optionally `subcategory_id` ([design](../../superpowers/specs/2026-11-04-pharmacy-category-report-filters-design.md)).

- **A sale line counts under the category in force on its sale date**,
  which slice 1 makes final (no approval takes effect on or before the day
  it is made). A product moved in the period is split, each sale where it
  was made.
- **A credit-note line counts under its sale line's category**, so a return
  cancels the sale it returns even after the product moved; the note is
  still in the period by its own date.
- **Sales and stock lists a product by its category today**, as its stock
  is now, with only the units sold and returned under that category.
- `subcategory_id` without `category_id`, or a malformed id, is `400`; an id
  that is no category of this outlet matches nothing. Retired categories
  filter as any other. Not audited, as the reports are not.
- **Screen.** Each report has its own category, then subcategory, picker for
  catalogue readers; changing the category drops the subcategory.
- **Not covered:** an "uncategorised" filter, category totals, and category
  filters on other reports.

### CSV downloads of the reports (#1060)

[Design](../../superpowers/specs/2026-11-05-pharmacy-report-csv-design.md); the audited files: [design](../../superpowers/specs/2026-11-05-pharmacy-audited-report-csv-design.md).

| Route                                                                                                                                 | Behaviour                                                                                                                                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/reports/items.csv?from=&to=&product_id=&category_id=&subcategory_id=&lot_number=&prescriber_registration=&prescriber_council=` | Sales by item as a file. Requires `pharmacy.sale.read`, and `pharmacy.prescription.read` with a prescriber. Audited with a prescriber                                     |
| `GET …/reports/receipts.csv?from=&to=&kind=`                                                                                          | Receipts and refunds as a file. Requires `pharmacy.sale.read`                                                                                                             |
| `GET …/reports/stock-sales.csv?from=&to=&category_id=&subcategory_id=`                                                                | Sales and stock as a file, each row with `stock_on`, the outlet's today. Requires `pharmacy.stock.read`                                                                   |
| `GET …/reports/patient-sales.csv?patient_id=&from=&to=`                                                                               | One patient's purchases as a file, each row with the patient id. Requires `pharmacy.sale.read`. Always audited                                                            |
| `GET …/sales/worklist.csv?from=&to=&customer=&linkage=`                                                                               | The worklist for a period as a file, newest first; `invoice` is refused (one sale is its summary). Requires `pharmacy.sale.read`. Audited when it holds an admission sale |
| `GET …/reports/daily.csv?from=&to=`                                                                                                   | The daily report as a file, one row per outlet day and currency. Requires `pharmacy.sale.read`                                                                            |

- **The report's own query and filters,** in one transaction, so a file matches its pages. Any other parameter, a repeated or empty one, `cursor` and `limit` are `400`.
- **All or an error.** At most 5,000 rows and 8 MiB; more is `413 report_export_too_large` before any byte of the file. A failed read is an error with no file. An empty period is a header alone.
- **Format.** UTF-8 with a byte-order mark (for Excel), CRLF, snake_case headers, `period_from` and `period_to` on every row. Formula-like text is apostrophe-prefixed. Amounts are exact in the currency's major unit (`-12.50`), from the country profile's `currency_minor_digits`; a currency no profile declares is a `500` and no file. Units are integers. `Cache-Control: private, no-store`, `nosniff`, `pharmacy-<report>-<outlet>-<from>-<to>.csv`, `X-Export-Row-Count`, `X-Export-Sha256`.
- **A personal file is a disclosure, audited in its read.** A file of one patient's purchases, of one prescriber's sales, or of a worklist holding an admission sale (checked over every row of the file) writes an `audit_events` row (`report_export`, `list_export` for the worklist; field names only) in the read's transaction, after the whole file is built: a refused file is never recorded, and an audit store that refuses the row fails the download with no file (tested). See [audited reads](#audited-reads-1555).
- **Every file sent is recorded** in the audit stream: `pharmacy.report.exported` with the report, actor, outlet, filters as applied, row count and SHA-256. The other files hold no patient, prescriber or buyer and write no `audit_events` row, as their pages do not.
- **The worklist file carries what the worklist carries:** no buyer name or phone, no patient id.
- **Screen.** Each report, the worklist and the daily report have a "Download report CSV" button for their period and filters; a filter change cancels a download in flight. Where a file is recorded, the screen says so.
- **The record is the `audit_events` row.** Its audit-log line is written with the row, before commit, as for audited pages: if the commit then fails, the row rolls back and no file is sent, but that log line remains.
- **Not covered:** buyer names or patient ids in the worklist file; the sale summary and credit notes as files; files above 5,000 rows; XLSX, PDF, scheduled exports and the governed export service (#594, #64, #444).

### Report screens and the legacy reports (#1060)

[Design](../../superpowers/specs/2026-11-05-pharmacy-report-walkthrough-design.md).

- **Each report section shows its own state.** This covers each section of Daily sales, the Sales worklist, and an opened sale's statement. In order:
  - **Refused:** a 403 or 404, including access withdrawn while the page is open. The report sections say "You don't have access to this outlet's reports."; the statement says "This sale was not found, or you do not have access.". Neither offers "Try again", because retrying can't change an access answer.
  - **Session ended:** a 401 says "Your session has ended. Sign in again to continue.", with no "Try again". Every screen behaves this way (#1591, [frontend standards §5](../../standards/frontend.md#failed-and-refused-reads-1276-1591)).
  - **Failed:** the section's message with "Try again". The other sections keep their rows.
  - **Loading** and **empty**, both announced (`role="status"`). The empty state is a sentence naming the period.

  A changed period or filter never shows the last answer's rows. An invalid period (the last day before the first, or over 366 days) is refused in the browser with no request sent.

- **Read from cache.** A period read in the last five seconds is answered from the browser's cache without asking the server. So after access is withdrawn, rows the user has just seen can come back for those five seconds. Nothing they hadn't already been shown is disclosed.
- **Browser tests.** `e2e/tests/pharmacy-reports.spec.ts` posts two cash sales through the counter (one to a walk-in buyer, one to a registered patient of a hospital that has verified the pharmacy) and returns one unit. It checks:
  - every report on Daily sales and Sales, and every one of their files, against those amounts. Each file has exactly the rows its screen shows.
  - on Daily sales, in a running browser: invalid periods, empty sections, one section failing while the others answer with no row of the last period left, and access withdrawn with every section refused.
- **Component tests only.** The Sales screen's and the statement's empty, failed and refused states, and a category filter narrowing sales to a product, are covered by component (Vitest) and API tests. A category takes effect tomorrow at the earliest, so the walkthrough can only show today's sales falling outside one.
- **Not in the walkthrough.** It covers no admission sale, which needs a MediCore admission, and no prescription sale, which is changing in #1477. API tests cover both.

| Legacy report                                              | Here                                                     | Not built, and why                                                                                         |
| ---------------------------------------------------------- | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Sales invoice summary (449, 731)                           | Daily sales: invoices, totals, discount                  | "Deleted amount": a posted sale is never deleted; it's corrected by a credit note, which the report counts |
| Sales receipts and refunds (450), receipt and refund (725) | Receipts and refunds, by kind                            | Cheque, demand draft, PayTM and NEFT, which the counter doesn't take                                       |
| Item-wise sales (441)                                      | Sales by item, with category, lot and prescriber filters | Average purchase cost: purchase cost formulas are not approved                                             |
| Patient-wise sales (442)                                   | Patient-wise sales                                       | Purchase amount, for the same reason                                                                       |
| Sales stock report (755)                                   | Sales and stock                                          | The last vendor: a receipt records no supplier                                                             |
| Sales profit margin (452, 719, 749), monthly G.P. (444)    | —                                                        | Purchase cost, tax and gross and net margin formulas need approved examples                                |
| Indent MIS (443, 737)                                      | —                                                        | Needs a published SupplyConnect contract                                                                   |
| Wholesale party-wise and invoice summaries (728, 734)      | —                                                        | There is no wholesale sale                                                                                 |
| Pharmacy summary report (752)                              | —                                                        | The patient, insurer and hospital split waits on ledger heads (#1051, #987 slice 3)                        |

### The sales worklist and sale summary (#987 slice 1)

| Route                                                        | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/sales/worklist?from=&to=&customer=&linkage=&invoice=` | Paged, newest first: one row per sale dated in the period (at most 366 outlet days), or the one sale with `invoice` exactly (no period needed). Each row: invoice, date, `customer` (`walk_in`, `patient` or `admission`, with the bed code for an admission), `linkage` (`admission`, `opd` or `none`: what the sale is linked to, never its id), total, discount, units, credited so far, net, and what is still on the admission account. `customer` and `linkage` narrow to one kind each. Requires `pharmacy.sale.read` |
| `GET …/sales/{id}/summary`                                   | One sale's statement: each line with units returned and amount credited, each service charge with amount credited, payments by kind, credit notes with refunds by kind, and the balances `credited_minor`, `net_minor`, `collected_minor`, `refunded_minor`, `admission_billed_minor`, `admission_reversed_minor`. At most 100 credit notes are listed, with `more_credit_notes` when there are more; the totals count every note. Requires `pharmacy.sale.read`; a sale of another outlet is 404                            |

- **The summary reconciles.** It is read in one snapshot, so a return posted meanwhile is wholly in or wholly out (tested). Lines and charges make the total; payments settle it; the credit notes credit exactly their lines and charges and are refunded in full. So `net = collected − refunded + (admission billed − admission reversed)` for every sale. The tests prove each identity for a walk-in sale with a credited service charge, a registered patient's sale and an admission sale.
- **No direct identifiers.** Neither read carries the buyer's name or phone, the patient's id or a credit note's recipient, only who the sale was for as a kind. An admission sale shows its bed code, which with the date points to whoever was in that bed, so a worklist page or summary showing one is recorded (see [audited reads](#audited-reads-1555)).
- **Credits are as of now.** A sale counts on its own date; its credit notes are every one posted so far, whatever the period.
- **Not covered:** payer liability split (slice 3, with #1051), casualty sales, sales across outlets, buyer or patient search. The worklist downloads as CSV (see [CSV downloads](#csv-downloads-of-the-reports-1060)). OPD encounter linkage is slice 2, below.

### Audited reads (#1555)

Every read of a pharmacy record that names or points to a person writes an `audit_events` row in the read's own transaction, after the rows are read: no record, no data. The row holds the filter names, never their values; the audit log line holds the values. A refused read (400, 403, 404) records nothing.

| Read                                               | `resource_type`           | `resource_id`    | `action`                                                                              | When                             |
| -------------------------------------------------- | ------------------------- | ---------------- | ------------------------------------------------------------------------------------- | -------------------------------- |
| `GET …/sales/{id}`                                 | `pharmacy_sale`           | the sale         | `record_read`                                                                         | always                           |
| `GET …/sales`                                      | `pharmacy_sales`          | the outlet       | `list_read`                                                                           | every page                       |
| `GET …/credit-notes/{id}`                          | `pharmacy_credit_note`    | the note         | `record_read`                                                                         | always                           |
| `GET …/credit-notes`                               | `pharmacy_credit_notes`   | the outlet       | `list_read` (`sale_id` when given)                                                    | every page                       |
| `GET …/prescriptions/{id}`                         | `pharmacy_prescription`   | the prescription | `record_read`                                                                         | always                           |
| `GET …/prescriptions`                              | `pharmacy_prescriptions`  | the outlet       | `list_read`                                                                           | every page                       |
| `GET …/prescriptions/open-lines`                   | `pharmacy_prescriptions`  | the outlet       | `open_lines_read` (`product_id`)                                                      | always                           |
| `GET …/sales/worklist`                             | `pharmacy_sales_worklist` | the outlet       | `list_read` (`from,to,customer,invoice,linkage`)                                      | a page showing an admission sale |
| `GET …/sales/{id}/summary`                         | `pharmacy_sale`           | the sale         | `summary_read`                                                                        | an admission sale                |
| `GET …/reports/patient-sales` and `.csv`           | `pharmacy_patient_sales`  | the outlet       | `report_read`, `report_export` (`from,to,patient_id`)                                 | every page; every file           |
| `GET …/reports/items` and `.csv` with a prescriber | `pharmacy_item_sales`     | the outlet       | `report_read`, `report_export` (`from,to,prescriber_registration,prescriber_council`) | every page; every file           |
| `GET …/sales/worklist.csv`                         | `pharmacy_sales_worklist` | the outlet       | `list_export` (`from,to,customer,invoice,linkage`)                                    | a file holding an admission sale |
| `GET …/opd-encounters`                             | `pharmacy_opd_encounters` | the outlet       | `patient_visits_read` (`patient_id,hospital_id`)                                      | always                           |

- **Who opened a record** is one indexed query on `(tenant_id, resource_type, resource_id)`.
- **The sale summary** reads in one read-only REPEATABLE READ snapshot, so its statement stays one reading. Its audit row, for an admission sale, is written in its own transaction with the outlet grant checked again, after the snapshot and before anything is sent: if it cannot be written, the read fails with no data (tested). No writable snapshot exists, so none can carry a business write.
- **An audit store that refuses the row fails the read** with an error and no data (tested through a test seam standing for that refusal, on every audited route).
- **Not covered:** the tenant-wide dispense queue (`GET /pharmacy/dispenses…`), which shows patient names from MediCore's visits; auditing it is a dispensing-workflow decision and #1555 stays open for it.

### Returns as credit notes (#1386 C-1)

A return is a GST credit note against one posted invoice. The rules followed are CGST Act 2017, s.34, and CGST Rules 2017, Rule 53(1A), as published by CBIC. Returned medicine goes to quarantine and is sold again only after a pharmacist releases it (A3).

| Route                                                    | Behaviour                                                                                                                                                                                                                                                                                                                                                                     |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/credit-notes/{uuidv7}`                            | Takes `{sale_id, lines: [{sale_line, quantity_units, quarantine_location_id}], refunds: [{kind, amount_minor, reference?}], reason, recipient: {name, address}}`. Requires `pharmacy.sale.return`, held by the pharmacist, with the outlet `editor` grant. Returns 201, or 200 for a replay; a different intent is 409. Every refusal is 409 `not_returnable` with its reason |
| `GET …/credit-notes/{id}`, `GET …/credit-notes?sale_id=` | The credit note (and the paged list). It carries the original invoice's number and date, the seller, the recipient, and each line's HSN, batch, units, rate, taxable value and tax credited. Requires `pharmacy.sale.read`                                                                                                                                                    |
| `GET …/sales/{id}`                                       | Each line now reports `returned_units`                                                                                                                                                                                                                                                                                                                                        |

- **Amounts reverse the sale line.** A credit uses the line's charged amount and tax, net of any discount, and the sale's own rate. Credits are the cumulative pro-rata step, `floor(v × (k+r) ÷ q) − floor(v × k ÷ q)`, of the taxable value and of each tax half, and the amount credited is their sum. So no step is negative, and all the returns of a line together credit exactly what it charged.
- **Limits.**
  - The units returned on a line, across all credit notes, never exceed the units sold.
  - Every line goes to a quarantine location.
  - The refunds must equal the total.
  - The credit note is refused after 30 November following the end of the sale's financial year (s.34(2)). The other limit in s.34(2), the date the annual return is filed, is not known to the system and is the outlet's to observe.
- **The number** is `PREFIX/FY/Cn`. It comes from its own gap-free series per outlet and per financial year of the credit note's date.
- **Database guarantees.**
  - Each credit line locks its sale line and checks the units and the pro-rata amounts.
  - A credit note is sealed to its own transaction.
  - At commit, the note must equal its lines, be refunded in full, and have one `return` movement per line.
  - A `return` movement is accepted only as its credit line's units of that line's batch, into its quarantine location.
- **Events.** `credit_note_issued.v1` carries ids, the number, currency, total and each line's product, batch and units. It carries no recipient details.
- **Not covered.**
  - Destruction and disposal, and a recorded condition or cold-chain judgement: every return goes to quarantine.
  - Returns from wards and failed deliveries.
  - Credit notes without goods, and debit notes.
  - E-invoicing (#539).
  - A digital signature: the credit note records who issued it.

### Discounts (#1386 B-3)

A discount recorded on the invoice at the time of supply is outside the value of supply (CGST Act 2017, s.15(3)(a), as published by CBIC; CBIC Circular 92/11/2019-GST), so GST is computed inside the discounted amount.

| Route                             | Behaviour                                                                                                                                                                                               |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/discount-ceiling/{uuidv7}` | Records `{max_bps 0–10000, reason}`, the outlet's ceiling in basis points. Requires `pharmacy.discount.manage`, which the pharmacy owner holds. The latest record is in force, and every record is kept |
| `GET …/discount-ceiling`          | Returns `{ceiling}`, or `null`. With no ceiling recorded the outlet allows no discount. Requires `pharmacy.sale.read`                                                                                   |

- **Giving a discount.**
  - A sale or quote line takes an optional `discount_bps` (1–10000). It needs `pharmacy.sale.discount`; without it the request is 403 before anything is priced.
  - That permission has no system role; a tenant administrator assigns it.
  - A discounted sale needs `discount_reason` (5–200 characters), and only a discounted sale may carry one.
  - A discount above the ceiling, or one that leaves nothing to charge, is 409 `not_sellable`.
- **Arithmetic.** Each batch line's discount is `floor(list amount × bps ÷ 10000)`, and the line charges the rest, with CGST and SGST inside it. The invoice shows each line's `list_amount_minor`, `discount_bps` and `discount_minor`, and the sale's `discount_minor` and `discount_reason`.
- **Database guarantees.** A line's discount must be its rounded percentage, its list amount must be within MRP, and something must be charged. The discount must be within the ceiling in force when the line is written, so a ceiling lowered after a quote refuses that sale. A discounted sale must record its reason.
- **Not covered.** Fixed-amount discounts, whole-sale discounts, approval by a second person, and promotions or schemes.

### Ward medicine billed to the admission (#1387)

A sale may be billed to the admission occupying a bed instead of collected at the counter. See the [design](../../superpowers/specs/2026-10-29-inpatient-medicines-to-admission-account-design.md).

- **Pharmacy's copy of who is in which bed.** `pharmacy_bed_occupancies` is fed by `pharmacy-bed-occupancy-started` and `-ended`, from MediCore's events. It holds ids and the ward, room and bed codes. Either event may arrive first, and a stay starts and ends once.
- **Resolve.** `GET …/outlets/:outlet/admission-occupant?hospital_id=&bed_code=` (or `room_code=`) needs `pharmacy.sale.create`, at a hospital that has verified the pharmacy (otherwise `404`).
  - It answers the one open stay, as ids and location codes, without when the stay began.
  - An empty bed is `404 no_occupant`; a room with several occupied beds is `409 ambiguous`.
  - Every resolve is audited (`audit_events`, `pharmacy_admission_resolve`), a miss included.
- **Confirm.** The counter confirms the patient with patientmaster's lookup by `patient_id`.
- **Post.** The sale carries `admission: {occupancy_id}` and one `admission_account` tender for the total. Nothing is collected. The sale is re-resolved under a lock, and it stores:
  - the admission, the stay, the bed code and the resolution time;
  - the occupant as its registered patient, so the B-2c rules apply too.

  It publishes `admission_charge_posted.v1`. The database refuses:
  - a sale on a stay that is no longer open;
  - an admitted sale with any other tender;
  - an `admission_account` tender on any other sale.

- **Daily report.** It shows `admission_account_minor` and `admission_account_refund_minor`, billed and returned to admission accounts rather than collected or paid out, so sales still reconcile to tenders.
- **Returns.** A credit note on an admitted sale refunds only through `admission_account`, and publishes `admission_charge_reversed.v1`. A wrong-bed sale is corrected by a full credit note and a new sale. Migration `0027_pharmacy`.

### A sale linked to its OPD encounter (#987 slice 2)

A registered patient's counter sale may cite the OPD encounter it serves. See the [design](../../superpowers/specs/2026-11-04-pharmacy-opd-encounter-link-design.md). MediCore's encounter contract is consumed as published; MediCore does not change.

- **Pharmacy's copy of OPD encounters.** `pharmacy_opd_encounters` holds the encounter, hospital, patient id, status and version, and `opened_at`, when MediCore published the opening (the envelope's `occurred_at`); no name. A patient's visits are ordered and told apart by `opened_at`, so a replayed or delayed delivery cannot make them look alike. It is fed by `pharmacy-encounter-opened` (`encounter_opened.v1`) and `pharmacy-encounter-status-changed` (`encounter_status_changed.v1`). Either may arrive first; a status moves only with a newer version; a replay naming another patient or hospital, or the same version with another status, is refused and retried, never overwritten.
- **Find.** `GET …/outlets/:outlet/opd-encounters?patient_id=&hospital_id=` needs `pharmacy.sale.create`. It lists the patient's citable visits (`arrived`, `in_consultation`, `closed`) with `opened_at`, newest opened first, at most 20, only for a patient the pharmacy has received at a hospital that has verified it (otherwise `404`). Every answer is audited.
- **Post.** The sale carries `encounter: {encounter_id}` with `patient`; never with `admission` or a walk-in buyer (`400`). The database refuses, as `409 not_sellable` with the reason, an encounter that:
  - the pharmacy has not received, has not yet seen opened, or is not this patient's at this patient's hospital (one answer for all three, so a refusal says nothing about whose a visit is);
  - was `cancelled` or `left_without_being_seen`.

  The hospital-verification check runs before the visit is read, so a hospital that has not verified the pharmacy is refused first.

  A `closed` encounter is citable, as patients collect medicines after the consultation; there is no time limit (agreed 2026-11-04). The sale takes the encounter row `FOR SHARE`, so a status change is wholly before or after it: a sale posted while a cancellation is being applied waits for it and is then refused (tested). A later cancellation does not undo a posted sale. The intent digest covers the encounter.

- **Read.** The invoice read carries `encounter_id` (audited, #1555). The worklist and summary show only `linkage`.
- **Events.** `sale_posted.v1` is unchanged: publishing the link to hospital billing belongs to #1051.
- **Not covered:** casualty sales (the emergency module's contract), changing a posted sale's link (a credit note and a new sale), linking sales posted before this change. Migration `0032_pharmacy`.

### Registered patients at the counter (#1386 B-2c)

A sale may name a registered patient instead of a walk-in buyer: `patient: {patient_id, hospital_id}`, never with `buyer` (`400`). The pharmacist finds the patient with patientmaster's [pharmacy lookup](patients-and-clinical.md#pharmacy-patient-lookup-1416b) by exact MRN at a hospital that has verified the pharmacy, and confirms the name, age and sex shown.

The sale posts only if the database accepts both conditions; otherwise it is `409 not_sellable`:

- `(patient_id, hospital_id)` is in `pharmacy_registered_patients` (a foreign key);
- the outlet's pharmacy holds an affiliation with that hospital whose latest verification step is `accepted` (trigger `pharmacy_sale_patient_affiliated`).

The sale takes the affiliation's lock, so a verification step being applied is ordered wholly before or after it. Pharmacy stores only the two ids (`patient_id` and `patient_hospital_id` on the sale read). `sale_posted.v1` carries no patient. The invoice says "Registered patient" with the hospital. Migration `0026_pharmacy`.

### Selling against a prescription (#1386 B-2b)

The source is Drugs Rules 1945, Rule 65 (CDSCO 2022 compilation): Schedule H and H1 are sold only on a prescription (65(9)), which names the patient's address and the total quantity (65(10)). It is dispensed only as often as the prescriber allows (65(11)), and nothing is supplied in its place (65(11A)). Schedule G is held to the same rule by product decision; that is stricter than Rule 65.

- **What was prescribed.** `PUT …/prescriptions/{uuidv7}` now requires `lines`, 1 to 20 of them. Each line is `{prescribed_as, product_id, quantity_units (1–100000), dispensings_allowed (1–12)}`. An optional `prescriber_address` is accepted. A prescription and its lines are recorded in one transaction and never change. `GET …/prescriptions/{id}` returns each line with `units_remaining` and `dispensings_remaining`. Prescriptions recorded before B-2b have no lines and cannot be sold against.
- **The counter's lookup.** `GET …/prescriptions/open-lines?product_id=` returns the lines for a product that can still be dispensed, newest first, at most 50. Each carries `addresses_on_file`.
- **The sale.** A sale or quote line takes an optional `prescription: {prescription_id, line_number}`:
  - It is required for G, H and H1 and refused on an OTC line.
  - The cited line must be for the same product, dated on or before the sale, and have enough units and a dispensing left.
  - For H and H1, the prescription must give both the prescriber's and the patient's address.

  Each refusal is 409 `not_sellable` with its reason. A sale whose allocation spans several batches is one dispensing. A replay dispenses nothing more.

- **Database guarantees.** The sale-line guard locks the cited prescription line and checks product, date, units, dispensings and addresses itself. A line carries a citation exactly when its class is not `none`. A bug in the handler cannot dispense more than the prescriber allowed.
- **Walk-in buyer.** A sale takes an optional `buyer: {name?, phone?}` (phone `^\+?[0-9]{6,15}$`). It is shown with the invoice to `pharmacy.sale.read` holders and never published.
- **Not covered.** Directions to dispense at intervals (the pharmacist records the number of dispensings), an age limit (the rules set none), Schedule X and NDPS (#165, #171), and the H1 register report (#1062).

### Prescription evidence (#1386 B-2a)

The outlet's record of a prescription, and its scanned pages. Nothing sells against it until B-2b. Every route needs the pharmacist's `pharmacy.prescription.record` (writes, with the outlet `editor` grant) or `pharmacy.prescription.read` (any outlet grant). The pharmacy owner holds neither: a prescription is patient data.

| Route                                             | Behaviour                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PUT …/prescriptions/{uuidv7}`                    | Records `{prescriber_name, prescriber_registration, prescriber_council, prescribed_on, patient_name, patient_address?}`. The date cannot be after the outlet's today. Returns 201, or 200 for a replay by the same pharmacist; any other body or recorder is 409. A prescription is never changed or deleted |
| `GET …/prescriptions/{id}`, `GET …/prescriptions` | The record and the paged list                                                                                                                                                                                                                                                                                |
| `PUT …/prescriptions/{id}/images/{uuidv7}`        | An upload intent `{filename, content_type: image/png\|image/jpeg, size ≤ 10 MiB, sha256}`, valid for 30 minutes. A replay with different details is 409. Without private storage configured it is 503 `evidence_unavailable`                                                                                 |
| `PUT …/images/{file}/content`                     | The exact bytes: matching type, size and checksum, and a passive image, before anything is written to quarantine. Returns 202 `scan_pending` and queues `prescription_image_scan_requested.v1`                                                                                                               |
| `POST …/images/{file}/retry/{attempt}`            | After `scan_error`, queues the next attempt only                                                                                                                                                                                                                                                             |
| `GET …/images/{file}/download`                    | Only an `available` page whose released generation, size and checksum match. Sent `private, no-store`, `nosniff`, as an attachment under a sandbox CSP. Every download writes an audit `read` event                                                                                                          |
| `GET …/prescriptions/{id}/images`                 | The paged pages with their status: `upload_pending`, `scan_pending`, `available`, `rejected` (`malware_detected`, `unsupported_content`, `integrity_mismatch`) or `scan_error`                                                                                                                               |

**The pharmacy evidence worker** (`cmd/pharmacy-evidence-worker`) consumes the scan request through Temporal (`pharmacy-prescription-image-v1`) and connects as `helivanta_pharmacy_evidence_worker` with `PHARMACY_EVIDENCE_WORKER_DATABASE_URL`; it never falls back to another module's credential. Its session names one file, and row security shows it that file only. It holds no privilege on prescriptions, can change only the lifecycle columns, and can write only its own audit rows and a `prescription_image_changed.v1` whose status the row holds. No page can be revoked yet. Once a sale can cite a prescription (B-2b), revoking a cited page would remove a posted sale's evidence, so that rule is decided there.

Until the worker is rolled out (#1406), uploaded pages stay `scan_pending` and are never downloadable.

### Stock movement totals (#1638)

`GET …/outlets/{outlet}/stock/reconciliation` (one batch at one location,
`from` to `as_of`) returns `opening_units` and `closing_units` (numbers), a
signed in-period total per movement kind as an exact decimal string
(`receipt_units`, `correction_units`, `release_units`, `sale_units`,
`return_units`, `supplier_dispatch_units`) and `movement_count`, in one
statement. Totals are stored quantities, never sign-flipped: receipts and
returns are positive, sales and supplies to hospital orders negative, a
release negative where stock left and positive where it arrived, a correction
either. `opening + every kind = closing`, tested; a test also fails when the
movement kind constraint names a kind without a total. Register rows (outlet
and pharmacy-wide) carry the same six totals over all history to `as_of`, so
`on_hand_units` equals their sum, and the stock CSVs end `on_hand_units`,
`receipt_units`, `correction_units`, `release_units`, `sale_units`,
`return_units`, `supplier_dispatch_units`. Snapshot rows and CSVs carry no
`dispensable_units`: sellability at a past `as_of` cannot be reconstructed, so
only the balance reports it (#1708). The panel shows every signed part and, if the parts are
missing or do not sum, an alert instead of a reconciliation.

### Stock across outlets (#1372)

`GET /v1/pharmacy/pharmacies/{pharmacy}/stock/positions` is a read-only,
pharmacy-level view of the existing outlet-owned stock ledger. It requires
`pharmacy.stock.read` **and a current explicit grant on every included outlet**;
a directory manager or tenant administrator receives no implicit stock grant.
No granted outlet in this pharmacy is 404; missing route permission is 403;
unavailable outlet authorization is 503. No hospital affiliation grants access.
The staff page is `/pharmacy/stock-overview`, through the Global API as usual.

- **Filters and paging.** An explicit `as_of` (RFC3339, microsecond precision,
  not future), `expiry=all|expired|unexpired|today|due_30|due_90|due_180`,
  `balance=all|on_hand`, and optional `outlet_id`. Unknown, repeated and empty
  filters are 400. A requested outlet outside the caller's grants or pharmacy
  is 404. Standard `limit`/`cursor` paging is bounded across the combined result,
  newest batch first with a scoped position hash as tie-breaker. Each outlet
  evaluates expiry in its own timezone, using the same source and filters as
  its stock register. Physical units include quarantine/expired stock; rows
  carry no dispensable figure (#1708).
- **Scope disclosure.** `X-Pharmacy-Stock-Coverage` is `all_accessible` or
  `restricted_outlets_excluded`, including empty pages. The latter explicitly
  means that totals do not cover the whole pharmacy. Excluded outlet names,
  quantities and values are never disclosed. Coverage, totals, current grants
  and rows use one read-only REPEATABLE READ snapshot with an eight-second SQL
  statement timeout and ten-second request query budget. Every page/export
  reauthorizes; `as_of` freezes the movement cutoff, not future grant changes.
- **Product totals.** Each position includes the owning outlet ID/code/name,
  currency, `product_on_hand_units`, `known_purchase_value_minor` and
  `uncosted_units`. The last three are exact decimal strings and cover every
  matching position of that **outlet/product/unit**, before pagination. They
  repeat on each position and **must not be added together**. Catalogue product
  IDs are outlet-owned; identical SKUs or names are not a shared formulation
  identifier. Cross-outlet identity consolidation awaits an authoritative
  shared product linkage; this view never invents one or sums different units.
- **Valuation.** Existing batch `purchase_cost_minor` is the recorded purchase
  cost **per pack**, as on the receiving form. For each product, known purchase
  value is `floor(sum(on_hand_units × known batch pack cost) / pack_size)`.
  All arithmetic is PostgreSQL numeric; rounding happens once per product,
  so splitting a batch between locations cannot change its value. Missing
  cost contributes to `uncosted_units`, not a fabricated zero cost. Recorded
  zero cost is a known zero. Landed cost, freight, tax adjustments, margin and
  sale/NPPA price compliance are not inferred by this report.
- **Combined value.** `X-Pharmacy-Stock-Valuation` is a JSON array, bounded to
  20 currencies, with `{currency, known_purchase_value_minor, uncosted_products}`
  (the latter two exact decimal strings). It sums each product value **once**
  within its currency, and identifies incomplete valuation. Currencies and
  quantities in different units are never combined. An empty filtered result
  returns `[]`, not a fabricated currency/value row. The UI validates both
  metadata headers and refuses to present unconfirmed coverage/value as stock.
- **CSV.** `GET …/stock/positions.csv` uses the same filters, without pagination
  parameters. It exports all matching accessible positions, with outlet scope,
  coverage, product totals and the existing register fields; it refuses over
  1000 positions or 8 MiB, never truncates. Product totals repeat per position.
  Formula-like text is neutralised, downloads are `no-store`/`nosniff`, and
  successful exports log actor, tenant/pharmacy, cutoff, row count and SHA-256
  without cell values. Row count and digest are also response headers.

Canonical additional fields are in `contracts/pharmacy.json` (generated Go/Zod).
Migration `0024_pharmacy` changes the read predicates on four inventory tables
only. Existing single-outlet editor `WITH CHECK` predicates remain intact;
prescription, sales, financial and other private tables gain no overview access.
This implements the operational read aspects of IN-PH-01/03 and ORG-AC-01/02/07/08 explicit
scope/fail-closed principles, not a new stock master, transfer, legal valuation
policy or clinical access path. There are no new events or consumers.

### Hospital affiliation verification (#1416)

A declared hospital link grants nothing until that hospital verifies it. The declaration stays immutable. Each step is a new row in `pharmacy_affiliation_verifications`, and the latest step is the link's `verification_status`: `pending_validation`, `requested`, `verified`, `declined`, `revoked` or `withdrawn`. Same tenant only. A link to another customer's hospital needs a separate data-sharing agreement. See the [design](../../superpowers/specs/2026-10-29-pharmacy-affiliation-verification-design.md).

| Route                                                            | Behaviour                                                                                                                                                                                                                                                                                |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/hospital-affiliations/:id/verification-requests/:request` | `pharmacy.access.manage`. Opens a round (`requested`) and publishes `affiliation_verification_requested.v1` in the same transaction. A retry of the same round is `200`. A new round while one is `requested` or `verified`, or a round id already used, is `409 verification_conflict`. |
| `POST …/verification-requests/:request/withdraw`                 | `pharmacy.access.manage`. Body `{reason}`, 1 to 500 characters. Ends a `requested` or `verified` round and publishes `affiliation_verification_withdrawn.v1`, which carries the request's whole claim. A retry with the same reason is `200`; anything else is `409`.                    |
| `GET …/hospital-affiliations/:id/verifications`                  | Paginated history with actor, reason and the hospital's decision receipt.                                                                                                                                                                                                                |

The hospital decides in patientmaster ([patients](patients-and-clinical.md#pharmacy-affiliation-review-1416)). The `pharmacy-affiliation-decision` consumer applies a decision only to the round that is still open with that id. A decision for a withdrawn or already-decided round is a no-op, so redelivery and reordering converge. A database trigger enforces the transitions and gap-free numbering. Both directions are delivered until confirmed. Patientmaster confirms each request and withdrawal it handles (`pharmacy_affiliation_received.v1`), and pharmacy confirms each decision (`affiliation_decision_received.v1`). Each module's redelivery loop re-sends an unconfirmed step with exponential backoff from 2 minutes up to 60, and never gives up. So a dead-lettered withdrawal or revocation cannot leave the two sides disagreeing. A verified link is the precondition for patientmaster's [pharmacy patient lookup](patients-and-clinical.md#pharmacy-patient-lookup-1416b). Pharmacy also keeps `pharmacy_registered_patients`, the ids-only list of the tenant's registrations built by `pharmacy-patient-registered`. Registrations made before the consumer existed arrive through the #1377 replay. B-2c will accept a patient on a sale only if the id is in this list for a hospital the outlet's pharmacy is verified with.

Each affiliation in the list and in the affiliation responses carries `hospital_name`. It is the hospital's name as workforce last announced it (#1637), and only while the affiliation's current `verification_status` is `verified`. In any other status, or before a name has arrived, it is `null`, so a link the hospital has not confirmed discloses nothing about the hospital. The `pharmacy-hospital-names` consumer stores every announced name in the tenant (`pharmacy_hospital_names`), and the newest `name_version` wins, so redelivery and reordering converge. The consumer confirms each version it handles, including stale ones, in the same transaction, and workforce re-sends names that are not confirmed, so a dead-lettered name arrives within about an hour after the failure clears. The counter's hospital pickers label an option with the name, add the id when two hospitals share a name, and show `Name unavailable · <id>` when `hospital_name` is `null`.

The affiliation management list displays an independently permitted hospital-directory name when the current account already has access to that directory, including while verification is pending. Otherwise it uses the verified `hospital_name` projection, or explicitly says `Hospital name unavailable`. It does not use the hospital UUID as a display name, query an unrestricted directory, or change what affiliation verification authorizes. The affiliation API keeps its verified-only name disclosure rule.

## Integration requirements

Published catalogue, stock, payable, settlement and safety events mostly carry scoped references only. `dispense_recorded.v1` and the `pharmacy-visit-intake` consumer of MediCore's `visit_created.v1` are retired (#1680). Pharmacy no longer receives visit patient names. The orphaned JetStream durable `pharmacy-visit-intake` holds nothing beyond the stream's 24 h retention and is removed by an ops step.

A clinical dispense contract must require an authorized current prescription/version, patient/hospital linkage, outlet authority, pack/unit conversion, available safe batch quantities, substitution policy and audit. Local stock reservation and dispense posting need durable idempotency and concurrency constraints. Billing uses a stable source-line ID. After physical dispense, billing failure is `charge_pending` recovery; deleting the dispense is not compensation.

Supply transfer requires a source dispatch, destination receipt, quantities/units/batches and immutable discrepancy record; only the receiving outlet owner updates its stock. Bilateral commercial sharing requires current country policy, licences, identity and approved relationship, not common ownership. Payment adapters must distinguish settlement recording from provider-side capture/refund and reconcile ambiguous responses.

## Remaining work and acceptance

Trace requirements to [#1133](https://github.com/tesserix/helivanta/issues/1133), [#159](https://github.com/tesserix/helivanta/issues/159), [#162](https://github.com/tesserix/helivanta/issues/162), [#166](https://github.com/tesserix/helivanta/issues/166), [#167](https://github.com/tesserix/helivanta/issues/167), [#987](https://github.com/tesserix/helivanta/issues/987), [#1060](https://github.com/tesserix/helivanta/issues/1060) and the [reconciled backlog](../../delivery/2026-10-18-pharmaconnect-backlog.md). Do not label these whole stories complete because a foundation exists.

Approve full sales/returns/recalls/expiry release, signed prescription fulfillment, GST/tax policy beyond the tax setup above, price schemes and regulated reporting per country before defining stable external contracts. Required tests include negative/overflow quantity, concurrent receipt/correction, duplicate command with changed content, expired/blocked batch, cross-outlet grants, stale tenant, local midnight expiry, export scope, settlement reversal and consumer retry after commit. Database failure denies a stock/financial mutation; broker lag leaves outbox work pending; a report must expose unavailable data rather than show a false zero.

### Registration envelope compatibility

Patientmaster publishes its existing ID-only registration payload on
`helivanta.in.patientmaster.registered.v1` with envelope type `PatientRegistered`.
The subject routes delivery; it is not the envelope type. Both owner publication
paths and Pharmacy's registered-patient consumer use the public
`TypePatientRegistered` contract constant. Other types, unsupported versions,
directed envelopes and malformed payloads remain rejected.

A real-owner integration test registers a synthetic patient through the owner
HTTP route, reads the actual durable outbox envelope, and delivers those bytes to
the Pharmacy consumer. It verifies no name/date of birth in the payload, exact
hospital retention, idempotent replay and zero foreign-tenant rows. This proves
producer/consumer serialization compatibility; broader broker/UI and deployment
acceptance remain separate gates. The consumer still grants no dispensing or
hospital affiliation authority; sales independently require current verified
scope and all existing stock/financial controls.

### Outlet licence images, independent review and renewal advice (#1391)

Pharmacy owns one immutable PNG/JPEG evidence image (up to 10 MiB) per outlet
licence declaration. `PUT /licences/:id/evidence/:file` records the exact filename,
media type, size and SHA-256 intent; the `/content` PUT accepts only those bytes.
The outlet prefix is the same as other catalogue commands. Writes require
`pharmacy.licence.manage` and an outlet editor grant. Evidence listing and
`/download` require `pharmacy.licence.evidence.read` (Pharmacy owner role) plus a
current outlet read grant; every successful download is audited and reauthorized
after the private-object read. Foreign tenant/outlet/record references remain 404.

Files stay quarantined until the dedicated Pharmacy evidence worker records an
exact-generation scan and guarded release. The new licence workflow shares the
existing bounded Pharmacy worker queue; it has a distinct workflow/activity name,
ID tuple and restricted table grants. Scan-error retries use the next canonical
attempt; malware rejection cannot be retried into approval. A corrected document
requires a new declaration, retaining the original file and history. Downloads
never expose bucket paths, hashes or public URLs. Licence keys use the separate
`pharmacy_outlet/.../k/licence/r/...` namespace, preventing identical caller-chosen
licence/prescription/file IDs from aliasing stored bytes.

The new scan-request/outcome events carry only tenant/outlet/licence/file IDs,
attempt and closed lifecycle status. Licence numbers, registration details,
filenames, hashes and bytes are excluded from events and Temporal history.
Existing prescription paths and completed licence decisions are preserved.

New `POST /licences/:id/review` decisions require available evidence for that exact
declaration. Both the handler and INSERT trigger enforce the requirement and
serialize on the declaration; absence, pending scan, rejection or scan error
returns `409 licence_evidence_required`. Independent review still returns
`409 four_eyes` for the author, and an existing completed decision remains
immutable (`409`). The decision pins the released file and generation; responses
add nullable `reviewed_evidence_file_id`. Historical decisions keep a null proof
reference and remain effective under their original outlet-date rules.

The setup UI provides declaration entry, exact-image upload, terminal scan status,
recovery, authorized download and independent review. It carries no public file
URLs and does not show self-review. The SDK derives declaration/review/renewal
fields from `contracts/pharmacy.json` and rejects wrong-scope command receipts.

`GET /licences/renewals` is a cursor-paged catalogue read under the current outlet
grant. Each notice carries the declaration ID, declared dates, outlet time zone,
local date, observation instant, exact calendar days remaining and one of
`not_yet_valid`, `expired`, `expires_today`, `renewal_due_30`, `renewal_due_90`,
`renewal_due_180` or `current`. These reuse SupplyConnect's operational reminder
bands; they are not statutory deadlines, issuer verification or operating authority.

Deploy and accept the matching worker capability before enabling the review gate.
#1391 remains open until browser and designated synthetic production worker
acceptance are complete. The local GCS/scanner protocol fixtures do not establish
production ClamAV signature coverage or regulatory approval.

### Reviewed hospital department relationships (unreleased candidate)

The Pharmacy owner records a hospital-owned outlet's responsibility relationship
to an Organization department/revision. The canonical models are
[pharmacy-staff-bindings.json](../../../contracts/pharmacy-staff-bindings.json);
the [design](../../superpowers/specs/2026-11-05-pharmacy-department-bindings.md)
records decisions, recovery and the uncompleted acceptance gates. This does not
establish professional acceptance, Staff duty or permission to dispense.

All routes enter Global API below
`/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/hospitals/{hospital}/staff-department-bindings`.
PUT `/{binding}` prepares an immutable proposal; POST `/{binding}/commands`
decides approve/reject/withdraw/end using a UUIDv7 command and expected version.
GET `/{binding}` separates recorded relationship from current source state;
GET `/{binding}/commands/{command}` recovers an immutable receipt. Collection
and `/{binding}/history` GETs accept only one limit (1–50) and optional opaque
cursor, returning the standard data/page envelope. Lists describe recorded
relationships; current source usability requires detail revalidation.

Each request requires single matching expected-tenant and expected-subject
headers as workspace preconditions, independently approved hospital-scoped IAM
authority and a current Workforce Staff actor. Exact outlet read access protects
disclosure; preparing/deciding additionally requires outlet editor access.
Preparation/withdrawal needs `pharmacy.staff_binding.prepare`; other decisions
need `pharmacy.staff_binding.review`; reads need `pharmacy.staff_binding.read`.
A second account linked to the proposing Staff record cannot review it.
New exact IAM templates are in the `pharmacy-department-bindings-v1` catalog;
the administration list is `/v1/iam/pharmacy-binding-assignment-proposals`.

Source changes or stale expected versions prevent approval. Replacement names
the prior approved binding/version, atomically ends it and approves the new one.
Exact command replay recovers original evidence under current authorization and
records a disclosure audit. Audit/outbox unavailability returns 503 and rolls back
business and Staff actor evidence. Foreign outlet/organization objects are hidden
with 404; missing scoped authority is 403, changed intent/source is 409.
Identifier-only `helivanta.in.pharmacy.staff_department_binding_changed.v1`
events carry the relationship transition and optional replaced identity/version;
there is no operating consumer. IAM0029 and Pharmacy0034 are additive persisted
candidates, not production migration or activation evidence.

The unreleased proposal PUT returns the same canonical
StaffDepartmentBindingCommandReceipt envelope as decision and receipt recovery:
the relationship is under `binding`, with retained `authorizations` and nullable
replacement transition fields. The earlier unactivated candidate returned only
the row; it was corrected before release to match the typed SDK and retained
receipt contract. Initial response, exact replay and GET recovery are equal.

Candidate GET `/v1/pharmacy/hospitals/{hospital}/staff-binding-outlet-choices`
accepts one `purpose=read|prepare|review` (default read), limit 1–10 and optional
opaque cursor bound to purpose/hospital/account. It returns only pharmacy/outlet
IDs and owner-record names. It needs matching expected-tenant/subject assertions,
exact hospital-scoped binding read authority and a current Staff actor; prepare
or review adds its exact action authority. Every listed outlet must have an
explicit current reader/editor grant (editor for prepare/review) and a
hospital-owned parent. Standalone parents are excluded without inventing a
hospital relationship. Gates/access locks are acquired in ascending outlet ID
order; presentation retains descending created-at/ID keyset order. Protected
read audits retain null binding ID and real outlet/hospital/actor plus two or
three authority observations. Revocation and audit failures withhold the page.
This metadata projection requires no catalogue/private HR permission and grants
no operating authority. The candidate Staff read view is
`/workforce/pharmacy-relationships`; its separate prepare/review modes now use the
canonical command/receipt SDK with immutable uncertain-attempt recovery. Normal
role browser acceptance remains open. No new migration is added by these choices
or command controls.

### Supplier acquisition and accounts handoff candidate

`GET /v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/accounts/acquisition-receipts`
requires `pharmacy.accounts.read`, current private outlet access and exact expected
workspace/account headers. It returns at most 50 original acquisition receipts per
signed keyset page; the cursor binds the actor, pharmacy, outlet and filters.
Unknown or repeated query fields are rejected. Reads are audited and uncached.
The canonical response is `contracts/pharmacy-accounts.json`; run
`node scripts/generate-pharmacy-accounts.mjs` and `--check`.

Each row retains its original receipt ID/units and immutable batch/catalogue
currency/unit. Supplier receipts add their locally retained order/shipment and
source stock owner (`supply` or `pharmacy`), effective accepted quantity and
receipt revision, current receiving summary revision, closure and discrepancy
count. They disclose no external Staff identity, patient or source balance.
A current correction does not rewrite the original invoice cost basis. Missing
required supplier evidence fails with 503 rather than appearing as a direct or
fully matched receipt. A recorded payable link is separate from a commercial match.

The accounts bill form uses this financial reader instead of the stock journal.
It offers only uncosted receipts in the bill currency. Invoice amounts, tax and
recoverable tax must come from the accountable original document. Receiving,
quarantine clearance and physical release do not create a bill, credit or payment.
Existing immutable payable, credit/payment/reversal and valuation ownership stays
in PharmaConnect; existing unique receipt/document/source-reference constraints
prevent duplicate costs and recorded settlements. These are external payment
records, not provider money transfers.

Bill saves pin the submitted ID, account, scope and normalized input. Ambiguous
outcomes freeze edits and cancellation until exact original GET recovery. If that
read currently returns 404, an explicit retry resubmits the same input/ID; it does
not assume the first request can no longer commit. Recovery compares invoice
fields, line/receipt amounts and original actor while permitting later balance
changes from settlements. Existing v1 bill routes accept legacy callers without
an expected-subject header; when supplied, that header must match before a write
or recovery read. Payable reads now record financial-read audit.

Additive migration `0043_pharmacy_acquisition_receipts` creates a SELECT-only
accounts purpose over retained receipt/head/correction evidence. It does not
change physical-write policies or grant finance permissions to operating duties.
No existing rows or prior migration are changed. Rollback removes the new reader
and UI while preserving recorded financial and physical evidence; migration
rollback does not delete or reinterpret history.

The candidate's unit/SDK/component tests, real owner database/broker tests and
production-shaped browser test are separate gates. The browser financial test
uses direct synthetic acquisition, never a fictitious trade licence. Positive
live bilateral dispatch/receiving still needs actual reviewed participant,
Maharashtra/Odisha state/category and trading-policy prerequisites. See the
[implementation design](../../superpowers/specs/2026-11-09-supplier-financial-handoff.md).

## Initial organisation business profile

Customer onboarding stores organisation contact, headquarters and main-branch
address/name on the pharmacy owner through `customercontract.OrganizationProfile`.
Migration `0044_pharmacy_business_profile` adds the nullable bounded
`onboarding_profile` column to `pharmacy_organizations`. A complete profile creates
the initial outlet in `pending_review`, using the organisation UUIDv7 in its own
outlet scope and the branch's supplied name.

`GET /v1/pharmacy/pharmacies/:pharmacy/business-profile` requires
`pharmacy.access.manage`, verified tenant membership and pharmacy visibility. It
returns `{profile: object | null}`, enforces the optional expected-tenant
precondition, disables caching and audits the read. Historical records can return
null. The read schema is generated from the onboarding-service owner contract.
[Automated onboarding](../../design/automated-customer-onboarding.md)

## Add an outlet product from the medicine master (#1683)

See [drug master operations](../../drugmaster.md#outlet-catalogue-consumer) for the
scoped search/import contract and release provenance. Drugmaster remains the
identity owner; Pharmacy stores immutable, version-linked identity evidence and
outlet commercial fields. Manual products remain supported. Import requires current
editor access, rejects stale/inactive source versions and does not replace local
schedule, tax, category, price-control or stock release reviews. Source transport
can fetch/stage licensed releases; live vendor activation remains dependent on
procurement and independent stewardship. #1683 is not complete until an approved
Indian branded release is evidenced in production.

## Outlet reorder levels and low-stock warnings (#1685)

This additive implementation is protected by `pharmacy_stock`, which depends on
`pharmacy_catalogue`. Neither capability is enabled by this change; passing the
new tests does not constitute acceptance of all stock/catalogue features.

The overview API is also gated by `pharmacy_stock`; Catalogue alone cannot
expose its stock counts. The overview page retains its existing `pharmacy_reports`
visibility gate, which already depends on Stock.

All paths below start at
`/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`. Each request verifies the
current principal, tenant, pharmacy and outlet grant. GET requires
`pharmacy.stock.read`; PUT also requires `pharmacy.catalogue.write` and an editor
grant. A tenant or hospital affiliation does not replace that grant.

| Method/path                                             | Input and result                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET `/catalogue/products/{id}/reorder-level`            | `{level: receipt                                                                                                                                                                                                                                                                                               | null}`; an existing product with no level returns null; an inaccessible product returns 404. |
| PUT `/catalogue/products/{id}/reorder-levels/{command}` | UUIDv7 command, strict `{minimum_units, expected_version, product_version}`. Minimum is an integer 0–1,000,000,000 issue units or explicit null to disable. All fields are required. Initial expected version is 0; product version is the current catalogue version. Returns the immutable receipt, HTTP 200. |
| GET `/catalogue/products/{id}/reorder-levels/{command}` | Returns the immutable receipt under current read authority, or 404. This is recovery after an ambiguous PUT; receipt remains the original revision after later changes.                                                                                                                                        |
| GET `/stock/low-products`                               | Standard bounded keyset envelope; only `limit` and `cursor`, each once and nonempty. Default 50, maximum 200. No historical stock or automatic ordering.                                                                                                                                                       |

Canonical command, receipt and list-row fields derive from
`contracts/pharmacy.json`. Receipt carries the command/product/outlet IDs,
minimum, expected/product/result versions, issue unit and UTC recorded instant.
List rows identify the product by `id`, with outlet, SKU, brand, unit, minimum,
exact integer-string available units, observed instant, outlet-local date and
timezone. UI labels use names and SKUs. No patient or Staff profile is disclosed.

Configuration history is append-only and audited in its write transaction.
Migration 0052 also installs the standard restrictive operating-choice policies
on this table and extends the integrity fingerprint only after the existing
fingerprint passes. Adding a table must not disable native location choices or
allow a governance-selection purpose to read or modify stock configuration.
Writes serialize on the product row; a database trigger checks active product,
product version/unit, actor and next revision. Repeating the same command, input
and actor returns its original receipt. Changed reuse and stale product/level
versions return 409 `conflict`. Missing/invalid fields return 400;
permission denial is 403 and inaccessible scoped objects are 404. No event,
stock movement, payable or order is created.

The list and additive `overview.stock.low_stock` count share one projection.
Only active products with a configured, non-null minimum in the current issue
unit participate. Zero is configured and warns at zero availability. Products
without batches are included. A unit change before any batch exists invalidates
the old-unit configuration; the editor identifies it and requires a new save.
Inventory already freezes product identity after a batch exists.

Availability reuses `pharmacy_sellable_buckets`, as the stock balance does:
sellable locations, MRP present, expiry strictly after outlet-local today, no
safety incident, and live supplier holds subtracted. It is not a dispensing
permission; product classification, licences, tax and sale checks still apply.
Equality is low stock. Counts/quantities are never substituted with zero when
an owner query fails. Lists have a 10-second request deadline and 8-second SQL
budget; configuration lookup uses the latest indexed revision for each product.
Different requests can differ when stock, reservations or configuration changes.

[Operator guide](../../guides/pharmacy-low-stock.md) ·
[Design and release gates](../../superpowers/specs/2026-11-20-outlet-low-stock.md)

## Reviewed outlet stocktakes (#1686, implementation candidate)

The draft implementation adds a frozen physical-book snapshot, versioned
counted observations, reviewed correction posting and a Stock workspace. It is unreleased and
protected by `pharmacy_stock`; capability eligibility and provider values are
unchanged. Reviewed decisions and atomic corrections have passed the native core
journey, including audit/outbox failure rollback, exact retry, register integrity
and sellable/quarantine adjustment cases. The SDK and UI cover start, count,
review, status, exact-command recovery and verified register downloads.
Native both-owner, Global API, reservation-conservation and atomic failure/retry
journeys have passed. Final CI, deployment and live UI acceptance remain required
before operational activation.
See [ADR-0073](../../adr/0073-pharmacy-stocktake.md).

Paths below use the same pharmacy/outlet prefix as reorder levels. Every request
requires verified tenant, pharmacy and current outlet scope through Global API.
Reads require `pharmacy.stock.read`; starting requires `pharmacy.stock.correct`
and a current editor grant. Ownership or affiliation alone is insufficient.

| Method/path                                                 | Contract                                                                                                                                                                                                                                                                                                                                                                                |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| PUT `/stock/stocktakes/{stocktake}`                         | UUIDv7 identity; strict `{selection, product_ids, quarantine_location_id, reason}`. `all` requires an empty product list; `selected` requires 1–100 unique UUIDv7 product IDs. Required reason is at most 500 characters; body at most 64 KiB. Returns the immutable creation receipt.                                                                                                  |
| GET `/stock/stocktakes/{stocktake}`                         | Current sealed snapshot and progress; no query or request body.                                                                                                                                                                                                                                                                                                                         |
| GET `/stock/stocktakes`                                     | Standard keyset list; only one nonempty `limit` and `cursor` allowed, default 50 / maximum 200.                                                                                                                                                                                                                                                                                         |
| GET `/stock/stocktakes/{stocktake}/lines`                   | Same pagination; immutable batch/location book quantities, issue units, product version, snapshot time and retained schedule declaration. Missing physical counts are null.                                                                                                                                                                                                             |
| PUT `/stock/stocktakes/{stocktake}/counts/{command}`        | UUIDv7 command; strict `{expected_version, counts: [{line_id, counted_units}], reason}`. Version zero and physical zero are explicit values. At most 100 unique lines per command, quantities 0–1,000,000,000,000 issue units, required reason at most 500 characters and body at most 64 KiB. Returns the immutable original receipt.                                                  |
| GET `/stock/stocktakes/{stocktake}/counts/{command}`        | Original count receipt under current read authority; no query or body. This does not return a later edit's quantities.                                                                                                                                                                                                                                                                  |
| PUT `/stock/stocktakes/{stocktake}/decisions/{command}`     | Strict `{decision, expected_version, reason, extra_units_disposition?}`. Decision is submit, approve, reject or cancel. Only approval requires a disposition: `moved_to_quarantine` if any difference is positive, otherwise `no_extra_units`. Required reason at most 500 characters; body at most 64 KiB. Returns an original immutable decision receipt, including adjustment count. |
| GET `/stock/stocktakes/{stocktake}/decisions/{command}`     | Original immutable decision receipt under current read authority; no query or body.                                                                                                                                                                                                                                                                                                     |
| PUT `/stock/stocktakes/{stocktake}/cancellations/{command}` | Cancellation-only retained-work continuation; same decision schema but only `decision: cancel` is accepted. Requires current correction permission and outlet editor authority, an existing sealed count, current version and original actor/input binding for replay. No observation or movement can be posted through this path.                                                      |
| GET `/stock/stocktakes/{stocktake}/register.csv`            | Audited physical-count register, bounded to 10,000 buckets / 8 MiB with no partial file. No query or body. Current read authority is required, as for the existing stock register CSV.                                                                                                                                                                                                  |

Start captures quantities and movement counts in one SQL statement, including
expired and non-sellable physical buckets. At most 10,000 buckets are permitted;
larger selections fail rather than truncate. Snapshot insertion and sealing are
atomic with audit evidence. The quarantine destination must exist in the same
outlet and cannot be sellable. Snapshot headers and lines are immutable after
sealing. Only one counting or submitted stocktake may exist per outlet. Posted,
rejected and cancelled counts remain retained and allow a new snapshot.

Exact start retries bind the original actor and normalized input; changed reuse
returns 409. Reads and recovery are audited in the same transaction and return
503 `audit_unavailable` if audit evidence cannot be retained. Inaccessible scope
is 404, missing permission 403, invalid input 400. Migration 0053 adds forced RLS
and restrictive operating-choice purpose policies, extending the policy
fingerprint only after validating the existing fingerprint. Canonical models
are generated from `contracts/pharmacy.json`; registry parity does not prove
native database or deployed acceptance.

Count observations are append-only. A command serializes on its sealed parent,
requires the exact current version and locks submitted ledger buckets in a
consistent order before recording. A database guard rejects a changed movement
count, even when its net quantity is unchanged. It verifies that all entries
belong to the same parent and original command transaction; a deferred guard
rejects a command with missing entries. Failure rolls back the whole command.
Exact actor/input replay returns its original version and quantities after later
edits; changed reuse is 409. Current progress and line observations use one
statement snapshot per request and preserve zero separately from missing. Starting
again with the original identity still returns the original creation receipt.
No observation changes stock or publishes a stock-movement event. Snapshot,
counting and core approval journeys have passed native tests; the operator UI,
failure/recovery cases and live acceptance are separate release requirements.

Submit requires an observation for every frozen bucket. Only counting permits
new observations or submission. Cancellation is available while counting or
submitted. Approval and rejection require submitted state and a reviewer who is
neither the starter nor any historical counter, including superseded counters.
Every action uses the exact current version. Terminal states cannot be reopened.
Exact original-command recovery remains valid after later decisions.
Detail includes `positive_difference_rows` from the entire current count, not
just its visible page. `review_relationship` describes the current actor as
starter, historical counter or independent; it is a presentation fact and grants
no permission or professional authority. Original creation receipts always retain
zero progress and the original actor's starter relationship.

Approval applies each nonzero difference to the current balance, preserving
movements after the accepted observation. Negative differences remove stock from
the counted location; positive differences go to the named quarantine location
after the reviewer's explicit physical-transfer attestation. The existing
reservation and overdraft guards remain authoritative. A narrowly linked native
movement guard allows only the approved shortage correction outside quarantine;
ordinary unlinked corrections retain the quarantine-only rule. Every correction
in this review is supported by its native adjustment link; existing free-text
source references remain valid and never create reviewed evidence on their own.
Each reviewed correction
is linked to its counted bucket and review command, uses the existing `correction`
movement kind, and publishes only protected stock references through the bounded
transactional outbox. This command does not issue a release, disposal or financial
posting command; movement consumers remain subject to their published contracts.

At most 500 nonzero corrections, each within the existing ±1,000,000,000 issue-unit
movement limit, may post in one approval. Excess differences return 409
`stocktake_posting_limit` without truncation or partial posting. Retain genuine
observations and ask the responsible stock owner to resolve the limit; never
alter a physical count merely to make posting succeed. Native version, transition,
independence, quantity, linkage, audit and outbox failure all roll back the whole
decision, leaving its identity available for a safe exact retry.
Audit or outbox write failures return 503 `stocktake_evidence_unavailable` without
revealing private database diagnostics; a failure is never reported as a completed
stock adjustment or a caller's invalid catalogue input.

The physical-count CSV retains the book, observation, issue unit, schedule
declaration and linked correction. H1, X and NDPS have separate sections; a row
with NDPS=yes uses the NDPS section while retaining its source schedule class.
Unknown declarations and NDPS status remain explicit. Other declared classes
appear in the ordinary report section without changing their dispensing rules.
Missing observations are blank, physical zero is zero. Actor/movement references
are retained for backend audit correlation; UI account presentation uses names.
The file is a physical stocktake report and does not replace statutory controlled
drug filings. The existing stock register retains its six movement totals and
includes these corrections without a new movement kind.

Export locks the sealed parent for the short read transaction so its revision,
observations and corrections cannot be mixed across a review. It buffers the
complete bounded file, neutralizes formula-like text cells and retains read audit
before returning any data. A missing bucket returns 503; excessive rows/bytes
return 413 `stocktake_export_too_large`, with no truncated CSV. The retained paged
record remains available for examination. Responses are non-cacheable attachments
named by the outlet's human stocktake number. Export does not change stock.
Responses retain `Cache-Control: private, no-store` through Global API and include
`X-Stocktake-Tenant-Id`, `X-Stocktake-Outlet-Id`, `X-Stocktake-Id`,
`X-Stocktake-Version`, `X-Export-Row-Count` and `X-Export-Sha256` (lowercase hex
SHA-256 of the actual CSV bytes). Download clients bind these to the displayed
record, enforce the byte bound while reading, and verify the digest before
offering a file. A concurrent revision requires refresh rather than exporting a
different revision under the displayed one.

### Stock workspace operation (unreleased source)

1. In the selected pharmacy/outlet's Stock workspace, choose **Stocktakes**.
   Starting and counting require `pharmacy.stock.correct` and the current outlet
   editor grant. Catalogue-edit permission alone does not show these actions.
2. Start for all products or select up to 100 products. Choose the named
   quarantine destination and supply the count reason. The start freezes the
   book; it does not freeze normal stock operations or count later new batches.
3. Open each bucket's count entry. Quantity starts blank even when a previous
   observation exists. Enter an actual whole physical quantity and reason; zero
   must be explicit. Existing observations are shown separately. Save and refresh
   the current version before continuing. A conflicting intervening movement
   requires examining/cancelling the count, not silently rebasing its frozen book.
4. After all buckets are counted, submit for independent review. The original
   starter and every historical counter are ineligible to approve or reject.
   The reviewer checks the full count, including positive differences beyond the
   currently loaded page, and explicitly attests physical quarantine placement
   for all extra units before approval. Cancellation and rejection retain history.
5. A lost save response retains the original command and input and locks edits.
   Check the saved result before retrying. A not-found recovery permits only an
   identical retry. Start recovery reads the current sealed record and verifies
   its original starter and immutable start fields, even after later revisions;
   it neither starts a replacement nor adopts another person's count. A changed
   version never resets a dirty form silently.
6. Download the register from the current detail. The client verifies the actual
   bytes and displayed revision before offering a file. The detail separates
   loaded H1, X, NDPS, other declared and unknown differences; it explicitly labels
   the loaded subset, while the file includes the complete bounded snapshot.

All UI actors use authorized named-account presentation. Internal subjects,
command identifiers and tenant IDs remain request/recovery bindings. Unavailable
identity or count data is explicit; new commands pause on unavailable status.

The sealed-count list/header and original count/decision receipt reads are
baseline continuations. Their product authorization and read audit remain in
force even when Stock is off. The cancellation-only path is also baseline; the
general decision path, start, count entry, snapshot-line reads and CSV export
remain Stock-gated. The **Retained stocktakes** page (`/pharmacy/stocktake-recovery`) is classified
under `pharmacy_setup` and requires stock-read permission plus the existing
pharmacy directory and outlet authority. It remains reachable to stock staff who
cannot manage access. Read-only outlet staff cannot cancel; correction permission
and a current editor grant are required. Managers also have the same panel in the
selected outlet's **Outlets & access** (`/pharmacy/setup`) detail. Both pages are independent of Stock and Reports;
the overview is not a recovery entry point. The panel uses the selected page's outlet or the opened setup detail's outlet, which can differ
from the working outlet. It reads status and offers
cancellation only. A displayed cancellation action still needs
the server's current outlet editor check; a route permission is not a scope grant.
The panel never fetches live stock, snapshot lines or an operational catalogue.

Unknown stocktake outcomes retain at most eight tab-local recovery markers per
verified tenant, actor, pharmacy and outlet. Markers contain only operation kind,
stocktake reference and command reference, never input quantities or reasons.
Storage must persist and verify a marker before the command is dispatched;
unavailable or full storage blocks new commands. Reload recovery is read-only and
checks the original actor before acknowledgement. A not-found result remains
unknown: the original request may still finish. Explicit dismissal refreshes
current state before new operations; it does not undo or assert failure of a
command. With retained markers, new Stock actions pause while an already active
form can recover its exact attempt. Confirmed outcomes clear only their matching
marker. The same recovery panel is available in the selected outlet's setup
detail when Stock is disabled.

Stocktake SDK requests send `X-Helivanta-Expected-Subject` alongside the existing
expected-tenant precondition. The server returns `409 session_changed` for a
supplied blank, duplicate or mismatched expected subject before resource access;
missing subject remains compatible with older/native callers. Identity and
current outlet authority always come from the verified principal, never these
headers. Actor-specific query keys and recovery storage prevent another account
in the same tenant from adopting a pending command. Write receipts must match the
original actor; authorized historical receipt reads remain available.

## Physical quarantine holds (#1687, unreleased source)

PharmaConnect owns these commands and both stock buckets. This is the physical
hold prerequisite for supplier returns, not supplier dispatch or a financial
settlement. Supplier returns, credit matching/allocation and their printable note
remain in development; see [ADR-0075](../../adr/0075-pharmacy-supplier-returns.md).
Migration `0054_pharmacy_stock_holds` has not been approved for live application.
No availability or role grants are enabled by this source change.

The Global API forwards
`/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/stock/holds/{hold}` to the
owning product. `hold` is a UUIDv7 command reference.

- `PUT` requires `pharmacy.stock.quarantine`, verified tenant membership and
  current editor access to the exact pharmacy/outlet. Its strict canonical
  `StockHoldInput` contains `batch_id`, `from_location_id`,
  `quarantine_location_id`, `quantity_units`, `reason` and
  `physical_disposition: moved_to_quarantine`. IDs must be UUIDv7, locations must
  differ, quantity is 1–1,000,000,000 issue units, and the required reason is at
  most 500 characters without control characters. Request bodies are limited to
  64 KiB. Queries, duplicate fields and unknown properties are rejected.
- `GET` requires `pharmacy.stock.read`, current exact-outlet read access and the
  original actor. It recovers the immutable `StockHoldReceipt`, including both
  movement references, actor and timestamp. No request body or query is accepted;
  successful recovery is audited and `private, no-store`. Recovery is classified
  as baseline so an operational release lock does not erase uncertain commands.

The source location must be sellable and the destination quarantine in the same
outlet. The hold does not require that an unsafe or expired batch be releasable
for sale. It creates an immutable parent and signed `quarantine_hold` movements:
negative from the source, positive into quarantine. Existing reservation and
balance guards remain authoritative; holds cannot consume an active supplier
reservation. Both movement events and audit evidence use the same transaction.
Database guards reject forged movement references and incomplete pairs at commit.
The hold table also has restrictive location-choice purpose/delete policies; the
prior operating-choice integrity fingerprint is validated before migration and
extended with the new policies afterward. Native Staff location selection remains
fail-closed without exposing hold records through that read purpose.
Releases and holds acquire both bucket locks in the same order.

Identical replay by the original actor returns the original receipt without
moving stock again, even after later stock changes. Reusing an identity with
changed intent or actor is `409`; a missing or foreign-scope object is `404` and
missing route permission is `403`. Supplied expected-tenant/subject headers are
checked before resource access; a changed account is `409 session_changed`.
Authority/evidence dependency failures fail closed; no stock success is returned.
Both commands have a 15-second transaction deadline. Retain the original command
reference when a response is uncertain and recover it before creating another.

Stock positions, reconciliation, CSV and the stock register include the signed
`quarantine_hold_units` contribution. Missing totals remain explicitly unavailable
rather than being presented as zero or as a reconciled balance. Hosted acceptance
covers owner models, scoped denial, exact recovery, concurrent holds, supplier
reservations and evidence rollback. Those tests do not establish live deployment,
regulatory return authority or supplier receipt of goods.

## Supplier return drafts (#1687, unreleased source)

The draft slice adds `PUT/GET /accounts/supplier-returns/{return}` and
`PUT/GET /accounts/supplier-returns/{return}/cancellations/{cancellation}` under
the existing pharmacy/outlet prefix. These handlers are implemented in source and remain unreleased pending
hosted and live acceptance. Migration
`0055_pharmacy_supplier_return_drafts` is not approved for live application.

`pharmacy.supplier_returns.prepare` permits creation/cancellation and
`pharmacy.supplier_returns.read` permits private reads. Module grants name pharmacy
owners and purchasing staff; the platform grant reconciler also adds tenant
administrators to every registered permission. This slice preserves that existing
platform policy. Each request still requires verified
tenant membership and a current exact-outlet reader/editor grant. Both expected
subject and expected tenant headers are mandatory intent fences; supplied IDs or
headers never grant authority. These legacy outlet grants do not by themselves
prove reviewed Staff employment or clinical/financial posting authority.

A bounded draft contains one original payable, at most 100 original receipt/bucket
lines, positive issue units, explicit expected credit in the payable's currency
minor units, and a reason. It freezes the original supplier, bill, product
revision, brand/generic labels, issue unit, lot, expiry and source receipt facts.
Only original inventory lines from that payable qualify. Platform-supplier
receipts are rejected until an acknowledged reverse protocol exists. Duplicate
receipt/bucket pairs, quantities beyond original receipt/available quarantine
stock, and credit expectations exceeding the bounded total are rejected.

Creation records **no stock movement, stock reservation or settlement**. Fully
paid bills remain fully paid. Immutable database evidence requires the complete
line set and exact source facts; failed audit writes roll back the whole draft.
Its per-outlet number is a commercial reference, not a statutory tax document.
Identical replay by the original actor retains the original creation receipt,
including after later cancellation. Changed intent or actor is `409`.

Cancellation uses a separate UUIDv7 command, expected version and reason. It
preserves the original draft, appends its own attributable receipt and changes
the read view from draft/version 0 to cancelled/version 1. Recovery is limited to
the original cancellation actor with current read authority. Repeated or stale
cancellations cannot erase or change evidence. Private business reads and recovery
are audited and fail closed if their audit cannot be retained. Read/cancellation
routes remain baseline-classified when operating availability is disabled.

Bodies are strict, bounded to 64 KiB and reject duplicate/unknown fields and query
parameters. Commands and reads have a 15-second deadline. Resource reads return
one guarded object with no more than 100 lines; collection pagination exemptions
must document that bound. The new tables also participate in the restrictive
location-choice purpose policies and integrity fingerprint.

Dispatch, actual supplier-credit recognition/matching/allocation, printable notes,
UI integration and complete live acceptance remain outstanding. The account-bound
SDK validates outlet, actor, command, original receipt quantities and explicit
credit totals without retrying uncertain commands under a new identity. The expected
credit is not an actual credit note, refund, GST/ITC decision or GL posting.

### Supplier return history and cancellation workspace (unreleased)

`GET /accounts/supplier-returns` under the same pharmacy/outlet root is a
private, audited cursor collection. It accepts only single nonempty `limit` and
`cursor` parameters and no body. Platform limits are 1–200 (default 50); the
workspace requests 25. Summaries use canonical `SupplierReturnSummary`, omit actor
subjects and source-line bodies, and project current cancellation state without
changing the stable original creation receipt. Audit failure returns 503 without
rows. Verified membership, return-read permission, current exact-outlet access
and both expected-account fences remain mandatory.

The accounts workspace adds a permission-controlled Supplier returns tab behind
the existing pharmacy-payables release gate; its panel classification is
`pharmacy.supplier_return_history`. Return numbers, named suppliers, original
bill references, product names, batch, expiry and units replace raw identity
labels. Preparation dates use the outlet timezone. Expected credit is explicitly
not an accepted supplier credit or bill settlement. No creation, dispatch, credit
allocation or tax-document action is exposed by this history view.

An authorised current outlet editor can cancel a draft with a reason and the
existing versioned immutable command. The UI first retains only the command
reference in session storage scoped to tenant/subject/pharmacy/outlet/return.
Storage failure prevents submission. Response loss or reload preserves recovery
of the same actor-bound command; a confirmed missing result permits an explicit
retry with the same reference and original reason. It never creates a fresh
command to resolve an uncertain write. Read authority permits recovery even if
preparation authority has subsequently been removed. Changes of account reset
the panel and use separate account-scoped cache keys. Unsaved/uncertain commands
participate in the workspace navigation guard.

See the [history design](../../superpowers/specs/2026-11-21-supplier-return-history.md)
and [delivery plan](../../superpowers/plans/2026-11-21-supplier-return-history.md).
Hosted native and browser acceptance and approved live rollout remain required;
this addition does not complete #1687 or enable any locked operational capability.

### Attributable supplier return dispatch (#1784)

`PUT /pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns/:return/dispatches/:dispatch`
requires `pharmacy.supplier_returns.dispatch` (owner/purchasing defaults; the
platform also grants tenant administrators), verified expected tenant and subject,
and current exact-outlet authority. It is Stock-gated. Input is canonical
`SupplierReturnDispatchInput`: expected draft version zero, current reviewed
outlet licence, commercial outward reference, recipient name, explicit handoff
instant, `handed_to_supplier` disposition and reason. Unknown, X or NDPS product
classifications refuse dispatch; outlet evidence does not certify supplier drug
licensing, statutory transport paperwork or tax treatment. See ADR-0075 for the
operational release-review boundary.

The whole draft dispatches atomically. Each line has one dedicated negative
`supplier_return` quarantine movement, its current approved schedule declaration
and original receipt lineage. Cumulative dispatch across all drafts cannot exceed
that receipt's original units; available balance and reservations remain enforced.
Partial movement, duplicate dispatch, cancellation after dispatch and dispatch
after cancellation are refused. Expected credit, payables, payments and actual
supplier credit are unaffected. No stock is silently restored by financial edits.

`GET` of that exact command requires return-read authority and the original
actor. It is private/no-store and audited, and remains baseline-available for
response-loss recovery when new Stock writes are paused. Exact PUT replay returns
the original receipt; changed intent or actor conflicts. Current return detail and
history report `dispatched`/version one; original creation replay stays draft/zero.
Outward references and recipient names are private business evidence; stock events
continue publishing protected references only. Stock register, reconciliation and
CSV expose signed `supplier_return_units` separately from hospital fulfilment
`supplier_dispatch_units` and customer `return_units`.

Canonical schema: `contracts/pharmacy.json`; owner: pharmacy. New append-only
migration: `0056_pharmacy_supplier_return_dispatch`. The Shell provides a Stock-gated physical dispatch form with
readable reviewed licence choices, explicit handoff evidence and actor-scoped
reference-only recovery storage. It hides failed or wrong-outlet licence results
and keeps recovery available when new writes are paused. Browser testing includes
response loss, reload, exact original recovery and unchanged financial balance.
Return preparation, actual credit, allocation, reversal and printable-document
workflows remain tracked in
#1784. This contract is not evidence those remaining interfaces are delivered.

### Supplier return preparation sources and original command recovery

Preparation lookups refuse bills without original inventory receipt lines with
404; unrelated expense bills cannot be disclosed through this projection.

Under `/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns`:

- `GET /original-bills` is an audited, account-bound cursor page capped at 50.
  It requires preparation authority and Stock availability and includes only bills
  with inventory receipts, including paid bills. The projection exposes bill and
  supplier labels, issue date and currency rather than unrelated expenses.
- `GET /preparation/:payable` requires `pharmacy.supplier_returns.prepare` and
  Stock availability. It returns canonical `SupplierReturnPreparation`: verified
  original bill labels, at most 100 original inventory receipt projections,
  committed dispatched units and a database-issued `stock_as_of`. It accepts no
  query/body. Fully paid bills are supported. Platform-supplier origins are
  explicitly identified as requiring their separate reverse protocol.
- `GET /:return/preparation` requires return-read authority and the original
  preparing actor. It returns the immutable creation receipt, even after later
  state transitions. It is baseline-available for response-loss recovery; this
  receipt does not assert the current return state.

Both reads require the expected tenant/subject and exact current outlet authority;
responses are private/no-store and fail unavailable if their audit cannot persist.
The shared original-source query is also used for draft validation. Previously
dispatched units reduce the observed original-receipt allowance; concurrent drafts
still reserve neither original quantities nor stock. Dispatch rechecks the cap
under locks. Stock positions now honor an explicitly supplied expected-subject
precondition, alongside their existing tenant precondition; callers omitting the
optional subject retain their previous authorization behavior.

The Shell preparation builder uses readable original bill, product/batch/expiry
and quarantine choices, supports multiple lines and explicit expected credit, and
stores only an account-scoped recovery reference. Preparation is Stock-gated;
original-result recovery survives a pause. See the supplier-return QA guide for
implemented scenarios and remaining financial/document acceptance. Actual accepted
credit, allocation, reversal and printable commercial notes remain distinct work.

### Private commercial return note

`GET /pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-returns/:return/note.html`
requires `pharmacy.supplier_returns.read`, active exact-outlet authority and both
expected tenant/subject headers. It accepts no body or query. A draft, undisclosed
return or missing dispatch is 404. Incomplete proof, missing historical name
snapshot, failed audit or timeout returns 503 with no file disposition or bytes.

Success is a private/no-store UTF-8 HTML attachment, not JSON:
`attachment; filename="supplier-return-<number>.html"`. It contains frozen
pharmacy/outlet names, supplier and original bill, outward reference, recipient,
UTC handoff time, products, lots, expiries and returned issue units. Migration0057
captures directory names at dispatch; later renames do not rewrite the note.
Pre-migration rows without that snapshot are deliberately unavailable.

`X-Supplier-Return-Tenant-Id`, `X-Supplier-Return-Outlet-Id`,
`X-Supplier-Return-Id` and `X-Supplier-Return-Number` identify the source;
`X-Export-Row-Count`, `X-Export-Byte-Count` and `X-Export-Sha256` prove completeness
and byte integrity. The file is capped at 100 lines / 1 MiB. Dynamic text is
escaped, content policies prohibit active/external content, and audit commits
before release. Current scope and all metadata/bytes are checked before the
browser creates a download. Download is baseline retained evidence and changes
neither stock nor financial balances. Print/PDF uses the downloaded document's
browser print view. It does not issue a statutory or supplier credit document.

### Retained supplier-credit documents (candidate)

Under `/pharmacy/pharmacies/:pharmacy/outlets/:outlet/accounts/supplier-credits`:

| Method/path                          | Meaning and permission                                                          |
| ------------------------------------ | ------------------------------------------------------------------------------- |
| `GET /`                              | Cursor page of current positions, maximum 50; `pharmacy.accounts.read`          |
| `PUT /:credit`                       | Record actual document or explicit correction; `pharmacy.accounts.adjust`       |
| `GET /:credit`                       | Current document position; `pharmacy.accounts.read`                             |
| `GET /:credit/recovery`              | Original recording receipt, original actor only; `pharmacy.accounts.read`       |
| `GET /:credit/void`                  | Retained void evidence; `pharmacy.accounts.read`                                |
| `PUT /:credit/voids/:entry`          | Void unused/unmatched credit at an explicit version; `pharmacy.accounts.adjust` |
| `GET /:credit/voids/:entry/recovery` | Original void receipt, original actor only; `pharmacy.accounts.read`            |

These routes are registered in the development candidate, not deployed or
operationally released. Inputs and responses derive from `contracts/pharmacy.json`.
Every route requires verified expected tenant/subject headers, current membership
and exact active outlet authority. Writes require the pharmacy-payables release
capability; retained reads use baseline availability and still reauthorize and
audit each disclosure. A reader may inspect another authorised actor's historical
record, but cannot recover that actor's original command receipt.

The recording command uses a UUIDv7 credit identity, dispatched `return_id`,
normalized supplier document reference, `issued_on`, explicit positive INR
`amount_minor` (at most 10^12), reason and optional `correction_of`. Source supplier,
outlet, original bill and currency come from the dispatched return, never from
caller-supplied supplier metadata. Dates start in 1970, cannot precede the original
bill and cannot exceed the database-authoritative current outlet date. Duplicate
references serialize within the outlet/supplier scope. A correction must name an
unused, unmatched voided document for the same supplier; one successor preserves
the original record as corrected.

Recording actual credit neither matches a return nor allocates a bill. In the
document-only migration0058 stage, `matched_minor` and `allocated_minor` are zero.
The migration0059 matching candidate below advances net matched values separately.
An active
record's available pool is its actual amount less allocation; a voided/corrected
record has zero matched, allocated and available amounts. Voiding starts from an
explicit `expected_version` and reason, preserving the original recording receipt.
Current views and immutable command receipts are distinct contracts.

Same-ID, same-actor, same-intent replay returns the original receipt; a conflicting
identity/source/state returns 409 `supplier_credit_conflict`. Invisible records or
another actor's recovery return 404. Audit/dependency/projection failure returns
503 `supplier_credit_outcome_unknown`; no private result or partial write is
released. Recover the original reference after uncertainty before any fresh intent.
Audit, immutable ledger, protected projection and minimal outbox event commit in
one owner transaction. Event fields have explicit privacy classifications and
exclude names, amounts, document references, actor and reason.

The SDK verifies exact resource/outlet identity, original actor/intent on writes
and recovery, current amount conservation, void version arithmetic and bounded
cursor integrity. Requests are account-bound, private/no-store and cancellable
with a 15-second limit; they never automatically retry a financial command.
The migration0060 allocation candidate below adds the owner journal and payable
balance integration. Source selectors, financial UI and full hosted/live
acceptance remain unfinished. No stock-cost, GST/ITC or general-ledger
effect follows document recording or event publication.

### Supplier-credit matching and full reversal (candidate)

Under the same credit prefix, `/accounts/supplier-credits/:credit/matches`:

| Method/path                             | Meaning and permission                                             |
| --------------------------------------- | ------------------------------------------------------------------ |
| `GET /`                                 | Current match history, cursor maximum 50; `pharmacy.accounts.read` |
| `PUT /:match`                           | Explicit partial attribution; `pharmacy.accounts.adjust`           |
| `GET /:match`                           | One match with optional unique `reversal_id`; accounts-read        |
| `GET /:match/recovery`                  | Original actor-bound match receipt; accounts-read                  |
| `PUT /:match/reversals/:entry`          | Full attributed-amount reversal; accounts-adjust                   |
| `GET /:match/reversal`                  | One retained reversal, if any; accounts-read                       |
| `GET /:match/reversals/:entry/recovery` | Original actor-bound reversal receipt; accounts-read               |

These are development routes, not a completed financial workspace or live release.
The same verified tenant/actor, exact outlet, operating-purpose and audit rules
apply. New writes are payables-gated; retained history/recovery remains baseline
under current authority. Inputs use the existing strict 64 KiB command decoder
without query parameters, UUIDv7 command keys and a 15-second deadline.

A match requires `return_id`, current credit `expected_version`, explicitly
observed `expected_return_matched_minor`, positive `amount_minor`, reason and
optional `overage_reason`. The source must have complete immutable dispatch proof
and the same supplier, outlet and currency as the credit. Both the document's
net matched value and each observed return total are bounded at 10^12 minor units;
attribution cannot exceed the actual document value. Attribution exceeding the
return's expected credit requires a nonempty explicit overage rationale.

One document can cover multiple returns in explicit parts; multiple documents
can cover one return. Credit-lock then return-attribution-lock ordering serializes
these commands. The observed return-total precondition catches changes made using
another document as well as stale credit versions. Receipts retain the original
return number, expected amount and resulting return matched total. Replaying the
same original actor/identity/intent returns that receipt without reapplying it.

A full reversal supplies the current credit version, observed return total and
reason. It subtracts exactly its immutable original match once. To correct a
partial amount, reverse the match and create a new explicit match. Both entries
remain, and original command recovery remains unchanged after reversal or later
document void. Normal authorised history can show another recorder; original
recovery cannot. History includes reversal identity without an extra request per
row. Current grants and private-read audit are checked before returning evidence.

Migration0059 adds immutable matching/reversal journals and forced tenant/exact-
outlet RLS. Its deferred projection proof reconciles command count, current
version/state, latest actor and net matched amount, retaining document void and
correction checks. Migration0059 alone requires allocated amount zero; the
migration0060 candidate extends the same proof with the allocation journal. Matching/reversal does not consume the available credit
pool, settle liability, reverse a payment or restore physically returned stock.
Audit, projection, journal and minimal outbox notification commit atomically.
Stale totals, duplicate/conflicting intent, invalid sources and missing excess
rationale return 409 `supplier_credit_conflict`; invisible records/recovery return
404; dependency/audit/proof failure returns 503 without partial evidence.

The SDK checks original matching intent, expected/result versions and observed
return-total arithmetic. Reversal submission takes the original match receipt so
it can additionally verify the returned source, human return reference and exact
original amount. Original-result recovery remains actor-bound and reference-only;
normal history retains other authorised actors and reversal references. Both
credit-document and matching collections share bounded cursor/duplicate checks.
New commands are not automatically retried after uncertain transport.

Source selectors, matching UI, bill allocation/reversal and corresponding
Global API/browser/live acceptance remain required in #1784. This candidate does
not activate any release capability or statutory finance policy.

### Supplier-credit bill allocation (candidate)

Under `/accounts/supplier-credits/:credit/allocations`, the registered development
candidate has cursor `GET /` (maximum50), `PUT /:allocation`, normal
`GET /:allocation`, actor-bound `GET /:allocation/recovery`,
`PUT /:allocation/reversals/:entry`, normal `GET /:allocation/reversal` and
actor-bound `GET /:allocation/reversals/:entry/recovery`. Writes require
accounts-adjust and payables availability; retained reads require accounts-read,
current exact outlet authority and verified tenant/subject headers. No flag is
activated and no completed financial workspace is asserted.

Allocation input binds `payable_id`, credit `expected_version`,
`expected_available_minor`, `expected_payable_version`,
`expected_outstanding_minor`, positive `amount_minor`, `occurred_on` and reason.
Inconsistent declared balances/amount or malformed input is400; changed actual
state, currency, supplier, date or source eligibility is409. The target must have
external inventory receipt evidence, the same supplier/outlet/currency and enough
positive liability. Platform-supplier or mixed-platform receipt sources remain
unsupported pending their distinct acknowledgement protocol. A fully paid source
bill can retain its credit for an eligible future bill.

Migration0060 serializes credit then the same payable key used by existing
payments. Credit version/available amount and bill version/outstanding are checked
under those locks. Bill version is creation1 plus legacy settlement, managed
allocation and managed reversal counts; net-zero payment/reversal still changes
it. Current payable wire fields stay unchanged. The payable balance view adds net
managed allocation to legacy credits, and the legacy settlement guard includes
that same net amount before allowing payment/manual credit.

A full allocation reversal restores exactly its original amount to credit and
liability, preserving intervening payments. It binds current credit/bill versions,
observed available/outstanding balances, date and reason; a second reversal is
refused. Original receipts and their recorded post-command versions/balances are
immutable snapshots, not current balances. Same original actor/identity/intent
replays that receipt after later activity; recovery by another actor is404.
Normal authorised history can show another recorder and its unique reversal.

Both journals have forced tenant/outlet RLS, actor/purpose fences, immutability and
audit. The deferred credit proof reconciles all document/match/allocation commands,
versions, latest actor and net amounts. Journal, projection, audit and minimal
outbox notification commit together. Audit/dependency/proof failure is503 with no
partial result; recover the original reference before fresh intent. Neither
allocation nor reversal moves stock, captures/refunds cash or decides tax/ITC/GL.
The SDK verifies allocation intent, actor-bound recovery, exact credit and bill
versions, and both post-command balance equations. Reversal submission takes the
original allocation receipt to verify the original bill, human references,
supplier and full amount. Normal history retains authorised recorders and
reversal references; bounded collections reject duplicate rows and inconsistent
continuation metadata. Transport/dependency errors do not trigger automatic
financial retry. These client checks supplement current server authority.

Current credit context and source choices are read through
`/:credit/context`, `/:credit/eligible-payables` and
`/:credit/eligible-returns` beneath the supplier-credit collection. All require
current finance read authority and exact owner/outlet scope. New eligible-source
choices additionally require Payables release admission; retained credit context
and financial history remain baseline reads under fresh authority. Context includes the
outlet's database-derived local date, timezone and as-of timestamp. Payable
choices expose positive outstanding external inventory bills for the same
supplier and currency, with current credit/payment-inclusive balances and a
bill version. Return choices expose dispatched returns for that supplier and
currency with the current net matched amount. Collections are audited, bounded,
and ordered by the owner's creation timestamp and identifier; supplied choices
never confer command authority. Migration 0061 creates supplier/currency keyset
indexes on existing source tables through the concurrent-index runner.

`/:credit/allocations/:allocation/context` is a retained finance read for an
original allocation and its current credit and bill observations, including a
fully paid bill. `SupplierCreditAllocationContext` binds the three projections;
`SupplierCreditPayableContext` permits zero outstanding while allocation choices
remain positive-only. Both use the same canonical payable fields. Acquire the
credit lock then the existing payable lock, check the original bill's tenant,
outlet, supplier and currency, and audit before returning data. The SDK validates
scope/linkage, original human references, current version lower bounds, balance
conservation and current allocation coverage. A reversed/voided document remains
readable under current authority and never rewrites the original receipt.

`/:credit/matches/:match/context` similarly binds an immutable match and its
reversal reference to the current credit and exact return's total attribution.
That total includes other actual credit documents; the original match receipt
must not be reused as the current return observation. Acquire the credit lock
then the existing return-matching lock, verify the supplier/currency in the same
tenant/outlet and audit before disclosure. `SupplierCreditMatchContext` is a
baseline retained read under current accounts-read authority. The SDK verifies
scope, linkage, expected return value, current version and active match coverage;
reversed/voided context remains available. Native tests cover three pharmacy
ownership models, another credit's matching, stale reversal refusal, exact
current reversal, retained void history, foreign scope and failed-read audit.
Latest hosted execution is required before claiming these scenarios passed.

Hosted SQL/native acceptance, finance UI/report explanations and actual Global API/browser/live acceptance are still required in
#1784.

### Bill-scoped supplier-credit journal (candidate)

`GET /accounts/payables/:id/supplier-credit-allocations` returns the existing
canonical `SupplierCreditAllocationView` collection for one visible bill. It
requires `pharmacy.accounts.read`, verified tenant/expected subject, current
exact pharmacy/outlet authority and a visible parent bill. It is a baseline
retained-history read, not a new-write or release-activation permission. A reader
may inspect another authorised recorder's application. Recovery remains bound
to the original actor through its existing separate route.

The resource collection is bounded to 50 rows per page, ordered by creation time
and identifier descending. Sealed cursors bind the actor, resource path and bill
parameters; unknown query fields, limits above 50 and a cursor from another bill
are refused. Invisible bill/outlet/tenant is 404, missing permission is 403,
changed expected actor is 409 and unavailable authority/audit is 503. Each page's
`list_read` audit names the selected bill under
`pharmacy_payable_supplier_credit_allocations` and must commit before disclosure.
The response retains an application after reversal with its unique reversal
identity; exact reversal evidence is loaded through the existing selected-credit
read, under fresh authority. No per-row reversal query is performed.

The bill UI displays human credit references and authorised recorder names. It
formats application amounts in the parent bill's currency, hides cached private
evidence after current read failures and resets selected details on an account or
resource switch. Current totals come from the owner bill projection, never a sum
of partial history pages. Legacy direct adjustments and managed applications
remain distinct. A reversal retains recorded payments and restores only its
original credit/liability amount. Receipt and reversal UUIDs stay internal.

Forward migration `0062_pharmacy_payable_credit_journal` adds the concurrent
`(tenant_id,outlet_id,payable_id,created_at DESC,id DESC)` index using the existing
runner. No old migration, balance master, write authority or flag changes. Hosted
native tests include hospital-owned, independent and chain scopes, multi-document
pagination, a fully paid bill's reversal, denial/revocation, audit failure and a
small synthetic `EXPLAIN (ANALYZE, BUFFERS)` result. That small plan is not
large-cardinality performance evidence. Hosted execution, visual review and live
acceptance remain separate gates in #1784.

### Supplier-credit entry and retained history UI (candidate)

Accounts includes a Supplier credits tab under current accounts-read authority.
It shows human credit/supplier/bill references and keeps actual amount, return
attribution, bill applications and unused pool distinct. Account labels use the
authorised presentation component instead of displaying recorder subjects.
Current history/detail failures hide cached records and verified-reference
notices. No deployed acceptance or feature activation is implied.

New recording requires outlet editor authority, accounts-adjust permission and
Payables availability. Its bounded `GET /accounts/supplier-credits/source-returns`
selector uses accounts-read, Payables admission and exact current outlet scope;
it exposes dispatched financial references without granting physical return or
stock permission. It works before an actual credit exists. The supplied note
reference, issue date, positive actual amount and reason remain explicit; expected
return value never defaults the actual amount.

The form stores only a command reference bound to tenant, actor, pharmacy and
outlet before submission. Uncertain responses retain it. Recovery verifies actor
and scope, plus the retained intent when available. After remount, a missing
original can be re-entered with that same reference; an immutable recorded
command cannot be overwritten or duplicated by changing the input. No automatic
financial retry occurs. Recovery reads remain available when new recording is
disabled. The hosted dispatch browser journey now additionally exercises a lost
actual-credit response and recovery, actual versus expected amounts, unchanged
bill liability and human-only rendered references. Latest hosted evidence is
required before claiming that journey passed.

Return matching now has a candidate workspace beneath the selected credit.
Its retained, bounded history uses return numbers, human reasons and authorised
recorder names. New matching requires Payables availability and current financial
editor authority; it chooses a dispatched financial source and keeps the amount
blank until entered. Current credit version and total return attribution come
from owner reads. Amounts cannot exceed remaining credit attribution; exceeding
the expected return value requires explicit evidence. Matching never applies the
credit to a bill. A stale refusal preserves the visible amount and offers a
refresh of current credit/source observations before another command.

Only the scope/credit-bound command reference is retained in session storage.
Uncertain writes are never retried automatically. Recovery checks the original
actor, scope and retained in-memory intent; after reload it rechecks the same
immutable command. If session cleanup fails, the same verified reference remains
recoverable and another match is blocked. Retained recovery still works when new
matching is disabled or all actual credit has already been attributed. Failed
history or source reads hide cached private evidence. The hosted browser journey
extends actual credit entry with full-amount matching, response-loss reload
recovery and proof that bill liability and unused credit remain unchanged.

A candidate **Review match** control now opens the exact original match and
current owner observations, including attribution from other credit documents.
Full reversal requires an explicit reason, the current credit version and current
return total; it preserves the original amount and leaves other credit matches,
unused credit, bill liability and physical stock unchanged. A stale refusal
preserves the reason and offers the shared authorised refresh control. Retained
reversal evidence remains readable after void and with new writes unavailable.

Reversal recovery persists only the command/match references under the verified
scope and credit. It reopens without scanning history pages, survives reload and
works when new writes are disabled. Normal retained reads bind evidence to the
original return, amount and reversal reference. Recovery additionally verifies
the original actor/command and any retained submitted intent. Private evidence
is hidden on failed owner reads; a storage cleanup failure preserves same-command
recovery and blocks another reversal. The hosted browser scenario now covers a
lost committed reversal, reload, one-write recovery and unchanged bill/stock.

Candidate bill application controls now select a named eligible outstanding bill
for the same supplier/currency. The actual amount starts blank and must fit both
the unused credit pool and current bill liability. Return attribution does not
spend that pool. The explicit application date must be on/after both documents
and no later than the owner-provided outlet date. Submission includes current
credit/bill versions and both observed balances. Financial editor authority,
accounts-adjust and Payables admission remain necessary for new commands.

Bounded immutable application history retains the original bill reference,
amount, date, reason and named recorder. Exact application context supports review
even after a bill is fully paid and absent from eligible choices. Full reversal
requires an explicit reason/date and current observations; it restores only the
original applied amount to credit and liability, retaining subsequent payments.
Its date cannot precede the original application or exceed the outlet date.
Neither command changes physical stock or captures/refunds cash.

Application recovery stores only its scoped command reference. Reversal recovery
stores only the scoped command and original application references, reopening the
exact review after reload without scanning eligible bills. In-memory intent is
checked when retained; the SDK also binds recovered reversal evidence to the
original bill, amount and human references. Unknown outcomes never retry
automatically. Missing originals reuse the same identity; failed storage prevents
submission, and failed cleanup blocks another command. Retained history/recovery
continues with new writes disabled under current read authority. Failed history
or current context reads conceal cached financial evidence.

The hosted browser candidate additionally covers a fully paid source bill,
partial application to a later bill, payment of its remainder, full application
reversal, unchanged payments/stock and same-command reload recovery for both
writes. Latest hosted/visual evidence and approved live acceptance remain
required. Complete bill history/report integration and the remaining supplier
acceptance remain open in #1784.

Candidate **Credit void** controls retain a separate reason and authorised
recorder without deleting the original note. A new void requires an active credit
with zero current net return attribution and zero current bill applications, the
current version, explicit reason and existing financial editor/accounts-adjust/
Payables authority. Active matches and applications must first be reversed through
their owning commands. Voiding removes the unused pool; it never changes physical
stock, bill liability, recorded payments or cash. A replacement/correction note
remains a separate explicit command and is not automatically generated.

Void recovery stores only the scope/credit-bound command reference, checks the
original actor and any retained version/reason, and remains available after reload
or withdrawal of new-write authority. Retained void history is available for
voided/corrected credits under current read authority; failed current reads hide
cached evidence. Stale refusals preserve the entered reason for review and explicit
refresh. Unknown outcomes retain their original identity without automatic retry;
storage and cleanup failures use the same financial-command safeguards. The
hosted candidate journey loses the committed void response after reversing all
applications/matches and verifies one-write recovery and unchanged bill payment.
Latest hosted/visual/live acceptance remains open in #1784.

Candidate **Replace voided credit** entry reuses the canonical actual-credit
form beneath the named original. It binds `correction_of` and the original
dispatched return internally; supplier/currency come from the current finance
context. The actual replacement amount, reference, date and reason are explicit,
not defaults copied from the original note. This control corrects the note on
that original return; it does not reassign an existing note to another supplier
or return. The owning command atomically verifies current voided state, same
supplier/currency, source/date validity and the unique correction. The original
becomes corrected with zero unused pool; the replacement has its own actual
amount, active pool and immutable receipt. Neither creates a bill application,
payment or stock movement.

Correction recovery is scoped additionally to its original note. After reload,
the recovered immutable receipt must still identify that original, return,
supplier and currency, even when no submitted form intent survives. Recovery
remains available when the original has become corrected or new writes are
disabled, including after closing and resuming the form. Stale refusal preserves
entered values and offers explicit refresh of the original observations.
Normal document entry retains its original recovery namespace and cannot accept
a correction receipt as an unrelated original note. The hosted candidate extends
void recovery with replacement response loss, one-write recovery, retained
original history and unchanged payments/stock. Latest hosted/visual evidence,
payable journal/report integration and approved live acceptance remain open.
