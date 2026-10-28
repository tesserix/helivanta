# Counter sale slice B-3 implementation plan: discounts under the outlet ceiling

**Goal:** a holder of `pharmacy.sale.discount` gives a percentage discount on a counter sale
line, with a reason, within the ceiling the pharmacy owner recorded for the outlet.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386): B3, product decision
3, and "B-3 decisions" 1–5.

**Architecture:** migration `0018_pharmacy` (`discounts.sql`):

- `pharmacy_outlet_discount_ceilings`, insert-only;
- `pharmacy_outlet_discount_ceiling_in_force(tenant, outlet)`;
- new columns on `pharmacy_sale_lines`: `list_amount_minor`, `discount_bps`,
  `discount_minor`, with CHECKs;
- a new `discount_reason` on `pharmacy_sales`;
- a line guard extension that checks the ceiling;
- a deferred trigger that requires the reason when any line is discounted.

`priceLine` gains the discount. The handler checks the permission with
`authz.PermissionsFrom`.

## Global constraints

- **Routes:**
  - `PUT …/discount-ceiling/:id` takes `{max_bps 0–10000, reason}` (`pharmacy.discount.manage`,
    held by the owner).
  - `GET …/discount-ceiling` returns `{ceiling}` or `null` (`pharmacy.sale.read`).
- **Sale and quote line:** an optional `discount_bps` (1–10000).
- **Sale body:** `discount_reason` (5–200 characters), required exactly when a line has a
  discount.
- **Permission:** a discount without `pharmacy.sale.discount` is 403 `forbidden`, with "a
  discount needs the pharmacy.sale.discount permission". The check runs before any pricing.
- **Refusals** are 409 `not_sellable`:
  - "the discount on SKU is above this outlet's ceiling of N%";
  - "SKU: the discount leaves nothing to charge".
- **Not covered:**
  - fixed-amount discounts;
  - discounts on the whole sale rather than per line;
  - approval of a discount by a second person;
  - promotions and schemes.

## Review focus

1. A ceiling lowered between a quote and the sale: the sale is refused, never charged at the
   quoted discount.
2. A discount split over two batches rounds per batch line, within the ceiling, and the
   invoice total equals the lines.
3. A replay of a discounted sale returns the posted invoice. A changed discount or reason is 409.
4. A direct insert with a discount above the ceiling, or a discount without a reason, is
   refused by the database.
5. Historical B-1 and B-2b lines migrate with `list_amount = amount` and no discount.

### Task 1: Migration, pricing and API

- [ ] **Failing tests** (`discount_test.go`):
  - `TestAHolderDiscountsWithinTheCeiling`;
  - `TestNoCeilingMeansNoDiscount`;
  - `TestADiscountNeedsThePermissionAndAReason`;
  - `TestALoweredCeilingRefusesTheQuotedDiscount`;
  - `TestASplitDiscountRoundsPerBatchLine`;
  - `TestTheDatabaseHoldsDiscountsToTheCeiling`;
  - the replay case;
  - `TestSaleArithmetic` extended with discount cases.
- [ ] **Implement:**
  - the migration;
  - `priceLine(mrp, pack, qty, nature, rate, bps)`;
  - the ceiling routes;
  - the discount permission check and the inserts.
- [ ] **Run the tests and commit.**

### Task 2: Contracts and docs

- [ ] Update:
  - the archtest matrix (`pharmacy.sale.discount` with no system role,
    `pharmacy.discount.manage` for the owner);
  - the unpaginated allowlist for `GET discount-ceiling`;
  - `contractdocs`;
  - `docs/api/domains/pharmacy.md`.
- [ ] **Commit.**

### Task 3: Counter screen

- [ ] **Counter:**
  - a "Discount %" field on a basket line, only for `pharmacy.sale.discount` holders, showing
    the ceiling;
  - a reason field in the payment form when a line is discounted;
  - the invoice shows each line's list amount and discount, and the total discount.
- [ ] **Owners** record the ceiling, with a reason, beside the invoice details.
- [ ] **Tests:**
  - the discount is sent;
  - the reason is required;
  - the field is hidden without the permission;
  - recording a ceiling.
- [ ] **Run the frontend gate and commit.**
