# Outlet low-stock warnings — #1685

The original storage/availability implementation shipped in #1761. Its broad
release gates did not expose the workflow. The [activation design](2026-11-24-low-stock-activation.md)
supersedes the broad-gate UI/release discussion below and adds audited narrow
projections; remaining activation acceptance is tracked in #1685.

## Owner, scope and decision

PharmaConnect owns reorder levels and reads its existing stock ledger. Clients
enter through Global API. The verified tenant, pharmacy and explicit current
outlet grant are mandatory; neither hospital affiliation nor tenant ownership
supplies an outlet grant. Reads require `pharmacy.stock.read`; configuring levels
also requires `pharmacy.catalogue.write` and outlet editor access. This is an
operational warning, not permission to dispense or an automatic order (#1144).

Assets are commercial inventory availability and accountable configuration.
Unauthenticated callers, other tenants and unauthorized insiders must cross the
Global API/session boundary and owner permission/outlet checks; supplied scope
IDs and cached UI permissions never establish authority.

Keep append-only per-product level revisions separate from catalogue identity.
Legacy catalogue clients cannot accidentally erase a level. Each UUIDv7 command
records minimum issue units, expected level version, expected product version,
unit, actor and timestamp. Null disables the warning; zero is a configured level
that warns on zero availability. A product with no configuration is not counted.
Only active products with a level in their current unit are counted. Changing a
unit before first stock invalidates the old level and requires explicit resaving;
stocked-product identity already cannot change under the inventory guard.

Reject storing a threshold in ProductFields: old callers replace the identity
fields and would reset a new field. Reject using physical on-hand: quarantine,
expired stock and supplier holds would conceal shortages. Reuse
`pharmacy_sellable_buckets`, the stock balance's lot/location availability rule:
released location, printed MRP, expiry strictly after outlet-local today, no
safety incident, less live supplier holds. Product/licence/tax dispensing gates
remain separate: this warning does not assert that a sale is authorized.

## API and recovery

Under `/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`:

- GET `/catalogue/products/{id}/reorder-level`: current level or null; product
  existence is checked. GET `/catalogue/products/{id}/reorder-levels/{command}`
  returns the immutable command receipt under current authorization.
- PUT `/catalogue/products/{id}/reorder-levels/{command}`: strict generated input
  `{minimum_units: integer|null, expected_version, product_version}`. Minimum is
  0–1,000,000,000 issue units; versions are bounded safe integers. Replay compares
  all submitted fields and actor and returns the original receipt even after a
  later change. Changed command content/actor or stale versions returns 409.
- GET `/stock/low-products`: standard bounded keyset list, newest product first;
  each row includes product name/SKU/unit, configured minimum, exact decimal-string
  available units and observation instant/local date/timezone. Includes products
  with no batches. No historical `as_of` claim. Unknown/repeated query fields fail.
- GET `/overview`: additive `stock.low_stock` count uses exactly the same query.
  Missing stock permission omits stock; dependency failure never reports zero.

Writes serialize on the product row; a database trigger enforces revision sequence,
current product version/unit, scope and actor. Revision, audit and receipt are one
transaction. Ambiguous saves retain the exact command/input; recovery reads that
command before any resubmission. Authoritative 404 offers an explicit identical
retry, never a fresh command. Background refreshes cannot replace an editing baseline or
unmount a pending command. After a confirmed save, a newer server revision
requires an explicit reload of the product and level before editing again. Every
request reauthorizes. No stock movement or
event is created by configuring a level.

## Capacity and failure behavior

Use the existing planning envelope (1,000/3,000 peak app RPS at months 12/36,
80:20 reads:writes, 2–20 KB payloads, p99 read 500 ms). These are targets, not
measured low-stock performance. Expect at most 10,000 configured products/outlet
for acceptance load; quantify query plans on a hosted scratch database before
release. Cursor pages default 50/max 200; SQL budget 8 seconds and query deadline
10 seconds. No service/cache/worker is added; cost is indexed configuration history
and stock reads in the existing Postgres. Lists use one statement and snapshot;
separate requests can legitimately differ after stock/configuration changes.

Authorization, database and audit dependencies are critical and fail closed.
No external network calls occur under locks. Additive migration 0052 creates
owned configuration history and its restrictive governance-purpose policies. It
verifies the existing purpose-integrity fingerprint before extending it for the
new table, preserving native location-choice isolation; no existing data is backfilled or
reinterpreted. Roll back application readers/writers while retaining history.
No destructive down migration is appropriate for accountable configuration.

## UI, release and acceptance

Stock page has a low-stock view and configuration for a selected active product,
with current issue unit, explicit disable, version conflicts and uncertain-save
recovery. Overview links to that exact view. Names label products, never account
IDs. Existing `pharmacy_catalogue` and `pharmacy_stock` release gates protect the
new routes and controls. The overview API also requires Stock; its existing UI
Reports gate already depends on Stock. Catalogue alone must never expose the
new stock count. Both remain locked until their prerequisites and this
slice's hosted owner, gateway, browser and release checks pass.

Prove thresholds 0/equality/above, no stock, quarantine, today-expiry/local
midnight, safety incidents, supplier reservations, disable, pre-stock unit change,
foreign tenant/outlet/pharmacy, reader writes, missing permissions, revoked grants,
concurrent/stale changes, duplicate/changed retries, recovery after later changes,
strict input/query validation, keyset completeness, overview parity and audit
rollback. Run meaningful pure checks locally; run actual database/gateway/browser
checks in hosted CI and deployed QA. Do not start local apps or containers.
