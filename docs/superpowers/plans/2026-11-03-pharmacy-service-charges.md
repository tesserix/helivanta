# Pharmacy service charges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reviewed, versioned per-outlet service charges that a cashier adds to a counter or ward sale as their own taxed invoice lines, reversed by credit note and reported apart from medicine revenue.

**Architecture:** One migration (`0031_pharmacy`, `service_charges.sql`) adds charge, version and review tables, an in-force function, sale charge lines and credited charge lines, and redefines the sale and credit-note balance triggers to include them. Go handlers follow the #1371 declaration/review pattern; the sale, quote and credit-note handlers gain a `charges` array. The shell gets a setup section on the counter page and charge pickers on the counter and returns pages.

**Tech Stack:** Go (gin, gorm, pgx), PostgreSQL 16 with forced RLS and triggers, testcontainers; Next.js shell, `@helivanta/api`, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-03-pharmacy-service-charges-design.md`

## Global Constraints

- Migration ID `0031_pharmacy` (Codex holds 0029 in #1477 and 0030 in #1528); re-check before merge.
- Charge code `^[A-Z][A-Z0-9_]{1,19}$`; name 1–80; amount 1–100,000,000 minor, tax inclusive; SAC `^99[0-9]{4}$`; rate multiple of 100, 100–1,000,000 when taxable, else 0; `tax_basis` 5–500; exemption basis 1–500 exactly when exempt.
- Channels `counter`, `ward_delivery` only. Treatments `own_service`, `principal_supply`.
- At most 5 charges per sale, distinct; at most 5 credited charges per credit note.
- Tax on a charge line is `taxInside(amount, nature, rate)`; the schema checks `cgst = div(2·rate·amount + 2·(1e6+rate), 4·(1e6+rate))` for taxable lines.
- Permissions `pharmacy.service_charge.declare`, `pharmacy.service_charge.review` → `RolePharmacyOwner`; reads use `PermSaleRead`.
- Refusals at posting are 409 `not_sellable` / `not_returnable`; four-eyes 409 `four_eyes`.

## Review Focus

1. A review racing a sale: the sale names the quoted `version_id` and is refused (409) when another version is in force — `TestAChargeChangedUnderTheSaleIsRefused`, `TestAChargeChangedAfterTheQuoteIsRefused`.
2. Principal-supply charge on a basket with mixed GST rates is refused, and on nil-rated-only medicines it takes nature `nil_rated`, rate 0 — `TestPrincipalChargeFollowsTheMedicines`.
3. A charge credited twice (two credit notes) is refused by the unique key — `TestAChargeIsCreditedOnce`.
4. A counter charge on an admission sale (and vice versa) is refused — `TestAChargeKeepsToItsChannel`.
5. Pending latest version (including a withdrawal) stops the charge at once — `TestAPendingVersionStopsTheCharge`.

---

### Task 1: Configuration

**Files:** Create `backend/internal/modules/pharmacy/service_charges.sql`, `service_charges.go`, `service_charges_test.go`. Modify `module.go` (migration, permissions, routes via `catalogue.go`), `contracts/pharmacy.json` (`serviceChargeInput`), regenerate `input_types.go` and `packages/api/src/pharmacy-contract.ts`; `archtest/arch_test.go` (`unpaginatedGETAllowlist` for `GET /service-charges/:charge`), `archtest/matrix_test.go`; `operational_roles_test.go`.

**Interfaces — Produces:**

- Tables `pharmacy_service_charges(tenant_id,outlet_id,id,code)`, `pharmacy_service_charge_versions(..., charge_id, seq, name, channel, amount_minor, available, tax_treatment, sac_code, supply_nature, rate_ppm, exemption_basis, tax_basis, effective_from, intent_sha256, declared_by)`, `pharmacy_service_charge_reviews(version_id PK, decision, reason, reviewed_by)`.
- `pharmacy_service_charge_in_force(tenant, outlet, charge, day) RETURNS TABLE(version_id uuid)`: one row (the version) or none.
- Routes: `PUT /service-charges/:charge/versions/:version` (declare; first version creates the charge with `code`, later versions must repeat it), `POST /service-charges/:charge/versions/:version/review`, `GET /service-charges` (paged list, each with `in_force` version for today and `latest` version), `GET /service-charges/:charge` (history, newest 50).
- Go `serviceChargeVersion` JSON: `id, charge_id, code, name, channel, amount_minor, available, tax_treatment, sac_code, supply_nature, rate_ppm, exemption_basis, tax_basis, effective_from, declared_by, created_at, decision, decision_reason, reviewed_by, reviewed_at`.

- [ ] Tests first: declare → pending, not in force; second person approves → in force from `effective_from`; declarer cannot review (409 four_eyes); reviewing a non-latest version 409; same id different body 409; code mismatch on later version 400; form rules (principal with SAC → 400; own_service without rate → 400; exempt without basis → 400); withdrawal declared → charge stops immediately; cross-outlet 404; pharmacist cannot declare (403).
- [ ] Implement SQL (#1371 lock/review/in-force pattern; `pharmacy_service_charge_version_form` CHECK) and handlers.
- [ ] `go test ./internal/modules/pharmacy -run ServiceCharge` green; archtests green; commit.

### Task 2: Charges on sales and quotes

**Files:** Modify `service_charges.sql` (charge lines), `sale.go` (`saleInput.Charges`, `saleQuoteInput.Charges`, `.Channel`, digest, `priceCharges`, totals, insert, `loadSale`, error mapping), `service_charges_test.go`.

**Interfaces:**

- Input `charges: [{charge_id: uuid}]`; quote also `channel: "counter"|"ward_delivery"` (default counter).
- Table `pharmacy_sale_charge_lines(tenant_id,outlet_id,sale_id,charge_number 1–5, charge_id, version_id, code, name, tax_treatment, sac_code, supply_nature, rate_ppm, amount_minor, taxable_minor, cgst_minor, sgst_minor)`, unique `(sale_id, charge_id)`.
- Triggers: `pharmacy_sale_charge_line_guard` (constraint `pharmacy_sale_charge_source`: version is in force on sale date, available, channel matches sale, values copied; own_service copies SAC/nature/rate); sealed; deferred `pharmacy_sale_charge_principal` (every medicine line has the principal line's nature and rate); `pharmacy_sale_balanced` redefined to add charge totals.
- `saleRecord.Charges []saleChargeLine` (`json:"charges"`); quote returns `charges` too.
- Go `func priceCharges(tx, tenant, outlet, day, channel string, ids []uuid.UUID, lines []saleLine) ([]saleChargeLine, error)` returning `errNotSellable` for: not in force/withdrawn ("the service charge X is not available today"), wrong channel, principal with mixed medicine rates.

- [ ] Tests first: counter sale with an own_service charge posts lines + charge, totals and tenders include it, medicine line amounts unchanged; principal charge copies medicine rate; mixed rates refused 409; channel mismatch 409; pending version 409; DB guard refuses a hand-inserted charge with a wrong amount (constraint name asserted); tenders short by the charge 409; idempotent replay returns the same sale.
- [ ] Implement; map `pharmacy_sale_charge_source`/`pharmacy_sale_charge_principal` to 409 "a service charge changed; price the basket again".
- [ ] Tests green; commit.

### Task 3: Credit notes

**Files:** Modify `service_charges.sql` (credited charge lines, redefine `pharmacy_credit_note_balanced`), `credit_notes.go` (`creditNoteInput.Charges []{sale_charge}`, lines `min=0`, require lines+charges ≥ 1, digest, totals, insert, `loadCreditNote` charges), tests.

- Table `pharmacy_credit_note_charge_lines(tenant_id,outlet_id,credit_note_id,charge_number,sale_id,sale_charge,amount,taxable,cgst,sgst)`, unique `(tenant_id,outlet_id,sale_id,sale_charge)`; guard `pharmacy_credit_note_charge_source` (values equal the sale's charge line, sale is the note's).
- [ ] Tests first: credit a charge alone; credit with medicine lines; second credit of same charge 409; admission sale refunds charge to admission account.
- [ ] Implement; tests green; commit.

### Task 4: Daily report

**Files:** `daily_report.go`, its test.

- [ ] Test first: a day with a charged sale and a credited charge shows the four new fields exactly.
- [ ] Add CTEs over charge lines and credited charge lines; commit.

### Task 5: Shell

**Files:** `packages/api/src/pharmacy.ts` (types `PharmacyServiceCharge`, `PharmacyServiceChargeVersion`, `PharmacySaleCharge`, `PharmacyCreditNoteCharge`, sale/quote/credit-note/daily-row fields, export `pharmacyServiceChargeInputSchema`); create `apps/shell/components/pharmacy/service-charges.tsx` + test (list, declare form, review form); `counter-sale.tsx` (pick charges available for the sale's channel, quote and post with them, show charge lines on the invoice: SAC and rate, or "Supplied with the medicines and taxed as them"); `returns.tsx` (credit charges); `daily-report.tsx` (medicine and service rows); `pharmacy-workspace.tsx` (render setup on counter view); tests for each.

- [ ] Tests first per component; implement; `pnpm turbo lint type-check test build format:check` green; commit.

### Task 6: Docs and gates

- [ ] `docs/api/domains/pharmacy.md` section "Service charges (#1388)", `docs/api/portfolio.md`; `go run ./cmd/contractdocs` then `-check`; `node scripts/staff-operational-access-inventory.mjs` then `--check`; `make lint-go`; `go test -race` for pharmacy and archtest; commit.
