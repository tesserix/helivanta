# Directed Cross-Tenant Events — Design

**Date:** 2026-08-22
**Status:** Draft (brainstorming session with Mahesh)
**Resolves:** [#932](https://github.com/tesserix/helivanta/issues/932)
**Related:** #14 (multi-tenant data isolation), #5 (event-driven integrations),
#835 (PHI in event payloads), #170 (prescription fulfilment), #158 (lab partner network)

## Context

Every event on the platform today writes back into the tenant that published it.
`Bus.runConsumerTx` (`backend/pkg/events/bus.go`) sets `app.tenant_id` from
`evt.TenantID` and runs the handler inside that scope, so `pharmacy-visit-intake`
opens its pending dispense in the same hospital that opened the visit. That is
correct for every event that exists, and it is why a cross-tenant write has not
been possible.

Prescription routing breaks the assumption. A patient discharged from Hospital A
picks a pharmacy that is a **different Helivanta tenant**, and a purpose-limited
fulfilment request has to be created in that pharmacy's tenant. The same shape
recurs for third-party lab partners (#158) and referrals (#89, #96).

The attractive wrong answer is to widen `hms_tenant_visible` so the destination
can read the origin's rows. That turns a one-record handoff into standing read
access between two unrelated businesses — the precise failure #14 exists to make
structurally impossible, a purpose-limitation breach under the DPDP Act 2023, and
an access-control finding under NABH.

This slice delivers the primitive alone, with its enforcement and its adversarial
tests, before any clinical feature depends on it.

## Decisions

### D1. The envelope carries a destination distinct from the origin

`events.Event` gains one field:

```go
// DestinationTenantID, when set, is the tenant whose data this event
// creates. TenantID remains the ORIGIN — who published, and therefore
// who is accountable for the disclosure. Empty means same-tenant: the
// behaviour every event has today, unchanged.
DestinationTenantID string `json:"destination_tenant_id,omitempty"`
```

Origin and destination are both retained, always. Overloading `TenantID` to mean
"destination when directed" would erase the origin from the envelope, and the
origin is what makes a cross-organisation disclosure auditable at all.

`omitempty` keeps every existing event's payload byte-identical, so no stored
outbox row changes shape and no consumer sees a new field.

### D2. The consumer transaction is scoped to exactly one tenant

`runConsumerTx` resolves the scope as: destination when set and permitted (D4),
origin otherwise. Never both, never neither.

This is a one-expression change at the existing `set_config('app.tenant_id', …)`
call. It is deliberately not a new code path: a second transaction shape for
directed events would be exercised only by the newest feature and would rot.

### D3. The outbox row stays pinned to the origin

`Publish` continues to derive `outbox_events.tenant_id` from `evt.TenantID`.

This is not an oversight and must not be "fixed" later. The `tenant_isolation`
policy on `outbox_events` (migration `0003_events_outbox_tenant_check`) has
`WITH CHECK (tenant_id IS NULL OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`.
Publishing happens inside the origin's business transaction, so a row stamped
with the destination would be **rejected by the policy** and the business write
would fail. The destination travels in the payload; the row belongs to the
publisher.

A useful consequence: the origin tenant can read its own outbox and see exactly
what it disclosed and to whom. The destination cannot read it at all.

### D4. The allowlist is declared per module, checked at boot, and enforced at runtime

Two layers, because the first is structural and the second is fail-closed.

**Declaration.** `platform.Module` gains:

```go
// DirectedSubjects declares which of this module's Publishes() may carry
// a DestinationTenantID. Must be a subset of Publishes(), and entries
// must be contract constants — the same rule Publishes() already has.
// A module with no cross-tenant events returns nil.
DirectedSubjects() []string
```

**Boot.** `Registry.Register` (or a validation pass over `All()`) fails if any
entry is absent from that module's `Publishes()`. Boot failure is second on the
enforcement ladder in `docs/standards/engineering-principles.md`; a documented
convention is last, and would not be a control.

**Runtime.** The `Bus` holds the union of declared directed subjects, set from
the registry during startup. It **defaults to empty**, so a subject is
non-directed unless something declared it. When an event carries a
`DestinationTenantID` on a subject that is not in the set, the bus refuses the
message rather than honouring it — logged, dead-lettered, no write. A misdeclared
subject fails loudly instead of quietly performing a cross-tenant write.

*Rejected: a boolean on the event, trusted at face value.* The destination would
then be whatever a publishing call site passed, and a cross-tenant write would be
one typo away in any of ~30 modules. The whole point is that the set of subjects
allowed to cross a tenant boundary is small, reviewed, and greppable.

*Rejected: allowlisting by consumer instead of by subject.* The consumer is the
receiving side; the disclosure decision belongs to the publisher's contract.

### D5. Provenance is a column, not a convention

Any table that accepts directed writes carries:

```sql
origin_tenant_id uuid NOT NULL,
origin_record_id uuid NOT NULL
```

Modules declare them, as a second `platform.Module` method:

```go
// DirectedWriteTables declares every table this module's consumers write
// into on behalf of another tenant. Each must carry NOT NULL
// origin_tenant_id and origin_record_id; LintRLS enforces it.
DirectedWriteTables() []string
```

`LintRLS` (`backend/pkg/tenantdb/db.go`) gains a rule over that set: a declared
directed-write table whose two provenance columns are missing or nullable is a
lint failure, in the same pass that already runs at boot (`cmd/api/main.go`) and
in `cmd/migrate`.

Declaring the tables rather than inferring them is deliberate. Which table a
handler writes into is not statically knowable from a `tx.Exec`, so inference
would silently cover nothing the day a handler gained a second `INSERT`.

"Where did this row come from, and under whose disclosure?" is then answerable
from the row itself. Reconstructing it from logs is not equivalent: logs are
retained for a window, and the question gets asked during an audit years later.

### D6. `hms_tenant_visible`, `WITH CHECK`, and the widening lint rule are untouched

No policy is altered. `LintRLS` keeps failing any table whose `WITH CHECK` calls
`hms_tenant_visible` — reads may widen, writes stay pinned to one tenant.

**That this design requires no relaxation of that rule is the evidence it is not a
widening.** Data crosses by copy, with provenance, into a row owned by exactly one
tenant. If a future change to this subsystem needs that rule loosened, the change
is wrong, not the rule.

### D7. An unusable destination is terminated, not redelivered

A `DestinationTenantID` that is present but not a UUID is a poison envelope:
the message is `Term()`'d — stopping redelivery, with no write attempted — and
the failure is logged, matching how an unparseable payload is already handled.
This is NOT a publish to the `.dlq.<consumer>` subject; that only happens on
the separate `maxDeliver`-exhaustion path. The evidence is not lost even so,
because the outbox row that produced the message survives, unmodified, in the
origin tenant.

A syntactically valid destination cannot yet be checked for existence — there is
no tenant table until #13. See Limitations.

### D8. The proof of wiring lives in the `reference` module

`reference` exists to prove the full stack end to end (issue #2). It gains a
directed publish and a directed consumer, so S4 (prescription routing) inherits a
primitive that has already been exercised adversarially rather than one being
debugged inside a clinical feature.

## Error handling

| Condition | Behaviour |
|---|---|
| Destination set, subject not allowlisted | Refuse: log, `Term()` (not redelivered, not published to DLQ), no write |
| Destination present but not a UUID | Refuse: log, `Term()` (not redelivered, not published to DLQ), no write |
| Destination valid, handler errors | Existing Nak / `maxDeliver` / DLQ path, unchanged |
| No destination | Existing same-tenant path, unchanged |
| Directed write missing provenance | Rejected by `NOT NULL`; handler error → DLQ |

Every failure mode ends with no partial write, because the handler already runs
inside the idempotency claim's transaction.

## Testing

Each assertion below must be shown capable of failing — by mutation, not by
inspection — per `docs/standards/engineering-principles.md`.

1. **Isolation, asserted on rows.** A directed write lands in the destination
   tenant, and the same query run as the **origin** tenant returns zero rows.
   This is the claim the whole design exists to support; it is asserted against
   the rows produced, not against the code that produced them.
2. **Boot fails on an undeclared directed subject.** A module declaring a
   `DirectedSubjects` entry absent from `Publishes()` fails registry validation.
   Reverting the check must make this test pass a tree that should not boot.
3. **Runtime refusal.** An event carrying a destination on a non-allowlisted
   subject writes nothing and dead-letters. The bus's default-empty set means
   this is also what happens if the registry wiring is ever dropped.
4. **Provenance is mandatory.** A directed insert omitting `origin_tenant_id` or
   `origin_record_id` is rejected by the database, not by the handler.
5. **Lint catches a nullable provenance column.** Mirrors
   `TestLintRLSFlagsWideningWithCheck` in `pkg/tenantdb/db_test.go`.
6. **Invalid destination dead-letters** with no row written in any tenant.
7. **Regression: same-tenant events are byte-identical.** Existing consumer tests
   pass unchanged, and a published envelope with no destination serialises exactly
   as it does today.

## What this slice does not cover

Stated explicitly, per the scope-down-never-quality-down rule:

- **No prescription semantics.** No prescription record, line items, versioning
  or signing — that is S1 (#83, #84).
- **No pharmacy directory, price or availability publication** — S3 (#19, #160).
- **No holds, no routing state machine, no patient identity matching** — S4 (#170).
- **No patient-facing surface** — S5 (#11, #73).
- **No tenant relationships or ownership groups** (#16). Independent track.
- **No destination-existence validation.** Deferred to #13; see below.

## Limitations and what is not verified

- **A directed write to a non-existent tenant creates an orphan row.** With no
  tenant table (#13 unstarted), a syntactically valid but unknown destination
  produces a row visible to nobody. That is safe — it leaks to no one — but it is
  silent, and a routing feature would report success. The existence check is a
  follow-up gated on #13 and must land before S4 goes to production; S4's spec
  should carry that as a dependency rather than assume it.
- **NOT VERIFIED: behaviour under a destination tenant whose RLS context differs
  in a way we have not modelled.** Every tenant table today uses the identical
  `tenant_isolation` policy shape, so there is no second shape to test against.
  If a premium isolation tier (schema-per-tenant, per #14's scope) ever lands,
  this primitive must be re-examined against it — a directed write assumes the
  destination is reachable from the same connection.
- **Ordering across tenants is not guaranteed.** JetStream ordering is per
  subject, and a directed event and a subsequent withdrawal of it are separate
  messages. S4 must therefore treat withdrawal as a state transition guarded on
  current status, not as an ordered pair — which is what the status-guarded
  `UPDATE` rule in `docs/standards/backend.md` already requires.
- **This is a disclosure mechanism, not a consent mechanism.** It enforces that a
  cross-tenant write is declared, provenance-bearing and isolated. It does not
  and must not decide *whether* a disclosure is lawful. That judgement belongs to
  the feature that publishes the event.
