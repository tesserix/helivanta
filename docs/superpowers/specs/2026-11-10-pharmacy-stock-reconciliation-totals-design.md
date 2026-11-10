# Stock reconciliation totals that add up — design

- **Issue:** #1638 (finding F-4 of the counter acceptance plan, case STK-20)
- **Date:** 2026-11-10
- **Status:** proposed

## Problem

`GET …/outlets/{outlet}/stock/reconciliation` returns, for one batch at one
location and a period, `opening_units`, `receipt_units`, `correction_units`,
`closing_units` and `movement_count`
(`backend/internal/modules/pharmacy/stock_register.go:198-221`). Opening and
closing sum every movement; the two in-period totals cover only `receipt` and
`correction`. The movement kind constraint allows six kinds
(`fulfilment_stock.sql:10`):

| Kind                | Sign at this location                               | Written by                    |
| ------------------- | --------------------------------------------------- | ----------------------------- |
| `receipt`           | positive (`inventory.sql:72`)                       | goods receipt                 |
| `correction`        | either                                              | stock correction              |
| `release`           | negative at the source, positive at the destination | quarantine → sellable release |
| `sale`              | negative                                            | counter sale                  |
| `return`            | positive                                            | credit note return to stock   |
| `supplier_dispatch` | negative (`fulfilment_stock.go:127`)                | supply to a hospital order    |

The issue names five kinds; `supplier_dispatch` is the sixth and has the same
gap. Once a batch is released, sold, returned or dispatched, Opening +
Receipts + Corrections ≠ Closing, and the screen states the opposite
("Opening + receipts + corrections = closing stock",
`apps/shell/components/pharmacy/stock-register.tsx:569`). The reconciliation
cannot be used to reconcile.

The register rows (`GET …/stock/positions`) and the stock CSV export carry the
same partial pair, `receipt_units` and `correction_units`, beside
`on_hand_units`, from the shared `stockPositionsSource`. They are not presented
as a sum, but the export is the auditable record and does not foot either.

## Decisions

1. **One in-period total per movement kind.** The reconciliation response
   gains `release_units`, `sale_units`, `return_units` and
   `supplier_dispatch_units`, computed in the same statement as the existing
   totals (one snapshot, same `from`/`as_of` cutoffs). Each is the signed sum
   of that kind's `quantity_units` at this location, as stored. No sign is
   flipped: a sale of 5 is `-5`, a return of 2 is `2`, a release out is
   negative and a release in is positive. Signed values are what make the parts
   sum, and they match the ledger rows shown under the totals.
2. **Types follow the existing in-period totals:** the new fields are decimal
   strings, like `receipt_units` and `correction_units` (a `sum` over bigint is
   `numeric`). Opening, closing and the count stay numbers. No existing field
   changes, so this is an additive change to v1.
3. **The identity is enforced, not documented.** A Go list of reconciled kinds
   drives the SQL `FILTER` columns. A database test reads the live
   `pharmacy_stock_movements_kind_check` definition and fails when it names a
   kind the list does not, so a seventh kind cannot ship without its column.
   The handler test asserts `opening + Σ kinds = closing` on a batch that has
   every kind.
4. **The register rows and CSV get the same four totals** (all recorded history
   to `as_of`, as their existing two are), so `on_hand_units` = the sum of its
   six kind totals on every row and in the export. CSV columns are appended
   at the end, after `dispensable_units`, so existing column positions do not
   move for anyone reading the file by position.
5. **The screen shows every part with its sign and the correct formula.** The
   reconciliation shows Opening, then one line per kind in the order of the
   table above (Receipts, Corrections, Releases, Sales, Returns, Supplied to
   hospital orders), then Closing. Values keep their sign (`−5`, `+2`; zero is
   `0`). The formula line reads "Opening + the movements in this period =
   closing stock. Sales and supplies are negative; receipts and returns are
   positive; releases are negative where stock left and positive where it
   arrived." The register table's expanded row adds the four columns.
6. **Fail closed on a mismatch.** If the returned parts do not sum to closing
   (a server defect or a kind added without the UI), the panel shows the
   figures with an alert, "These figures do not add up. Do not use this
   reconciliation; report it.", rather than presenting it as reconciled. The
   sum is computed with `BigInt` from the strings, never floating point.

## Out of scope

- `dispensable_units` is always 0 (F-5, tracked separately: a different
  calculation).
- Splitting a kind further (for example sales by prescription or not), and
  money values in the reconciliation.
- Reconciliation across batches or locations; it stays one batch at one
  location.

## Contracts and docs

- `docs/api/domains/pharmacy.md`: the stock register/reconciliation section
  lists the response fields, their signs and the identity, and the CSV columns.
- `packages/api/src/pharmacy.ts`: `PharmacyStockReconciliation` and the
  position row type gain the four fields.
- No route, permission, event or migration changes; `go run ./cmd/contractdocs
-check` still runs.
- The acceptance plan: STK-20 expects the identity to hold on Q1 and S1, and
  F-4 is marked done by the PR.

## Tests

- Backend: one batch receives, is corrected, released from quarantine to a
  sellable location, sold, returned and dispatched; the reconciliation at each
  location asserts every kind total and `opening + Σ = closing`, with a `from`
  inside the history so opening is non-zero. Positions and CSV assert the same
  totals and `on_hand = Σ`. The kind-constraint test above. Existing scope tests
  (403/404/401) stay.
- Frontend: the panel renders each signed part and the formula; a response
  whose parts do not sum shows the alert; register rows show the new columns.
- E2E: `e2e/tests/pharmacy-stock-pane.spec.ts` checks the identity on screen after a
  release and a sale.
