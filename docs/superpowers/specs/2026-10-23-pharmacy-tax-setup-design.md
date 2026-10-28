# Pharmacy tax setup

> **2026-10-23, #1287:** Pakistan was removed; India is the only supported country.

Resolves #1283. First slice of pharmacy Sales (#172): a sale cannot be taxed until outlets are registered and products are classified. Hospital-wide billing tax stays with #208 and #539.

## Problem

A counter sale needs three things that do not exist:

- **Where the seller is registered.** An outlet records its country and timezone, but no state and no GSTIN. Without them, CGST+SGST cannot be told apart from IGST, and the invoice cannot name the seller's registration.
- **How each product is taxed.** A product has a pack MRP but no HSN code, no rate and no supply nature (taxable, nil-rated, exempt).
- **A calculation.** Nothing computes tax. Supplier bills (`payables`) store a typed-in tax amount, with no rate and no split.

The platform holds no approved tax rates (IN-RC-10 in `docs/domain/india/billing-and-insurance.md`; §Country configuration in `docs/requirements/pharmaconnect-india.md`). Rates also change: India rationalised its GST slabs in 2025. So **the hospital sets the HSN code and rate on its products** (decided by the product owner on 2026-10-23), and the country setting describes only how tax works there.

## Decisions

### D1. Per-country tax scheme in the country profile contract

`contracts/country-profiles.json` gains a `tax` object per country. It is generated into `backend/pkg/countryprofile` and `packages/api/src/country-profiles.ts`, like the rest of the profile. The scheme describes structure, never rates:

| Field               | India                                                                                                                                                                       |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scheme`            | `"gst"`                                                                                                                                                                     |
| `registration_kind` | `"gstin"` (must be one of the country's `tax_identifier_kinds`)                                                                                                             |
| `classification`    | `{ "name": "HSN", "pattern": "^([0-9]{4}\|[0-9]{6}\|[0-9]{8})$" }`                                                                                                          |
| `jurisdictions`     | Each state and union territory: `{ code, name, kind }`, where `code` is the two-digit GST state code and `kind` is `state` or `union_territory`                             |
| `components`        | Within one jurisdiction: CGST plus SGST (a state, or a union territory with a legislature) or CGST plus UTGST (a union territory without one). Between jurisdictions: IGST. |
| `sources`           | The https source and review date for the jurisdiction list and the component rule                                                                                           |

- The jurisdiction list and the UTGST set are copied from the GST portal's state-code list and the UTGST Act at implementation time, then recorded in `sources`. They are not taken from memory. A jurisdiction's `kind` decides SGST vs UTGST, so a code-review mistake there is a wrong invoice.
- _(Builder's call.)_ The list holds only places an outlet can be: states, union territories and 97 (Other Territory). It excludes:
  - 96 (foreign) and 99 (Centre jurisdiction), which are not outlet locations;
  - retired codes 25 and 28. Dadra & Nagar Haveli and Daman & Diu registrations moved to 26 in the 2020 merger, and Andhra Pradesh moved from 28 to 37; neither appears in the GST portal's current state list (checked 2026-10-23), so a new registration starting `25` or `28` is refused.
  - If the sources show otherwise when the list is copied, the sources win and this paragraph is corrected in the same change.
- A country with `"tax": null` (no scheme) **refuses** tax setup for its outlets (`409 tax_scheme_unavailable`), failing closed rather than guessing a scheme. Adding a country means adding its scheme, plus a new calculation kind if it is not `gst`. No `if country == "IN"` branch anywhere.
- The generator validates the shape: unique jurisdiction codes, `registration_kind` among `tax_identifier_kinds`, a compilable `pattern`, and https `sources`. The generator's existing check fails CI when a generated file is stale.

### D2. Outlet tax registration

New table `pharmacy_outlet_tax_registrations`. Registration is outlet administration, so it follows the **registry** model, not the catalogue model (_builder's call_):

- RLS is tenant isolation only.
- The handler checks scope with `pharmacyVisible`, the same check the outlet registry uses. That way an owner can register an outlet before granting themselves an outlet access row.
- Reads also need `pharmacy.tax.manage` (_builder's call_: a route declares one permission, and the owner role holds both `tax.manage` and `access.manage`). The sale (slice 3) reads the registration inside its own transaction.

The table and its rows:

- Columns: `outlet_id`, `jurisdiction_code`, `identifier_kind`, `identifier`, `effective_from` (a date), `reason` (text), `recorded_by`, `created_at`, and `seq` (a `bigint` identity).
- **Append-only** (immutable trigger). A change or a correction is a new row.
- **The registration in force on a date** is the row with the latest `effective_from` on or before that date. When two rows share an `effective_from`, the later-recorded one (highest `seq`) wins. `seq` is drawn inside the insert trigger after the per-scope lock, so it follows the order rows are recorded even when writers contend.
  - So a mistake entered for today is fixed the same day by recording the right value with the same date.
  - The wrong row stays in the history, shown as "Replaced".
  - A row that shares its `effective_from` with an earlier row must give a non-blank `reason` (CHECK through a trigger). A correction is always explained.
- `effective_from` cannot be before today in the outlet's timezone. Backdating would rewrite what the registration "was" for past days. A sale stores its own snapshot (slice 3), so a later change or correction never alters an issued invoice.
- **Validation (server, then mirrored in the form):**
  - `jurisdiction_code` is in the scheme;
  - `identifier_kind` equals the scheme's `registration_kind`;
  - the GSTIN is 15 characters: state code, PAN shape, entity digit, `Z`, and a checksum character that must pass the GSTIN mod-36 check;
  - the GSTIN's first two digits equal `jurisdiction_code`.
- GSTN portal verification (legal name, active status) is not covered. The UI says "Format checked, not verified with GSTN".

### D3. Product tax classification

New table `pharmacy_product_tax_classifications`, outlet-scoped, keyed to the product:

- Columns: `product_id`, `classification_code` (HSN), `supply_nature` (`taxable` | `nil_rated` | `exempt`), `rate_ppm` (parts per million: 5% is `50000`), `exemption_basis` (text), `effective_from`, `reason`, `recorded_by`, `created_at`, `seq`.
- **Rate unit.** Rates are entered to 0.01% (`rate_ppm % 100 = 0`) and stored in parts per million, so half of any rate is still a whole number (0.25% gives two components at 0.125%, `1250` each). Components carry `rate_ppm` too.
- **CHECKs:**
  - `taxable` requires `rate_ppm BETWEEN 100 AND 1000000`;
  - `nil_rated` and `exempt` require `rate_ppm = 0`;
  - `exempt` requires a non-blank `exemption_basis`, so an exemption without a recorded reason cannot be stored (IN-RC-10).
- The HSN must match the scheme's `classification.pattern`.
- Append-only, effective-dated and never retroactive, with the same correction rule as D2: the later-recorded row wins for the same date, and a correction needs a reason. A hospital can enter a known future rate change ahead of its date.
- **Withdrawn products** cannot be classified (`409`, matching the catalogue's existing refusal to change them). Discontinued products can be classified: they may still have stock to sell. The `?tax=unclassified` filter counts active and discontinued products.
- Classifications follow the catalogue model: `pharmacy_outlet_allowed` RLS, so an outlet access row is required, as for every product read and write.
- **Why a separate table, not a field on the product revision:** a batch pins the product version it was received under, which fixes its MRP. Tax is decided by the **date of sale**, not the date of receipt. A rate change must apply to stock already on the shelf, so classification cannot ride on the pinned revision.
- The product list and product detail return the classification in force today (or `null`), and the detail returns its history.

### D4. Calculation package `backend/pkg/tax`

A pure Go package with no database, usable later by MediCore (#208). All amounts are `int64` minor units, and no float may appear: a package test (`floats_test.go`) parses every source file and fails on any `float32`/`float64` or float literal.

- **Input per line:** an amount, its basis (`inclusive`, where the amount includes tax as MRP does, or `exclusive`), `rate_ppm` and `supply_nature`. **Per invoice:** the scheme, the supplier's jurisdiction and the place-of-supply jurisdiction.
- **Output per line:** the taxable value plus a list of components (`{name, rate_ppm, amount}`). Intra-jurisdiction gives two components at half the rate each; inter-jurisdiction gives one at the full rate.
- **Rounding rule (one rule, documented in the package):** round half up to the minor unit, once per component, per line. With `r` in ppm and `M = 1000000`:
  - Within a jurisdiction: compute the half-tax `h` once and use it for both components, so CGST always equals SGST/UTGST.
  - Inclusive: `h = round(A × r / (2 × (M + r)))`, tax = `2h`, taxable = `A − 2h`.
  - Exclusive: `h = round(V × r / (2M))`.
  - Between jurisdictions (IGST): `round(A × r / (M + r))` inclusive, or `round(V × r / M)` exclusive.
  - So for inclusive amounts, taxable plus tax equals the amount exactly, and the MRP ceiling is never broken by rounding.
- Intermediate products use `math/big` (or checked 128-bit arithmetic), so a ₹10,00,00,000 line at 28% cannot overflow.
- No invoice-level round-off. Rounding the payable total to the rupee is a sale-document decision for slice 3.
- `nil_rated` and `exempt` give zero components, with the nature carried through.
- **Tests:**
  - a golden file (`testdata/gst.json`) of hand-computed cases: 5% and 18% inclusive MRP lines, odd-paise halves, a 0.25% rate (0.125% halves), IGST, a union territory (UTGST), nil-rated and exempt, the 100% ceiling, and a 1-paisa line;
  - a property test that inclusive taxable + tax equals the input and that halves are equal over random inputs;
  - each golden case is shown to fail first by perturbing the expected value.

### D5. API and permission

- New permission **`pharmacy.tax.manage`**, granted to `RolePharmacyOwner` only. It is deliberately separate from `pharmacy.catalogue.write` (pharmacist, purchasing): a wrong rate taxes every sale of that product wrongly, so the people who edit product names are not the ones who set tax.
- Reads:
  - classification reads ride on `pharmacy.catalogue.read`, through the catalogue's outlet scope;
  - registration reads need `pharmacy.tax.manage`, under the D2 registry scope. `GET …/tax-registrations/in-force` returns the row in force today (or `null`), so a screen never infers it from a history page.
- Routes, under the existing outlet scope:
  - `GET` and `POST` `…/tax-registrations`: list the outlet's registrations, newest first, or record a new one.
  - `GET` and `POST` `…/catalogue/products/{id}/tax-classifications`: list a product's history, or record a new classification.
  - `POST` returns `201`. A bad field is a `400` whose message names the field (the module has no field-error envelope). An unknown or out-of-scope product or outlet is a `404`. A null scheme is a `409 tax_scheme_unavailable`. A withdrawn product is a `409`. A same-date correction without a `reason` is a `400`.
- **Contract.** The request bodies are canonical in `contracts/pharmacy.json` and generated to Go and Zod by `scripts/generate-pharmacy-contract.mjs`, like every other pharmacy input (`docs/api/schemas.md`). No handwritten second model and no new OpenAPI file: pharmacy has none, and `clinical.openapi.json` is a limited pilot. The routes and the permission show up in the generated contract docs (`go run ./cmd/contractdocs`, then `-check`), and `docs/api/domains/pharmacy.md` gains a "Tax setup" section.
- No events: nothing consumes them yet, and slice 3 reads the tables in its own transaction.

### D6. UI

- **Setup page (Outlets & access):** a "Tax registration" panel per outlet, with the registration in force, a form (jurisdiction `SelectControl` from the scheme, GSTIN, effective from) and the history. When the outlet's country has no scheme, it shows the plain message "Tax setup is not available for {country} yet", not a form.
- **Product catalogue:**
  - each row shows "HSN 3004 · 5%", "Nil-rated", "Exempt" or a "Not classified" badge;
  - the product detail has a "Tax" section with the classification form (shown only with `pharmacy.tax.manage`) and the history;
  - a "Not classified" filter (`?tax=unclassified` on the list route) lets an owner work through the gap.
- Rates are entered and shown as percentages with up to two decimals (`5`, `0.25`) and stored as parts per million.
- A history row that a later same-date row replaced shows "Replaced"; the correction's reason shows on the row that replaced it.
- Forms use `useZodForm` + `Field`, `noValidate`, sonner toasts ("Registration recorded", "Classification recorded") and `QueryState` lists, plus Vitest tests per panel.

## Not covered

- **Selling.** The counter sale, invoices and their numbering, payments and the MRP ceiling check are #172 slices 2–4. Slice 3 will refuse to sell an unclassified product, or one at an outlet with no registration.
- **Approval (maker-checker)** of registrations and classifications. Today they are attributed and permission-separated, not second-person approved.
- GSTN verification, composition-scheme outlets (bill of supply), cess, reverse charge, e-invoicing and GST reports (#1061).
- Bulk import of HSN codes.
- Hospital-wide billing tax and a central rate registry (#208, #539). This slice's `pkg/tax` is a building block for them, not a replacement.
