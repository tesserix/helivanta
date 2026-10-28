# Counter sale slice B-1 implementation plan: the OTC sale

**Goal:** a pharmacist sells sellable OTC stock at the counter. One command allocates FEFO,
prices at batch MRP, computes GST, numbers the invoice, records tenders and decrements stock.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386), B1–B4 and B7–B11,
with the "Slice B pull requests" table: B-1 sells `none`-class products only and records no
customer data.

**Architecture:** migration `0015_pharmacy` (`sale.sql`). Every table is outlet-scoped through
`pharmacy_outlet_allowed`, with forced RLS and audit. All are insert-only except the invoice
series counter. The sale is one transaction, in `sale.go`. Movements of kind `sale` leave
sellable locations only, and only under a recorded sale line.

## Global constraints

- **Routes** are under `/pharmacy/pharmacies/:pharmacy/outlets/:outlet/`:
  - `PUT /invoice-profile/:id` and `GET /invoice-profile` (`pharmacy.tax.manage` to write,
    `pharmacy.sale.read` to read);
  - `POST /sales/quote` (`pharmacy.sale.create`, prices without posting), `PUT /sales/:id`,
    `GET /sales/:id`, `GET /sales` (paged) and `GET /sales/availability?product_id=`.
- **Permissions:**

  | Permission             | Holders           |
  | ---------------------- | ----------------- |
  | `pharmacy.sale.create` | pharmacist        |
  | `pharmacy.sale.read`   | pharmacist, owner |

  Writes also need the outlet `editor` grant. Other tenants and outlets get 404.

- **Today** is the outlet's local date (`outletLocalDate`). The financial year runs April to
  March in the outlet's date and is written `2627` for 2026-27.
- **Money** is in integer minor units. Per line:

  ```
  amount  = floor(mrp_minor * qty / pack_size)
  half    = round_half_up(amount * rate_ppm / (2 * (1_000_000 + rate_ppm)))
  cgst    = half
  sgst    = half
  taxable = amount - 2 * half
  ```

  For nil-rated or exempt lines, half = 0. The total is the sum of `amount`, with no round-off.

- **The invoice number** is `PREFIX/FY/N`, at most 16 characters. The prefix matches
  `^[A-Z0-9]{1,4}$` and comes from the profile in force. N is gap-free per outlet and FY. The
  counter row is updated inside the sale transaction, so a rolled-back sale returns its
  number. A number that would exceed 16 characters refuses the sale; it is never truncated.
- **Refusal** returns 409 with code `not_sellable` and a reason a cashier can act on. Nothing
  is partly sold.

## Sale preconditions (each refuses with its reason)

1. The outlet has a **retail** licence in force today
   (`pharmacy_outlet_retail_licence_in_force`).
2. A GST registration of kind `gstin` is in force today. Its state code is the place of
   supply, and it must be a state: a union territory (UTGST) is refused until it is modelled.
3. An invoice profile is recorded.
4. Every product is active, has a tax classification in force, and has a class in force of
   `none` that is not NDPS. G, H and H1 are refused with "needs prescription evidence, which
   the counter does not record yet". X and NDPS are refused as in A3.
5. FEFO allocation:
   - From buckets (sellable location, batch) of the product whose batch:
     - has an MRP;
     - expires after today;
     - has no safety incident.
   - Order: `expires_on`, batch id, then location id.
   - The candidate bucket advisory locks are taken first, sorted by bucket key, with the key
     the quantity guard uses. Two tills therefore serialise without deadlock, and the
     balances read afterwards are current.
   - Short stock refuses the sale and lists each short product.
6. Tenders:
   - Between 1 and 5 tenders, with kinds `cash`, `upi` or `card`.
   - At most one is cash; its `tendered_minor` must be at least its amount, and change is the
     difference.
   - Each UPI or card tender carries a reference of 1 to 64 characters.
   - The amounts must sum exactly to the total.
7. Lines: between 1 and 50, with distinct products and `quantity_units` in 1..1,000,000.

## Tasks

1. **Migration and invoice profile.**
   - Contents of `sale.sql`:
     - `pharmacy_outlet_invoice_profiles`: seq, legal_name, address, prefix, reason and
       recorded_by. The profile in force is the highest seq.
     - `pharmacy_invoice_series`: (tenant, outlet, fy) with next_number. Only the sale
       transaction updates it.
     - `pharmacy_sales`, `pharmacy_sale_lines` and `pharmacy_sale_tenders`, all immutable.
     - The movement kind gains `sale`.
     - The location guard is extended: a `sale` movement must be negative, come from a
       sellable location, and carry `source_reference = 'sale:<sale>:<line>'` naming an
       existing line with the same batch and location.
   - Tests:
     - profile replay and conflict;
     - the guard refuses a sale movement into quarantine or without a line.
2. **The sale command** (`sale.go`), covering preconditions 1–7, idempotent replay
   (`intent_sha256` and actor), and events.
   - Events:
     - `sale_posted.v1`: ids, invoice number, currency, total, and each line's product, batch
       and quantity;
     - `StockMovementRecorded` for each movement.
   - Tests:
     - FEFO across two batches, as two invoice lines;
     - the rounding values;
     - short stock;
     - each precondition refusal;
     - the series numbering 1, 2, …, with a refused sale consuming no number;
     - financial year rollover on 1 April in the outlet's time;
     - replay, and a conflicting replay;
     - cross-tenant 404;
     - two concurrent sales of the last unit: exactly one succeeds.
3. **Reads.**
   - The sale GET returns the invoice: seller snapshot, lines and tenders.
   - The list is paged newest first.
   - Availability sums the units a sale would allocate, with the nearest expiry, using the
     same predicate as allocation. One SQL function serves both, so they cannot diverge.
4. **Contract, docs and arch.**
   - The sale inputs are Go request structs, as the A3 release's are. No canonical
     `contracts/pharmacy.json` model is added until a second consumer needs one.
   - The pharmacy.md sections, the archtest matrix and allowlist, and the operational roles
     test.
5. **UI.**
   - The `/pharmacy/counter` route, and a "Counter sale" zone item under Dispensing
     (`pharmacy.sale.create`).
   - `CounterSale` component:
     - product search showing sellable units and nearest expiry;
     - basket and tenders;
     - the posted invoice, printable;
     - "NPPA ceiling prices are not checked" stated.
   - An invoice-profile form for `pharmacy.tax.manage` holders when none is recorded.
   - Vitest tests.

## Review focus

1. Allocation and availability must share one predicate, so the screen never offers units the
   sale then refuses for expiry or an incident.
2. Midnight in the outlet's timezone. A batch expiring today is not sellable, even at
   00:30 IST, which is still the previous day in UTC.
3. A refused sale after the series row has been created must not leave a gap.
4. A replay after the class changed, or the stock moved, returns the original invoice
   unchanged.
5. Rounding at rates 5%, 12% and 18% with odd paisa amounts. The halves must be equal, and
   taxable plus both halves must equal the amount.
