# NPPA ceiling prices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Outlets declare reviewed NPPA ceilings per product; the counter prices within them and the database refuses lines above them.

**Architecture:** Migration `0029_pharmacy` (`price_control.sql`) adds declarations, reviews, `pharmacy_product_price_in_force`, three sale-line columns and a sale-line price trigger. `price_control.go` holds the routes; `sale.go` reads the declaration in force in `saleProducts` and caps the list price in `allocate`. The shell adds a price-control section to the product record and a ceiling note on counter lines.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16, Next.js, zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-03-nppa-ceiling-price-design.md`

## Global Constraints

- Migration `0029_pharmacy`; re-check against open Codex PR #1477 (also `0029`) before merge.
- Permissions `pharmacy.price_control.declare` and `pharmacy.price_control.review`, pharmacist and pharmacy owner.
- Ceiling in paise per unit of issue, excluding GST, 1 to 1e12.
- Constraint names: `pharmacy_price_four_eyes`, `pharmacy_sale_line_price_unknown`, `pharmacy_sale_line_price_source`, `pharmacy_sale_line_ceiling`.

## Review Focus

- A newer declaration awaiting review blocks the product, even with an older approved one.
- A future-dated approved declaration leaves the earlier one in force until its date.
- Taxable lines whose GST rounding would put the taxable value one paisa above the ceiling.
- A discounted line on a capped price stays within both limits.
- An undeclared product still sells, and the invoice says the ceiling was not checked.

---

### Task 1: Declarations, reviews and the in-force rule

**Files:** Create `backend/internal/modules/pharmacy/price_control.sql`, `price_control.go`, `price_control_test.go`; modify `module.go` (migration, permissions, routes), `input_types.go` (`priceControlInput`).

- [ ] Test `TestAPriceControlNeedsASecondPerson`: declare controlled (ceiling 250, order `S.O. 1234(E)`, dated, effective today); `GET` says `blocked`; self-review `409 four_eyes`; another pharmacist approves; `GET` in force. Same id same body `200`, other body `409`; a controlled declaration without a ceiling `400`; an uncontrolled one with a ceiling `400`.
- [ ] Test `TestTheCeilingInForceFollowsItsDates`: approved A (effective yesterday), approved B (effective tomorrow) → A in force; pending C → blocked; rejected C → blocked until a new approved D.
- [ ] Run (FAIL), implement, run (PASS), commit.

### Task 2: The counter prices within the ceiling

**Files:** Modify `sale.go` (`saleProducts`, `allocate`, `saleLine`, insert, `loadSale`, quote), `price_control.sql` (sale-line columns and trigger); test `price_control_test.go`.

- [ ] Test `TestTheCounterSellsWithinTheCeiling`: a product with MRP above ceiling + GST is quoted and sold at the cap (`price_limit` `ceiling`, taxable ≤ ceiling × units); a product with MRP below it sells at MRP (`price_limit` `mrp`); a blocked product `409 not_sellable`; an undeclared product sells and `nppa_ceiling_checked` is false; all declared → true; a 10% discount on a capped line stays within both.
- [ ] Test `TestTheCeilingHoldsInTheDatabase`: direct inserts above the ceiling, with another declaration, or for a blocked product are refused by their constraints.
- [ ] Test `TestCeilingCapUnitsAndRounding` (internal): for rates 0, 5%, 12%, 18% and many ceilings and quantities, the cap is the largest amount whose `taxInside` taxable value is within the ceiling.
- [ ] Run, implement, run, mutation-check, commit.

### Task 3: Shell

**Files:** Create `apps/shell/components/pharmacy/price-control.tsx` + test; modify `catalogue-panel.tsx`, `pharmacy-workspace.tsx`, `counter-sale.tsx`, `packages/api/src/pharmacy.ts`; regenerate `pharmacy-contract.ts`.

- [ ] Section on the product: state in force, declare form (controlled toggle, ceiling in rupees, order, dates, source), history with Approve/Reject (reason) for others' pending declarations. Counter line shows "NPPA ceiling" when `price_limit` is `ceiling`.
- [ ] Vitest for the section and the counter note; commit.

### Task 4: Docs and gates

- [ ] `docs/api/domains/pharmacy.md` (replace the "not checked" line), `portfolio.md`; contractdocs, inventory, archtest; `make lint-go`, pharmacy `-race`, turbo lint/type-check/test/build/format:check.
