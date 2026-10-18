# ADR 0033: Physical addresses on quarantine locations

Status: proposed for #1229, child of #162.

## Context

Read-only legacy review (18 October 2026) found RACK/SHELF beside stock,
lot and expiry; AssigningRackShelf.htm has Store, Category, Unit and import
controls. No assignment/import was executed and no customer data is copied.

Planning assumptions, not measured production promises: 100 tenants × 20
outlets × 100 locations = 200,000 locations at 12 months; 3× growth gives
600,000 at 36 months. Peak location traffic 20 reads/s and 2 writes/s,
payload below 2 KiB; target 99.9% availability and p99 API latency below
300 ms. Four codes add at most 128 ASCII bytes per location (~26/77 MB
raw at these volumes), with no new service or infrastructure cost.

## Decision

Extend pharmacy_stock_locations with non-null default-empty zone_code,
aisle_code, rack_code and shelf_code. The existing unique location code
remains the bin identifier; several bins may share a shelf. Each coordinate
is independently optional: standalone shelving does not require a rack.
Codes trim and uppercase to ASCII identifiers of at most 32 characters;
blank means unknown, never a fabricated address.

Existing PUT, location list and stock positions expose nested
storage_address from a shared generated contract. Omission maps to blank
values; output always contains four strings. Same ID and normalized
identity replays 200; a different address returns 409. Existing immutable
triggers protect history; inserts retain one actor-attributed audit record.
Coordinates confer no stock suitability or release approval.

Threat boundary: unauthenticated users, other tenants, and same-tenant staff
without outlet access must not read or relabel inventory. Existing
stock.receive plus explicit outlet editor checks guard PUT, stock.read plus
outlet access guards reads; WithTenant/FORCE RLS remain mandatory. No new
roles or ownership-based access. Existing unique constraints/advisory lock
serialize retries; insert and audit commit together. No cross-service
write, new event, queue, Valkey cache or Temporal workflow is needed.
Existing movement outbox/NATS behavior remains unchanged. PostgreSQL and
authz are critical: errors deny requests; no fallback discloses or creates
stock. Network timeout permits exact PUT retry with the same ID.

## Alternatives and consequences

A separate bin master duplicates inventory identity and is rejected.
Free-text addresses lose consistent labels and bounds. Coordinate indexes
and uniqueness add cost without a supported search/uniqueness requirement.
Existing scoped joins project fields without new lookups or pagination.

Forward-only migration 0008_pharmacy adds columns and checks with bounded
lock/statement timeouts. Existing rows retain empty metadata; no backfill
or historical UPDATE. Test old-schema upgrade and fresh boot locally.
Application rollback leaves columns and stored values intact; destructive
column removal is deliberately unsupported. Old binaries cannot display
the metadata but do preserve it in PostgreSQL. Production migration and
release require named approval after a tested PR is ready.

This slice excludes stock release, transfer, put-away/capacity/temperature
approval, layout imports, catalogue-to-rack mapping and warehouse automation;
parent issues remain open.
