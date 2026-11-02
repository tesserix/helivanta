# Staff department handoff implementation

Follow the [command and owner specification](../specs/2026-11-02-staff-department-handoff.md).

- [x] RED: a normal Workforce manager creates and recovers a pending mapping.
- [x] Canonical command/receipt/events and additive owner migrations.
- [x] Workforce commands/history and retained Organization observation protocol.
- [ ] RED/green: wrong scope, stale/duplicate/concurrent intent and immutable SQL.
- [x] RED/green: contradictory/reordered/malformed observations and rollback.
- [x] Real OpenFGA revocation/outage and owner-to-owner payload acceptance.
- [x] Canonical SDK validation and normal HR setup selection/history/retry UI.
- [x] Browser acceptance with synthetic local fixtures and persisted proof.
- [x] Regenerate/check contracts and inventory; tests/build/lint; progress record.

No completion or production activation from a pending request or matched snapshot.

Evidence: [local verification](../../qa/2026-11-02-staff-department-handoff-local.md).
Manager mapping/history and final guards pass. The remaining command matrix now passes: stale HR source is refused without a
receipt; an exact historical retry remains stable; actor rebinding is refused;
two concurrent new command keys commit one pending mapping/version and audit.
Removing the actor guard makes its regression fail (200 instead of 409). The
existing reviewer passed read-only HR/history and mapping-control denial in the
browser; positive mapping-reader access and canonical posting/migration consumers
remain open.
