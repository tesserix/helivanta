# Units available to dispense — design

- **Issue:** #1708 (counter acceptance finding F-5)
- **Date:** 2026-11-14
- **Status:** proposed

## Problem

Every stock surface reports `dispensable_units: 0`: the balance (`inventory.go:510`), each register and overview row (`stock_register.go:276`) and both CSVs. The stock panel labels it "Available to dispense", so a pharmacist sees 0 beside sellable stock. The contract calls the 0 "deliberately non-authoritative", but a figure a person reads is never non-authoritative.

## Goal

A dispensable figure appears only where it can be computed correctly, and there it equals what a sale could take from that stock.

## Decisions

### D1. The balance computes it with the sale's own predicate

`GET …/stock/balance` returns `dispensable_units` as the `on_hand` of the row that `pharmacy_sellable_buckets(tenant, outlet, batch.product_id, outlet today)` returns for this location and batch, or 0 if it returns none. This is the function the sale allocates from (`sale.go:494`), so the rules exist once:

- a sellable location;
- a batch with an MRP;
- an expiry after the outlet's local today;
- no safety incident on the batch;
- less any supplier hold.

It runs in the same read transaction as `on_hand_units`, so both figures come from one snapshot.

Product-level and outlet-level gates are left out on purpose: product status, schedule class, tax, NPPA ceiling, retail licence and GSTIN. They decide whether a product or outlet can sell at all, not how much of this batch can be sold. `GET …/sales/availability` and the sale itself still check them. The panel says so under the figure: "Before outlet and product checks."

### D2. Snapshots drop the field

Register rows, overview rows and both CSVs are snapshots `as_of` a requested time. A location's status, safety incidents and supplier holds are current state with no history, so sellability at a past `as_of` cannot be reconstructed. Rather than reporting a value that is true only when `as_of` is now:

- remove `dispensable_units` from the register and overview JSON rows and the TypeScript `PharmacyStockPosition`;
- remove the "dispensable" register column;
- remove the CSV column.

The CSV is a download read by people, not a versioned integration contract. It has no other consumer in the repository, so later columns shift left by one. The contract docs say so.

### D3. Contract docs

`docs/api/domains/pharmacy.md`:

- the balance row describes D1;
- the register and overview text drops "`dispensable_units=0` remains deliberately non-authoritative";
- the CSV column list is corrected.

The counter acceptance plan marks F-5 as done and verified by STK-24 and a new case for the balance.

## Out of scope

- Historical sellability, which would need versioned location status, incidents and holds.
- A per-product "available" total on the register. `GET …/sales/availability` already gives one.

## Tests

- **D1:** one batch split across sellable and quarantine locations. The quarantine balance reports 0 dispensable, and the sellable one reports its on-hand units. The sellable figure drops to 0 after a safety incident and after expiry, and reduces by a supplier hold. Mutation: return on-hand regardless of location, and the test fails.
- **D2:** register, overview and CSV rows have no `dispensable_units` key or column. Mutation: put the column back, and the test fails.
- **UI:** the panel renders the balance's figure and the note; the register has no dispensable column.

Gates: `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`, `go run ./cmd/contractdocs -check`, `pnpm turbo lint type-check test build format:check`.
