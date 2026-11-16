# Pharmacy tax command identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A resent tax registration or classification is recorded once: an identical resend replays the row, and any other reuse of its ID is 409.

**Architecture:** Both POSTs become `PUT …/{uuidv7}`. A shared helper takes an advisory lock on the ID, looks the ID up by tenant, and compares a stored `intent_sha256` and `recorded_by`. The shell forms keep one ID for each set of submitted values.

**Tech Stack:** Go (gin, gorm, Postgres), Next.js shell, `@helivanta/api`, Vitest, Playwright support helpers.

**Spec:** `docs/superpowers/specs/2026-11-14-pharmacy-tax-command-identity-design.md`

## Global Constraints

- Routes: `PUT …/outlets/{outlet}/tax-registrations/{uuidv7}` and `PUT …/catalogue/products/{id}/tax-classifications/{uuidv7}`. The POST routes are removed.
- Responses: 201 for a new row, 200 for a replay, 409 for any other row under the ID. Nothing is written on a replay or a 409.
- The replay lookup runs after the scope checks and before any date, format or reason validation.
- The hash is SHA-256, with fields joined by `\x1f`, as in `putInvoiceProfile` (`sale.go`).
- Migration ID: `0048_pharmacy_tax_command_identity` (codex holds 0047 on `codex/pharmacy-prescription-evidence`). It adds a nullable `intent_sha256 text` to both tables.
- Bodies, permissions and the response row are unchanged.

## Review Focus

- A replay after the outlet's date has moved past `effective_from` must return 200, not 400. Covered in Task 1, Step 1.
- The same ID reused by a different owner must be 409, never a replay. Covered in Task 1, Step 1.
- The same ID sent for a different product or outlet must be 409, not a second row. The primary key includes `outlet_id`. Covered in Tasks 1 and 2.
- After a failed request, editing the values and resubmitting must send a new ID, so the user doesn't get a 409. Covered in Task 3.
- Two concurrent requests with the same ID must produce one row. Covered in Task 1, Step 1.

---

### Task 1: Registration PUT with replay, plus the migration

**Files:**

- Create: `backend/internal/modules/pharmacy/tax_command.go`, `tax_command_identity.sql`
- Modify: `backend/internal/modules/pharmacy/tax_registrations.go`, `module.go` (migration list), `tax_registrations_test.go`, `overview_test.go:196`
- Test: `backend/internal/modules/pharmacy/tax_command_test.go`

**Interfaces:**

- Produces: `func taxIntent(fields ...string) string` returns the hex SHA-256 of the fields joined by `\x1f`.
- Produces: `func priorTaxCommand(tx *gorm.DB, table, tenant string, id uuid.UUID, intent, subject string) (replay bool, err error)`. It takes `stockLock(tx, tenant+":tax-command:"+id.String())` and reads `intent_sha256, recorded_by FROM <table> WHERE tenant_id=? AND id=?`. It returns `(false, nil)` when no row is found, `(true, nil)` when the intent and recorder are equal, and `errStockIdentity` otherwise. A NULL intent never matches.
- Produces (tests): `func taxCommandPath(base string) string`, which returns `base + "/" + uuid.Must(uuid.NewV7()).String()`.
- `taxError` gains `case errors.Is(err, errStockIdentity): respond.Conflict(c, "this tax command id was already used with different details")`.

- [ ] **Step 1: Write the failing tests** in `tax_command_test.go`, using `taxHarnessDB`. Each test sends `PUT registrationsPath(pharmaOutlet)+"/"+id`:
  - `TestTaxRegistrationResendReplays`: the same body twice, with an empty reason and again with `"TEST reason"`. The first call gets 201 and the second 200 with the same `id`, and `listRegistrations` has length 1 (one row per subtest, each in a fresh harness).
  - `TestTaxRegistrationReplayAfterItsDate`: record with `effective_from` 2026-11-01, then move the clock to 2026-11-03 with `pharmacy.SetClock`. The same PUT gets 200.
  - `TestTaxRegistrationIDReuseConflicts`: three cases, each 409 with the row count unchanged: a different identifier (`27PQRST5678K2Z1`, jurisdiction `27`); the same body from `"editor"` after `"owner"`; and the same body under `registrationsPath(schemelessOutlet)` or a second seeded outlet. Use whichever reaches the lookup; if the schemeless outlet refuses earlier with 409 `tax_scheme_unavailable`, seed a second Indian outlet through `PUT /pharmacies/:pharmacy/outlets/:outlet`.
  - `TestTaxRegistrationConcurrentResend`: two goroutines send the same PUT. The status codes are {201, 200}, and the count is 1.
  - `TestTaxRegistrationStoresItsIntent`: after a 201, `SELECT count(*) FROM pharmacy_outlet_tax_registrations WHERE intent_sha256 IS NULL` through `db` is 0.
- [ ] **Step 2: Run** `GOTOOLCHAIN=go1.27.4 go test ./internal/modules/pharmacy -run 'TestTaxRegistration(Resend|Replay|IDReuse|Concurrent|Stores)' -count=1`. Expected: FAIL with 404, because there is no PUT route yet.
- [ ] **Step 3: Implement.**
  - The migration: `ALTER TABLE … ADD COLUMN intent_sha256 text` on both tables, with a comment that rows written before #1715 carry none and can never be replayed. Register it as `{ID: "0048_pharmacy_tax_command_identity", SQL: taxCommandIdentitySchema}`.
  - The route becomes `g.PUT("…/tax-registrations/:id", …)`, with the ID parsed as in `catalogueID`.
  - In the transaction: `pharmacyVisible`, then `outletTaxContext`, then `priorTaxCommand` with the intent `taxIntent(outlet, jurisdiction, NormalizeRegistration(identifier), effective_from, reason or "\x00none")`. A replay skips straight to the existing select; otherwise the existing checks run and the insert sets `IntentSHA256`.
  - Return 201 for a new row and 200 for a replay.
- [ ] **Step 4: Move the existing callers** to PUT with `taxCommandPath`: every `"POST", registrationsPath(...)` in `tax_registrations_test.go`, and `overview_test.go:196`. Run `go test ./internal/modules/pharmacy -run 'TestTax|TestOverview' -count=1`. Expected: PASS.
- [ ] **Step 5: Mutations, each reverted.** Comparing only the intent fails IDReuse (the different-recorder case). Removing the `stockLock` fails Concurrent within 10 runs (`-count=10`). Validating the date before the lookup fails ReplayAfterItsDate.
- [ ] **Step 6: Commit** `fix(pharmacy): replay a resent tax registration instead of writing it twice (#1715)`.

### Task 2: Classification PUT with replay

**Files:**

- Modify: `backend/internal/modules/pharmacy/tax_classifications.go`, `tax_classifications_test.go`, `release_test.go:65`, `sale_test.go:103,486`, `service_charges_test.go:298,302`, `supplier_receiving_corrections_test.go:369`, `backend/internal/globalapi/supplier_fulfilment_test.go:345`
- Test: `backend/internal/modules/pharmacy/tax_command_test.go`

**Interfaces:**

- Consumes: `taxIntent`, `priorTaxCommand` and `taxCommandPath` from Task 1.

- [ ] **Step 1: Write the failing tests:** `TestTaxClassificationResendReplays` (201 then 200, with one row from `GET classificationsPath`), `TestTaxClassificationReplayAfterItsDate`, and `TestTaxClassificationIDReuseConflicts`. The conflict cases are a different rate, a different recorder, and the same ID and body under a second product created with `PUT /catalogue/products/:id`.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -run TestTaxClassification -count=1`. Expected: FAIL with 404.
- [ ] **Step 3: Implement.**
  - The route becomes `s.PUT("/catalogue/products/:id/tax-classifications/:classification", …)`. The second ID is parsed as a UUIDv7, and a bad one gets 404, as `registryID` answers.
  - After the product-in-outlet check, call `priorTaxCommand` with the intent `taxIntent(outlet, product, code, nature, rate_ppm, basis or "\x00none", effective_from, reason or "\x00none")`.
  - Return 201 or 200 as in Task 1.
- [ ] **Step 4: Move every listed caller to PUT.** Each caller appends a new UUIDv7 to the path. The globalapi test builds the path with `uuid.Must(uuid.NewV7())`. Run `go test ./internal/modules/pharmacy ./internal/globalapi -count=1 -timeout=20m`. Expected: PASS.
- [ ] **Step 5: Mutation.** Dropping `product` from the intent fails the second-product case. Revert it.
- [ ] **Step 6: Commit** `fix(pharmacy): replay a resent tax classification (#1715)`.

### Task 3: Shell forms keep one ID per submitted command

**Files:**

- Create: `apps/shell/components/pharmacy/command-id.ts`
- Modify: `apps/shell/components/pharmacy/tax-registration-panel.tsx`, `tax-classification.tsx`, their `.test.tsx` files, `e2e/tests/support/pharmacy-counter.ts:231,257`

**Interfaces:**

- Produces: `export function useCommandId(): (values: unknown) => string`. It keeps a ref of `{key: JSON.stringify(values), id}` and returns the same ID while the key is unchanged; otherwise it stores and returns `newPharmacyId()`. Forms remount after a success (`key={history.items.length}`), so the next entry gets a new ID.
- The forms' `submit` prop becomes `(values: Record<string, string>, id: string) => Promise<…>`. The parent sends `PUT \`${root}/tax-registrations/${id}\``and`PUT \`…/tax-classifications/${id}\``.

- [ ] **Step 1: Write the failing tests**, one set in each panel's test file:
  - The mocked fetch records `(method, url)`.
  - Submit, fail with a 500, then submit the same values again: two PUTs to the same URL ending in a UUIDv7.
  - Submit, fail, change the identifier (or the rate), then submit: the second URL is different.
- [ ] **Step 2: Run** `pnpm --filter shell test -- tax-` (with the toolchain from memory). Expected: FAIL, because the method is POST.
- [ ] **Step 3: Implement** the hook and the two `submit` changes. Change the e2e support calls to `PUT …/${randomUUIDv7}` using the `uuid` package `v7`, which is already a dependency.
- [ ] **Step 4: Run** `pnpm turbo lint type-check test --filter=shell --filter=e2e`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(shell): send tax setup with one command id per entry (#1715)`.

### Task 4: Contracts and docs

**Files:**

- Modify: `docs/api/domains/pharmacy.md` (tax setup row, line 38), `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md` (F-7 row, SET-14), generated `docs/api/reference/*`

- [ ] **Step 1:** Add to the tax setup row: both commands are `PUT …/{uuidv7}`, an identical resend by the same person replays 200, and any other reuse is `409`.
- [ ] **Step 2:** Mark F-7 Done (#1715). SET-14 expects one row, and the second click is a replay with no error.
- [ ] **Step 3: Run** `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`. Expected: the reference shows the two PUTs, with no POST, and the check passes.
- [ ] **Step 4: Commit** `docs(pharmacy): tax setup commands are client-identified (#1715)`, including the spec and this plan.

Final gates: `make lint-go`, `./scripts/coverage-gate.sh`, `go test -race ./internal/modules/pharmacy`, `pnpm turbo lint type-check test build format:check`.
