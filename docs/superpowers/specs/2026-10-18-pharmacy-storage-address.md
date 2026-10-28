# Quarantine storage addresses — #1229

Decision and boundaries: [ADR 0033](../../adr/0033-pharmacy-storage-address.md).

Storekeepers enter optional zone/aisle/rack/shelf when creating quarantine
locations. API PUT, list, stock position and reusable UI choices/register
show the same normalized values. Missing addresses display “Address not
recorded”. Coordinates never imply storage qualification or shared access.

Acceptance: old payload/row compatibility; bounded ASCII labels including
independent shelf; malformed input handling; exact/concurrent replay without
extra audit; changed identity conflict; tenant/outlet read/write isolation;
stock-position projection and frozen snapshot/cursor behavior; accessible
inline validation; stable retry IDs and scope clearing. Verify PostgreSQL
and genuine local browser/API flows with fictional India fixtures. Keep
#1229 open until release verification.
