# Pharmacy Tax Setup Implementation Plan

> **2026-10-23, #1287:** Pakistan is no longer a supported country. India is the only country profile; existing Pakistan records refuse the migration rather than being changed. Pakistan content below is historical.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Outlets record a GST registration and products record an HSN classification, both effective-dated and append-only, and a pure package computes per-line GST from them.

**Architecture:** The per-country scheme (structure, never rates) lives in the generated country profile contract. `backend/pkg/tax` holds the pure calculation and registration validation. The pharmacy module gets migration `0009_pharmacy` (two tables) and four routes. The shell gets a registration panel on Setup and a tax section in the product catalogue.

**Tech Stack:** Go + GORM + PostgreSQL (forced RLS), the Node contract generators in `scripts/`, Next.js + `@helivanta/api` + `@helivanta/ui`, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-pharmacy-tax-setup-design.md` (#1283)

## Global Constraints

- Money is `int64` minor units. Rates are `rate_ppm` (5% = `50000`), entered to 0.01% (`rate_ppm % 100 = 0`). No `float32`/`float64` in `backend/pkg/tax`.
- Rounding: half up to the minor unit, once per component, per line. Within a jurisdiction, one half `h` is used for both components.
- Rows are append-only. The row in force on date `d` is the one with the max `effective_from <= d`, and the highest `seq` among equal dates. A same-date correction requires a non-blank `reason`.
- `effective_from` must be on or after today in the outlet's timezone.
- A country whose `tax` is `null` gets `409 tax_scheme_unavailable` on every tax write. There is no `if country == "IN"` anywhere.
- The `pharmacy.tax.manage` permission goes to `RolePharmacyOwner` only.
- Pharmacy request bodies are canonical in `contracts/pharmacy.json`. Regenerate with `node scripts/generate-pharmacy-contract.mjs`, never hand-edit the outputs.
- The jurisdiction list and the UTGST set come from primary sources, recorded with review dates in the profile's `tax.sources`.

## Review Focus

1. **A wrong `kind` on a jurisdiction** (a UT with a legislature marked as `union_territory`) silently puts UTGST on every invoice. Task 1 asserts Delhi (07), Puducherry (34) and Jammu & Kashmir (01) are SGST, and Chandigarh (04) and Ladakh (38) are UTGST.
2. **Two corrections recorded in the same second.** `seq` decides, not `created_at`. Task 4 records two rows for one date in one transaction and asserts the later one is in force.
3. **The outlet's timezone is ahead of UTC.** At 00:30 IST, "today" is the IST date. Task 4 tests `effective_from` = the IST date with the server clock at 19:00 UTC on the previous day.
4. **The GSTIN state code disagrees with the jurisdiction**, or the checksum is valid for a different state. Task 2's vectors include a valid-checksum GSTIN under the wrong jurisdiction.
5. **The `?tax=unclassified` filter uses today in UTC,** missing a classification that starts today in IST. Task 5 asserts it uses the outlet's local date.

---

### Task 1: Tax scheme in the country profile contract

**Files:**

- Modify: `contracts/country-profiles.json`, `scripts/generate-country-profiles.mjs`
- Regenerate: `backend/pkg/countryprofile/profiles_gen.go`, `packages/api/src/country-profiles.ts`
- Test: `backend/pkg/countryprofile/profiles_test.go`, `scripts/generate-country-profiles.test.mjs` (create if absent; match how other `scripts/*.test.mjs` run)

**Interfaces:**

- Produces (Go):
  - `countryprofile.TaxScheme{Scheme, RegistrationKind, ClassificationName, ClassificationPattern string; Jurisdictions []Jurisdiction; Sources []Source}`
  - `Jurisdiction{Code, Name, Kind string}`, where `Kind` is `"state"` or `"union_territory"`
  - `countryprofile.Tax(country string) (TaxScheme, bool)`, which is false for a null scheme or an unknown country
  - `(TaxScheme) Jurisdiction(code string) (Jurisdiction, bool)`
- Produces (TS): `taxScheme(country): TaxScheme | null` with the same fields in snake_case, as the profile already uses.

- [ ] **Step 1: Write failing tests**
  - `TestIndiaTaxScheme`:
    - `Tax("IN")` is ok, with `Scheme=="gst"`, `RegistrationKind=="gstin"` and `ClassificationName=="HSN"`;
    - jurisdiction `"07"` has kind `state` (Delhi is treated as a state for SGST because it has a legislature; record this in `sources`), and so do `"34"` and `"01"`;
    - `"04"` and `"38"` have kind `union_territory`;
    - `"25"`, `"96"` and `"99"` are absent, and `"97"` is present;
    - codes are unique;
    - the classification pattern matches `3004`, `300490` and `30049099`, and rejects `300` and `30049`.
  - `TestPakistanHasNoTaxScheme`: `Tax("PK")` is not ok.
  - Generator test: it refuses a profile whose `registration_kind` is not in `tax_identifier_kinds`, a duplicate jurisdiction code, a non-https source, or an uncompilable pattern.
- [ ] **Step 2: Run them.** `cd backend && go test ./pkg/countryprofile/...`. Expected: FAIL (`Tax` undefined).
- [ ] **Step 3: Implement.**
  - Copy the state-code list from the GST portal's state-code table, and the UTGST set from the UTGST Act 2017 §2 (as amended), into `tax.jurisdictions`. Record both URLs and today's date in `tax.sources`.
  - Extend the generator's validation and emitters for both outputs.
  - Pakistan gets `"tax": null`.
- [ ] **Step 4: Regenerate and run.** Run `node scripts/generate-country-profiles.mjs`, then the Go tests, the generator test and `node scripts/generate-country-profiles.mjs --check`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(country): per-country tax scheme in the country profile (#1283)`.

### Task 2: `backend/pkg/tax`: calculation and registration validation

**Files:**

- Create: `backend/pkg/tax/{tax.go,gstin.go,tax_test.go,gstin_test.go,floats_test.go}`, `backend/pkg/tax/testdata/{gst.json,gstin.json}`
- Create: `packages/api/src/tax-registration.ts` + `.test.ts` (TS mirror of the GSTIN check, reading the same `gstin.json`)
- Modify: `backend/.golangci.yml` only if depguard needs `pkg/tax` allowed

**Interfaces:**

- Consumes: `countryprofile.TaxScheme` (Task 1).
- Produces:
  - `tax.Basis`: `Inclusive` or `Exclusive`.
  - `tax.Nature`: `Taxable`, `NilRated` or `Exempt`.
  - `tax.Line{Amount int64; Basis Basis; RatePPM int64; Nature Nature}`
  - `tax.Component{Name string; RatePPM int64; Amount int64}`
  - `tax.Result{Taxable int64; Components []Component; Nature Nature}`
  - `tax.Compute(s countryprofile.TaxScheme, supplier, placeOfSupply string, l Line) (Result, error)`. Errors are `ErrUnknownJurisdiction`, `ErrInvalidRate` and `ErrNegativeAmount`.
  - `tax.ValidateRegistration(s countryprofile.TaxScheme, jurisdiction, kind, identifier string) error`, returning `ErrKind`, `ErrFormat`, `ErrChecksum` or `ErrJurisdictionMismatch`.
  - TS: `validateTaxRegistration(scheme, jurisdiction, identifier): "format" | "checksum" | "jurisdiction" | null`.

- [ ] **Step 1: Write failing tests.**
  - `gst.json` holds about 15 hand-computed cases, each as `{name, supplier, pos, amount, basis, rate_ppm, nature, taxable, components[]}`:
    - ₹112.00 inclusive at 12% in one state: taxable 10000, CGST 600, SGST 600;
    - ₹105.00 inclusive at 5%: 10000, then 250 and 250;
    - an odd-paise half case, ₹100.01 inclusive at 18%:
      - h = round(10001×180000/(2×1180000)) = round(762.79…) = 763;
      - so the tax is 1526 and the taxable value is 8475;
      - compute and verify every value by hand in the test comment;
    - 0.25% (`2500`), whose components are `1250` ppm each;
    - IGST inter-state;
    - Chandigarh, giving CGST + UTGST;
    - exclusive 18%;
    - nil-rated and exempt, with zero components;
    - the 100% ceiling (`1000000`);
    - a 1-paisa line;
    - ₹10,00,00,000.00 at 28% (overflow guard).
  - `TestGolden` runs them all.
  - `TestInclusiveConserves` (property, `testing/quick` or a seeded loop of 10k): for an inclusive amount, taxable + the sum of the components equals the amount, and the two halves are equal intra-state.
  - `TestRejects`: a rate not a multiple of 100, a rate over 1000000, a taxable rate of 0, a nil-rated rate above 0, a negative amount, and an unknown jurisdiction.
  - `gstin.json`:
    - valid GSTINs (computed with the mod-36 algorithm, synthetic PAN-shaped, never real registrations);
    - a wrong check character;
    - a lowercase input (normalised to upper and accepted);
    - 14 and 16 characters;
    - no `Z` at position 14;
    - a valid checksum under a different jurisdiction.
  - The TS test runs the same file.
  - `TestNoFloats` parses every non-test `.go` file in the package with `go/parser` and fails on any `float32`/`float64` identifier.
- [ ] **Step 2: Run them.** `go test ./pkg/tax/...` and `pnpm --filter @helivanta/api test tax-registration`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement.**
  - Use the formulas from spec D4 with `math/big` intermediates; round half up as `(n*2 + d) / (2*d)` on non-negative big ints.
  - Pick component names from the scheme: CGST plus SGST, or CGST plus UTGST, by the place-of-supply jurisdiction's `Kind` when it equals the supplier's; otherwise IGST.
  - Implement GSTIN mod-36 per the GSTN checksum algorithm, citing it in a one-line comment with the source URL.
- [ ] **Step 4: Prove the goldens can fail.** Perturb one golden `taxable` by 1 and run; expected FAIL. Revert it, then run both suites; expected PASS.
- [ ] **Step 5: Commit** `feat(tax): GST line calculation and GSTIN validation (#1283)`.

### Task 3: Migration `0009_pharmacy` and contract models

**Files:**

- Create: `backend/internal/modules/pharmacy/tax.sql`
- Modify: `module.go` (embed it, add `{ID: "0009_pharmacy", SQL: taxSchema}`, and `PermTaxManage` granted to `RolePharmacyOwner`), `contracts/pharmacy.json` (models `taxRegistrationInput{jurisdiction_code, identifier, effective_from, reason?}` and `taxClassificationInput{classification_code, supply_nature, rate_percent, exemption_basis?, effective_from, reason?}`), then regenerate
- Test: `backend/internal/modules/pharmacy/tax_test.go` (database-level cases)

**Interfaces:**

- Produces:
  - tables `pharmacy_outlet_tax_registrations` and `pharmacy_product_tax_classifications`, with the columns in spec D2/D3 plus `id` (a v7 uuid CHECK, like the siblings), `tenant_id` and `seq bigint GENERATED ALWAYS AS IDENTITY`;
  - the generated Go `taxRegistrationInput` and `taxClassificationInput`;
  - the Zod `pharmacyTaxRegistrationSchema` and `pharmacyTaxClassificationSchema`.
- `rate_percent` is a string matching `^(100(\.0{1,2})?|[0-9]{1,2}(\.[0-9]{1,2})?)$`. The handler converts it to ppm, so the wire never carries a float.

- [ ] **Step 1: Write failing database tests** (real PostgreSQL, like `payables_test.go`):
  - UPDATE and DELETE on either table raise errors;
  - a same-`effective_from` insert without `reason` is refused;
  - the CHECKs refuse: taxable with rate 0, nil-rated with a rate, exempt without a basis, and a rate not a multiple of 100;
  - a classification row for a product in another outlet fails RLS;
  - a registration row in another tenant is invisible.
- [ ] **Step 2: Run them.** `go test ./internal/modules/pharmacy/ -run TestTaxSchema`. Expected: FAIL (relation does not exist).
- [ ] **Step 3: Implement `tax.sql`.**
  - It has the same header (`lock_timeout`, `statement_timeout`), forced RLS and audit triggers as `payables.sql`.
  - Registrations use the tenant policy, as `pharmacy_outlets` does; classifications use `pharmacy_outlet_allowed`.
  - Add an immutability trigger and a `BEFORE INSERT` trigger requiring `reason` when a row with the same scope and `effective_from` exists.
  - Add the index `(tenant_id, outlet_id[, product_id], effective_from DESC, seq DESC)`.
  - Update the contract models and run `node scripts/generate-pharmacy-contract.mjs`.
- [ ] **Step 4: Run.** The tests, then `node scripts/generate-pharmacy-contract.mjs --check`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): tax registration and classification tables (#1283)`.

### Task 4: Outlet tax registration routes

**Files:**

- Create: `backend/internal/modules/pharmacy/tax_registrations.go`
- Modify: `registry.go` (register the routes in `registryRoutes`)
- Test: `tax_test.go`

**Interfaces:**

- Consumes: `countryprofile.Tax`, `tax.ValidateRegistration`, `pharmacyVisible`, `registryManager`, Task 3's input.
- Produces:
  - `GET  /pharmacy/pharmacies/:pharmacy/outlets/:outlet/tax-registrations` (`ListRoute`, newest `effective_from` then `seq` first). Each row has `in_force bool` and `replaced bool` computed against the outlet's local today.
  - `POST /pharmacy/pharmacies/:pharmacy/outlets/:outlet/tax-registrations` (`PermTaxManage`) returns `201` with the row.
  - Unexported `registrationInForce(tx, tenant, outlet uuid.UUID, on civil date) (registration, error)` for slice 3.
  - Read permission is `PermTaxManage`, or `PermAccessManage` checked in the handler. Pick whichever matches how `platform.Router` expresses "either"; if it can't, register with `PermTaxManage` and give `RolePharmacyOwner` both, documented in a one-line comment.

- [ ] **Step 1: Write failing handler tests.**
  - `201` for a valid India registration, with `in_force` true.
  - `400` with a field error for each `ErrFormat`, `ErrChecksum`, `ErrJurisdictionMismatch` and unknown jurisdiction case.
  - `400` for an `effective_from` before the outlet's local today. The clock is injectable: reuse the module's existing clock if there is one, otherwise add the smallest `now func() time.Time` on the handler.
  - The Review Focus #3 boundary case.
  - A same-date correction with a reason gives `201`; the earlier row then reads `replaced`. Two rows in one transaction: the later `seq` is in force (Review Focus #2).
  - `409 tax_scheme_unavailable` for a PK outlet.
  - `404` for another tenant's outlet and for an outlet of a pharmacy the subject can't see.
  - `403` for `RolePharmacist`.
- [ ] **Step 2: Run them.** `go test ./internal/modules/pharmacy/ -run TestTaxRegistration`. Expected: FAIL.
- [ ] **Step 3: Implement** with `respond.*`, `requestid.Logger(c)` and `WithTenant`. Wrap errors with `%w`.
- [ ] **Step 4: Run** the tests, then `go test -race ./internal/modules/pharmacy/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): outlet tax registration (#1283)`.

### Task 5: Product tax classification routes and catalogue fields

**Files:**

- Create: `backend/internal/modules/pharmacy/tax_classifications.go`
- Modify: `catalogue.go` (routes in `catalogueRoutes`; the list and detail responses gain `tax` = the classification in force today or `null`; the list accepts `?tax=unclassified`)
- Test: `tax_test.go`, `catalogue_test.go`

**Interfaces:**

- Consumes: `catalogueScope`, `countryprofile.Tax`, Task 3's input, and `tax.Nature`.
- Produces:
  - `GET  …/catalogue/products/:id/tax-classifications` (`PermCatalogueRead`, newest first, with `in_force` and `replaced`).
  - `POST …/catalogue/products/:id/tax-classifications` (`PermTaxManage`) returns `201`.
  - The product JSON gains `"tax": {"classification_code", "supply_nature", "rate_ppm", "effective_from"} | null`.
  - Unexported `classificationInForce(tx, tenant, outlet, product uuid.UUID, on civil date)` for slice 3.

- [ ] **Step 1: Write failing tests.**
  - `201` taxable with `rate_percent "5"`, so `rate_ppm` is 50000.
  - `400` for a bad HSN against the scheme pattern, for `"5.001"`, for exempt without a basis, and for nil-rated with a rate.
  - `409` for a withdrawn product, and `201` for a discontinued one.
  - `409 tax_scheme_unavailable` for a PK outlet's product.
  - `404` across outlets.
  - `403` for a pharmacist (who has `catalogue.write`).
  - The list `tax` field reflects the row in force today. A future-dated row is not in force yet.
  - `?tax=unclassified` returns only active and discontinued products without a classification in force on the outlet's local date (Review Focus #5); any other `tax` value is a `400`.
  - The existing catalogue tests still pass, with the `tax: null` field added to their expectations.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.** For the in-force row per product, use a `LEFT JOIN LATERAL (… ORDER BY effective_from DESC, seq DESC LIMIT 1)`.
- [ ] **Step 4: Run** the tests, `go test -race ./internal/modules/pharmacy/...`, then `cd backend && go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): product tax classification (#1283)`, including the regenerated contract docs.

### Task 6: Shell UI

**Files:**

- Create: `apps/shell/components/pharmacy/tax-registration-panel.tsx` + `.test.tsx`, `apps/shell/components/pharmacy/tax-classification.tsx` + `.test.tsx`
- Modify: `pharmacy-workspace.tsx` (Setup view renders `TaxRegistrationPanel` for the selected outlet), `catalogue-panel.tsx` (row badge, `Tax` section in the detail, "Not classified" filter), `packages/api` types for the new responses (generated where the contract covers them)
- Modify: `e2e/tests/` (extend the existing pharmacy spec that seeds an outlet: record a registration, classify a product, and see the badge change from "Not classified")

**Interfaces:**

- Consumes: the Task 4/5 routes, the Task 3 Zod schemas, `taxScheme()` and `validateTaxRegistration()` (Tasks 1–2), `useApiPagedQuery`/`useApiMutation`, `useZodForm`, `Field`, `SelectControl`, `QueryState`, `Panel` and `Can`.
- Copy: "Tax registration", "Format checked, not verified with GSTN", "Tax setup is not available for {country name} yet", "Registration recorded", "Classification recorded", "Not classified", "Replaced", and badges "HSN {code} · {rate}%", "Nil-rated" and "Exempt".

- [ ] **Step 1: Write failing Vitest tests** with `renderWithProviders`:
  - **Registration:**
    - it lists the in-force row;
    - it refuses a bad checksum inline without posting;
    - posts `{jurisdiction_code, identifier, effective_from}` and toasts "Registration recorded";
    - asks for a reason when a row with the same date exists;
    - shows the not-available message, not a form, for a PK outlet;
    - hides the form without `pharmacy.tax.manage`;
    - a failed load shows `RequestFailure`, not empty.
  - **Classification:**
    - the badge text for each nature;
    - `rate "0.25"` posts `rate_percent "0.25"`;
    - the exempt basis is required;
    - the form is hidden for a pharmacist;
    - the "Not classified" filter requests `?tax=unclassified`;
    - history shows "Replaced".
- [ ] **Step 2: Run them.** `pnpm --filter shell test tax-`. Expected: FAIL.
- [ ] **Step 3: Implement**, following `accounts-panel.tsx` and `stock-register.tsx` patterns.
- [ ] **Step 4: Run** `pnpm turbo lint type-check test build format:check`, then the e2e suite against a production build. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): tax registration and classification screens (#1283)`.

### Task 7: Docs

- Modify:
  - `docs/api/domains/pharmacy.md`: a "Tax setup" section with the routes, the permission, the rules and what's not covered;
  - `docs/requirements/pharmaconnect-india-pakistan.md` §Country configuration: the country metadata now carries a tax **scheme** but still no rates; hospitals classify;
  - `docs/domain/india/README.md`: link to the spec if the index lists specs.
- Commit `docs(pharmacy): tax setup contract and requirement notes (#1283)`.

## Verify

- Frontend: `pnpm turbo lint type-check test build format:check` and `make lint-shell`.
- Backend: `make lint-go`, `GOTOOLCHAIN=go1.27.3 ./scripts/coverage-gate.sh` (from `backend/`), `go test -race ./...`, and `go run ./cmd/contractdocs -check`.
- E2e on a production build.
- Screenshots of Setup (registration in force, plus history with "Replaced") and the catalogue (badges and the "Not classified" filter).
- PR body: closes #1283; lists the sources copied in Task 1 with their review dates, the contracts changed and what's not covered (from the spec).
