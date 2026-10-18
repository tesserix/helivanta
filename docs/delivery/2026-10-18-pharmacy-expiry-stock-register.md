# Pharmacy expiry stock register — #1226

Parent #167; procurement epic #1133; exploration tracker #979.

## Scope and authority

Extend the existing tenant/outlet-scoped stock register, rather than creating another inventory master. PostgreSQL derives signed calendar days from each batch's expiry date and the saved outlet timezone at the explicitly frozen stock snapshot. Server-derived bands are expired, today, 1–30, 31–90, 91–180 and later. Cumulative due filters include today and exclude already expired stock. Positive-on-hand filtering is independent of date filtering; zero-balance history remains available.

This is inventory visibility. Bands are operational search windows, not approved country/category policy, supplier-return eligibility or permission to dispense. Existing stock remains quarantined with zero dispensable units. Automated sweeps/notifications, expiry policy configuration, valuation, destruction/write-off and stock-release gates remain under #167 and their owning stories.

## Capacity and consistency

Planning assumptions, not measured production guarantees: 20 peak register reads/s, 50 rows/page below 64 KiB, 20:1 read/write ratio, 100,000 movement rows/outlet at 12 months and 1 million at 36 months (10× growth), 99.9% monthly availability and p99 under 500 ms for one indexed outlet. Reuse the existing movement aggregation and cursor ordering; filtering happens before pagination. No new datastore, queue, timer, cache or synchronous external call is needed for a derived read. PostgreSQL and authorization are critical dependencies: errors deny the request; no cached private result is used as fallback. NATS/Temporal/Valkey outages add no new dependency to this read.

## Security

Assets are private stock quantities, product/lot identity and commercial history. Attackers include a foreign-tenant user, an unassigned user in the same tenant and an operator with revoked outlet access. The route retains `pharmacy.stock.read`, authenticated tenant selection, expected-tenant preconditions, exact pharmacy/outlet authorization and FORCE RLS. Location/product references are checked inside the scoped transaction. The browser never determines the authoritative timezone or expiry band. Unknown filter values and conflicting repeated values are rejected. No history, eligibility or inventory mutation is added.

## Delivery and recovery

Additive response fields and query options only; existing expiry_status and all/expired/unexpired filters retain their meaning. No schema migration or infrastructure change. Roll back application code through the approved GitOps release path if necessary; no persisted business state needs recovery. Incremental infrastructure cost is zero; existing indexed aggregation remains the query cost.

Local and hosted validation, genuine API/browser acceptance and release evidence will be attached to the PR/issue. Keep #1226 open until approved deployment and verification, and keep #167 open for the remaining lifecycle acceptance.

## Legacy comparison

Read-only browser review on 18 October 2026: `http://59.94.33.26/mednet/` → Inventory → Store (`StoreHome.htm`). The stock grid exposes stock-on date, Managed/Expired/Unmanaged, store, category/subcategory/manufacturer, item name/code, expiry date, batch, MRP/EPR, rack/shelf, stock and vendor. The current response had no stock rows; no customer records were copied. Item Ledger and Stock Posting Exceptions are separate navigation entries. This increment adds explicit cumulative due windows and positive-balance filtering to Helivanta's existing location/lot snapshot and reconciliation. Rack/shelf, valuation, managed-stock definitions, supplier-return handling and disposition remain separate acceptance under the broader inventory stories.

The snapshot freezes movement quantities and the evaluation timestamp. The timezone and product/location labels come from current saved outlet records, as the register already states; this does not reconstruct historical timezone configuration.

## Validation so far

- Red/green PostgreSQL tests: new response fields first failed; next window/positive-balance tests failed before their implementation. Final focused register suite passes.
- `go build -o /tmp/helivanta-1226-api ./cmd/api`, `go vet ./internal/modules/pharmacy`, `golangci-lint run ./internal/modules/pharmacy/...`: pass (0 lint issues).
- `pnpm turbo lint format:check type-check`: all 15 tasks pass, including generated-contract drift checks.
- `node /tmp/helivanta-1217-identity/run.mjs build pnpm turbo build`: Next.js 16.4.0 production build passes using the isolated local identity configuration.
- Genuine local API acceptance: 130 successful checks across IN/PK, 20 batches/receipts, 2 net-zero corrections, all date windows, two-row cursor pages, reader access, no-store headers, invalid/repeated filters and foreign-outlet references. API rate limiting remained enabled; the helper respects bounded Retry-After retries.
- Browser against the production build and real local OIDC session: 30-day filter returns today, day 1 and day 30 only, positive balance composes without changing snapshot, and Inspect lot scrolls/focuses reconciliation with closing stock 5.

Full Go/TypeScript suites, pharmacy race tests and final browser country/scope checks are still running. No production rollout, schema migration or live stock operation has been performed by this increment. Closure requires approved deployment and deployed verification.
