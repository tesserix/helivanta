# Pharmacy stock across outlets — #1372

## Ownership and scope

PharmaConnect owns existing outlet catalogue, batches, locations and movement
records. The staff shell consumes them through the Global API. The tenant comes
from the verified pharmacy principal, the pharmacy from the requested resource,
and every included outlet from its current `pharmacy_outlet_access` grant.
`pharmacy.stock.read` is required; directory management and affiliation confer
no stock authority. No new role vocabulary, event, consumer or stock master is
introduced. The existing JSON contract generates additional Go/Zod fields.

This is an operational read (IN-PH-01/03 and ORG-AC-01/02/07/08), with no
clinical or statutory policy change. A stock position is not dispensable stock.
The current supported jurisdiction remains India; expiry follows each outlet's
timezone. No new external legal, price, drug-schedule or tax rule is activated.

## Design

Add pharmacy-level `stock/positions` and `stock/positions.csv` routes and a
`/pharmacy/stock-overview` staff screen. Reuse the outlet register's SQL source
and expiry/balance predicates. The query explicitly restricts pharmacy, tenant
and current grants, in addition to forced RLS. Four inventory tables gain an
additional scoped read predicate in migration `0024_pharmacy`. The existing
single-outlet editor `WITH CHECK` stays intact. This ID deliberately leaves
`0023_pharmacy` for Claude's independent affiliation-verification branch.

Coverage, rows, product totals and currency totals use one read-only repeatable
read snapshot. Missing authority fails closed (404 without object scope, 403
without route permission, 503 when authority cannot be checked). A ten-second
query deadline and eight-second SQL statement timeout bound work. Standard
keyset pagination bounds rows across all granted outlets. A position's derived
key also includes its outlet because local IDs are not global identity.

Product totals are calculated before pagination, partitioned by owning outlet,
product and unit. Purchase value uses the existing receiving form's purchase
cost **per pack**: `floor(sum(units * known_pack_cost) / pack_size)`, rounded
once per product. PostgreSQL numeric arithmetic and decimal-string responses
preserve precision. Missing costs expose uncosted units; zero is a known cost.
Currency totals deduplicate the product totals before summing, never combine
currencies, and count incompletely costed products. Metadata arrays are bounded
to 20 currencies and refused above that bound; current country policy uses INR.

Coverage and currency values are bounded response headers so the collection
retains the standard list envelope, including an explicit scope warning for an
empty page. The client validates both headers and hides data on query failure.
Every page and export reauthorizes. The movement cutoff does not freeze grants
across later requests; revocation removes that outlet from subsequent pages.

CSV uses the same source, cutoff, filters and authorization, with the outlet
column and explicit partial coverage. It exports all matching accessible rows,
refuses above 1000 rows or 8 MiB, neutralizes formula-like text and logs the
actor/scope/cutoff/count/digest without cell values. Product totals repeat per
position and must not themselves be summed. No command or retry state changes:
reads can repeat; unavailable dependencies return an error, not a false empty
report. There is no stock compensation or outbox work for this read.

## Alternatives rejected

- **Client fan-out across outlets:** cannot enforce one server-side scope or
  calculate whole-result totals/pagination; misses unloaded outlet pages.
- **Widen the shared outlet authorization function:** would widen reads of
  prescriptions and financial records. Only four operational inventory-table
  read predicates change instead; write checks remain unchanged.
- **Multiple permissive RLS policies:** rejected by the repository's structural
  RLS gate. Preserve one policy, extending only its read predicate.
- **Merge equal SKUs or names:** those identifiers are outlet-local. They do not
  prove equal ingredients, strength, formulation, pack or manufacturer. Keep
  products distinct until authoritative shared product linkage exists.
- **Treat uncosted stock as free or use MRP as acquisition cost:** fabricates a
  financial value. Show known value and its incompleteness explicitly.

## Acceptance and limits

Synthetic tests cover two granted outlets and an excluded outlet; equal names
with different units; product totals across pages; no repeated currency value;
rounding across locations; missing costs; revocation; foreign tenant and missing
permission; invalid filters; dependency failure; empty-result coverage; read
context write rejection; bounded CSV and digest; page/filter navigation; precise
money formatting and unavailable metadata. Existing stock/expiry/export tests
exercise the shared source and outlet-local calendar rules.

This view does not transfer stock, provide reorder suggestions, consolidate
outlet-local catalogue identities, calculate landed cost/margin, or authorize a
sale. Shared drugmaster linkage remains a prerequisite for a single formulation
total across separately registered outlet products. #1372 should not be taken
as evidence that this unresolved identity mapping is implemented.
