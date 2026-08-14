# Event contract registry — design

**Issue:** [#827](https://github.com/tesserix/hms/issues/827) — Event contracts
exist as three unlinked copies.
**Status:** approved 2026-08-14.
**Related:** #667 (the standard this enforces), #710, #685, #774 Tier 2,
ADR-0005.

---

## Problem

An event's subject and payload shape exist as independent copies in every module
that touches them, with nothing linking publisher to consumer.

`hms.in.medicore.visit_created.v1` is declared three times — `medicore/module.go:13`
(publisher), `pharmacy/module.go:15` and `lab/module.go:13` (each consumer's own
copy). Its payload is declared three times too: `VisitCreatedData` in
`medicore/visits.go:38`, and a private `visitCreatedData` in each of
`pharmacy/consumers.go:13` and `lab/consumers.go:13`.

This is not carelessness. Modules must not import one another — enforced by
`depguard` and `TestModulesDoNotImportEachOther` — and copying the contract is
currently the *only* way a consumer can read a publisher's event.

### The failure is silent and corrupts clinical records

Reproduced against `encoding/json` before this design was written, per
`docs/standards/engineering-principles.md` §5:

```
publisher renames patient_name -> patient
wire:            {"visit_id":"v-1","patient":"Asha Rao"}
unmarshal error: <nil>
consumer sees:   visit_id="v-1" patient_name=""
```

`encoding/json` ignores unknown fields and leaves missing ones at their zero
value, so **nothing errors**. Both consumers keep compiling and running, and
write `lab_orders.patient_name = ''` and a pharmacy dispense against an empty
patient name — a clinical record attached to nobody, produced by a system
reporting success.

A second, quieter case: bumping a subject `.v1` → `.v2` leaves consumers
subscribed to a subject nobody publishes. No wrong data, just pharmacy and lab
intake silently ceasing.

---

## Decisions

### D1 — The contract lives in a `contract` subpackage of the publishing module

```
internal/modules/medicore/
  contract/events.go     SubjectVisitCreated, VisitCreatedData   ← importable
  module.go, visits.go                                            ← still forbidden
```

Consumers import `.../modules/medicore/contract` directly. Ownership stays where
the change happens: medicore's event shape lives in medicore, so the person
renaming a field is already in the directory that defines the contract.

**Why here and not a central package.** A `contract` package is not merely a
convenient location — it is the **published interface** of a module. Per
ADR-0005, HMS stays a modular monolith and extracts services only on a named
trigger; when that happens, `medicore/contract` lifts out as a shared library
unchanged, because it already holds exactly what a consumer needs and nothing it
must not have. A central `internal/contracts` would have to be untangled first —
separating five modules' contracts from one file at the moment of least
appetite for surprises. The location is what keeps ADR-0005's position honest
rather than aspirational.

*Rejected: one central contracts package outside `internal/modules`.* Needs no
isolation exception at all, which is genuinely simpler. But it inverts ownership
and becomes a file every module edits — merge contention that grows with the
module count.

*Rejected: one package per event outside `internal/modules`.* Removes the
contention but proliferates tiny packages indefinitely, and still detaches
ownership: nothing local tells you medicore owns `visit_created`.

### D2 — The isolation exception is narrow and mechanically policed

Two rules each gain exactly one exception — an imported path ending in
`/contract`:

```
depguard  deny: github.com/tesserix/hms/internal/modules  (from within internal/modules)
          →  allow when the imported path ends in "/contract"

TestModulesDoNotImportEachOther
          →  skip when the cross-module import path ends in "/contract"
```

Everything else — handlers, repositories, models, tables — stays exactly as
forbidden as today.

A new arch test then polices what may live behind the exception. Every
`internal/modules/*/contract` package:

- declares **only** `const`, `type`, `var` — any `func` or method declaration fails
- imports **only** `{time, github.com/google/uuid}` — anything else fails,
  including its own module

Both halves are load-bearing. A method on a payload type is behaviour crossing
the module boundary, which is what the isolation rule exists to stop. A contract
package that could import its own module would re-open the boundary
transitively, because the exception makes contract packages importable by
everyone.

**Accepted cost:** a payload type cannot carry even a `Validate()` helper. A
contract describes a shape; it does not do anything. Genuinely shared validation
belongs in `pkg/`, which both sides may already import.

*Rejected: import allowlist only, functions permitted.* More useful, and shared
validation is arguably a feature. But "data only" is a bright line and "no
behaviour that matters" is not; the first helper invites the second.

*Rejected: allow the import path, rely on review.* §4 ranks documented
convention last, and this is the one place the isolation rule can be widened —
precisely where nothing mechanical noticing would be worst.

### D3 — Modules declare what they publish

`platform.Module` gains `Publishes() []string`, alongside the existing
`Permissions()`, `Migrations()`, `Consumers()` and `Broadcasts()` — declarative,
in the same style.

It also makes the event catalogue derivable from code rather than a document
that drifts: the catalogue is `⋃ Publishes()`, always current. That serves #667
directly.

*Rejected: static analysis of `bus.Publish` call sites.* Reflects what the code
does rather than what it claims, but must resolve a *constant value* through
arbitrary indirection, is brittle against refactors, and fails opaquely when it
cannot resolve one.

*Rejected: rely on the shared constant alone.* Rules out typos and stale
strings — the main sources of this bug — but leaves the "publisher stopped
publishing" case uncovered.

---

## Architecture

### Migration

| Event | Contract package | Duplicates deleted |
|---|---|---|
| `visit_created` | `medicore/contract` | 2 subject consts, 2 payload structs |
| `dispense_recorded` | `pharmacy/contract` | — |
| `member_granted`, `member_revoked` | `iam/contract` | — |
| `credential_revoked` | `iam/contract` | — |
| `pinged` | `reference/contract` | — |

Only `visit_created` is currently duplicated, because it is the only
cross-module event. The other four are published and consumed within one module
— which is why nobody has been bitten yet, and why this is cheap now rather than
after the sixth cross-module event.

**Every event gets a contract package, including the intra-module ones.** An
event that is intra-module today becomes cross-module the moment a second module
wants it, and at that point the choice is either to move the contract (touching
the publisher, the new consumer and any test that referenced the old location)
or to copy it — which is how the current state arose. Doing it uniformly now
costs four small file moves. It is also forced by check 3 below: a `Publishes()`
entry must be a constant from the module's own contract package, so a module
that publishes anything needs one regardless.

**Import naming.** A file may import two `contract` packages at once — for
example `pharmacy/consumers.go` consumes `medicore`'s event while `pharmacy`
publishes its own. Both packages are named `contract`, so imports are aliased
`<module>contract` (`medicorecontract`, `pharmacycontract`). The alias is
mandatory rather than stylistic: unaliased, the second import will not compile,
and a reader of `contract.VisitCreatedData` cannot tell whose contract it is.

### The compile-error property

With `lab/consumers.go` importing `medicore/contract`, renaming `PatientName` →
`Patient` in `VisitCreatedData` means `d.PatientName` in lab no longer resolves.
`go build` fails, naming file and line.

The failure moves from *a blank patient name in a lab order six weeks later* to
*the build, before the commit lands*.

It covers the quieter case too: bumping the subject to `.v2` changes the constant
both sides share, so consumers follow automatically — no silent unsubscribe. If
the payload changed as well, they fail to compile, which is the correct forcing
function.

### CI checks

1. Every `Consumer.Subject` and `Broadcast.Subject` is in `⋃ Publishes()`.
2. No two modules publish the same subject.
3. Every `Publishes()` entry is a constant from that module's own contract
   package, not a bare string.

---

## Error handling

This design adds no runtime error paths. Every guard is a compile error or a CI
failure; nothing new can fail at request time.

The existing runtime behaviour is unchanged: a consumer handler returning an
error still nacks and eventually dead-letters, and `json.Unmarshal` failing in a
handler is still that handler's error to return. What changes is that the
*shape* it unmarshals into can no longer silently disagree with the publisher's.

---

## Testing

Per `docs/standards/engineering-principles.md` §5, every assertion is proven able
to fail before it is trusted.

| ID | Test | Why it exists |
|---|---|---|
| T1 | Rename a contract field → `go build` fails, naming lab and pharmacy | The headline property. Proven by mutation in the plan, since a compile failure cannot be a passing Go test. |
| T2 | Every consumer unmarshals into a type from a contract package | The standing guard against a local copy being re-introduced later. |
| T3 | Every `Consumer`/`Broadcast` subject ∈ `⋃ Publishes()` | The silent "subscribed to nothing" case. |
| T4 | No two modules publish the same subject | Ambiguous ownership. |
| T5 | Every `Publishes()` entry is a constant from the module's own contract package | Stops the declaration drifting from a bare string. |
| T6 | Contract packages declare only const/type/var | The exception cannot widen into behaviour. |
| T7 | Contract packages import only `{time, uuid}` | A contract importing its own module re-opens isolation transitively. |
| T8 | `TestModulesDoNotImportEachOther` still fails for non-contract paths | Proves the exception is narrow — mutate an import to `medicore/module.go` and it must still fail. |
| T9 | Generator emits a contract package and `Publishes()` | Otherwise module six reintroduces the pattern. |

### On T2, which contradicts a rejection above

D3 rejected AST-walking `bus.Publish` call sites as brittle, so proposing an AST
check here needs justifying rather than waving through.

The difference is what is resolved. The rejected check had to resolve a
*constant value* through arbitrary indirection and fails opaquely when it cannot.
T2 resolves the *type* of the second argument to `json.Unmarshal(evt.Data, &d)`
using `go/packages` type information — exactly what the type checker already
computed — and fails loudly: a type it cannot resolve is a test failure, not a
silent skip.

If T2 proves fragile in practice, the fallback is the documented rule plus
review. That is a worse outcome and should be recorded as such rather than
discovered quietly.

---

## Limitations

- **A declared publication is not proven to happen.** `Publishes()` is a
  declaration; a module could list a subject it never publishes and CI would be
  satisfied. Closing this properly needs the call-site walk rejected in D3. The
  mechanism eliminates drift between publisher and consumer, and typo'd or stale
  subject strings — not this.
- **No versioning or upcasting.** `.v1` and `.v2` coexisting during a rollout,
  and replaying historical events under a new schema, are #667's schema-evolution
  territory.
- **PHI in event payloads is untouched.** Identifiers still travel in payloads;
  that is a separate #774 Tier 2 item.
- **Payload types carry no behaviour**, by construction (D2). Shared validation
  must live in `pkg/`.
- **T2 is the one AST-based check accepted.** If it proves fragile, enforcement
  of the payload-sharing property degrades to convention plus review.

---

## Out of scope

- Event naming, envelope and schema-evolution conventions, and the catalogue
  document — #667 owns the standard; this owns the mechanism.
- Contract testing in CI for OpenAPI — #710.
- A runtime schema registry, Avro/Protobuf, or broker-side validation. The
  platform publishes JSON through an outbox to NATS JetStream and this does not
  change that.
- Removing PHI from payloads — #774 Tier 2.
