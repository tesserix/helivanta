# Event contract registry — design

**Issue:** [#827](https://github.com/tesserix/helivanta/issues/827) — Event contracts
exist as three unlinked copies.
**Status:** implemented 2026-08-14. Corrections made during implementation are
marked *(corrected)* and collected under "What implementation changed" below.
**Related:** #667 (the standard this enforces), #710, #685, #774 Tier 2,
ADR-0005.

---

## Problem

An event's subject and payload shape exist as independent copies in every module
that touches them, with nothing linking publisher to consumer.

`helivanta.in.medicore.visit_created.v1` is declared three times — `medicore/module.go:13`
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
ADR-0005, Helivanta stays a modular monolith and extracts services only on a named
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
depguard  deny: github.com/tesserix/helivanta/internal/modules  (from within internal/modules)
          →  allow when the imported path ends in "/contract"

TestModulesDoNotImportEachOther
          →  skip when the cross-module import path ends in "/contract"
```

Everything else — handlers, repositories, models, tables — stays exactly as
forbidden as today.

**The depguard rule needs `list-mode: lax` *(corrected)*.** depguard's default
mode is `original`, under which a non-empty `allow` stops being an exception and
becomes a near-total allowlist: every import must *also* match an `allow` entry.
Adding the one contract entry under `original` fails roughly fifty unrelated
imports across `internal/modules` — `context`, `errors`, `gin`, `gorm`,
`pkg/authn` and the rest — because none of them match the single-entry allow
list either. Confirmed by running `make lint-go` against `original` during
implementation. Under `lax` the rule stays a denylist and the allow entry only
carves out the exception because it is *more specific* (longer) than the deny
entry it overrides. The comment in `backend/.golangci.yml` records this, because
without it `list-mode: lax` reads as redundant and invites deletion.

Also *(corrected)*: depguard's `allow` is a prefix list naming each contract
package explicitly, so a new module needs an entry. Nothing fails at generation
time; `new-module.sh` prints a reminder as its step 3.

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

**There are seven subjects, not five *(corrected)*.** The count in the approved
spec omitted `lab`'s `result_ready`, and treated `iam`'s three as fewer. Reading
the code on 2026-08-14 gives:

| Event | Publisher | Consumers | Contract package | Duplicates deleted |
|---|---|---|---|---|
| `visit_created` | medicore | pharmacy, lab | `medicore/contract` | 2 subject consts, 2 payload structs |
| `dispense_recorded` | pharmacy | *none* | `pharmacy/contract` | — |
| `result_ready` | lab | *none* | `lab/contract` | — |
| `member_granted` | iam | iam (`iam-fga-sync`) | `iam/contract` | — |
| `member_revoked` | iam | iam (`iam-fga-sync-revoke`) | `iam/contract` | — |
| `credential_revoked` | iam | iam (broadcast) | `iam/contract` | — |
| `pinged` | reference | reference | `reference/contract` | — |

Only `visit_created` is duplicated, because it is the only cross-module event —
that part of the approved spec was right. The other six are published and
consumed within one module (or not consumed at all), which is why nobody has
been bitten yet, and why this is cheap now rather than after the second
cross-module event.

**Two subjects have no consumer at all** — `dispense_recorded` and
`result_ready`. That is legal and deliberately checked by nothing: publishing is
how a module becomes observable to something added later without changing the
publisher. Check 1 below asserts consumers ⊆ published, never the converse.

Four payload types were unexported and became exported on moving into a contract
package: `dispenseRecordedData` → `DispenseRecordedData`, `resultReadyData` →
`ResultReadyData`, `pingedData` → `PingedData`.

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

**The publisher's own file fails too *(corrected)*.** This section originally
named only the consumers. Performing the mutation showed
`medicore/visits.go` — the code that *builds* the payload — breaking in the same
compile, because it now references the shared type rather than a local
declaration it could rename in lockstep. That is a stronger property than the
one designed for: not only can a consumer not drift from the contract, the
publisher cannot drift from its own contract either. Before this change,
renaming the field in `medicore/visits.go` compiled cleanly everywhere and was
precisely the silent-corruption path in the reproduction above.

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

**T2 did not prove fragile — enforcement did not degrade to convention
*(corrected)*.** `TestConsumersUnmarshalIntoContractTypes` resolves the type at
all six current `json.Unmarshal(evt.Data, …)` call sites and was observed
failing against a reintroduced local payload struct in `lab`. Its real
limitation is different from the one anticipated, and is a *selection*
limitation rather than a resolution one: the call sites it inspects are matched
by name — any `json.Unmarshal(x.Data, &y)` where the first argument's selector
is `Data` — not semantically by "this is an event payload". All six matches are
genuine today, but a future struct with an unrelated `.Data` field would be
misclassified and would have to unmarshal into a contract type or fail the
build for the wrong reason. Test files are deliberately not inspected: a test
may legitimately unmarshal an event into an ad-hoc struct precisely to pin the
wire shape, which is the opposite of the mistake this guards.

## What implementation changed

Every item below was found while implementing, not while reviewing, and each is
marked *(corrected)* at the point it applies above.

1. **Seven subjects, not five.** `lab`'s `result_ready` was missing from the
   count and the migration table; `iam`'s three were undercounted. Two subjects
   (`dispense_recorded`, `result_ready`) have no consumer, which is legal and
   checked by nothing.
2. **`list-mode: lax` is required on the depguard rule**, and the reason is not
   obvious enough to survive without a comment.
3. **The rename breaks the publisher's own file**, not only the consumers.
4. **T6 had to reject func literals bound to vars.** The check as designed
   inspected `*ast.FuncDecl` only, so `var Describe = func(…) {…}` in a contract
   package passed it — behaviour crossing the module boundary through the one
   door the isolation rule leaves open, which is exactly what T6 exists to stop.
   Observed passing before the fix and failing after.
5. **`TestPublishedSubjectConstants` is now derived from `⋃ Publishes()`.** It
   previously screened a hand-maintained map of four subjects; `iam`'s three had
   never been checked against the subject naming regex at all. Deriving it means
   a module added later is covered without anyone remembering to extend a map —
   a §5 "proxy for the claim" removed rather than patched.
6. **T5 (`TestPublishesUsesContractConstants`) compares values, not
   references.** A bare string literal equal to a contract constant passes it.
   Verified deliberately during implementation by replacing medicore's
   `Publishes()` entry with the identical bare string (test passed) and then
   with a `.v9` variant (test failed). It catches drift, not indirection — the
   distinction is recorded here so it is not later believed to prove more.
7. **The coverage gate had to exempt `internal/modules/*/contract`.** A
   data-only package has no functions and therefore no statements, so
   `go test -cover` reports "no test files" for it forever. The exemption is
   safe only because T6 mechanically guarantees no behaviour can live behind
   that path; the comment in `backend/scripts/coverage-gate.sh` ties the two
   together so deleting T6 forces revisiting the exemption.

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
- **T2 is the one AST-based check accepted.** It did not prove fragile, so
  enforcement did not degrade to convention plus review. It does select its call
  sites by name (`json.Unmarshal(x.Data, …)`) rather than semantically, and
  ignores test files deliberately — see "On T2" above.
- **T5 compares values, not references.** A bare string equal to a contract
  constant satisfies it.
- **The coverage gate exempts `internal/modules/*/contract`**, because a
  data-only package has no statements to cover. The exemption depends on T6
  continuing to exist.

---

## Out of scope

- Event naming, envelope and schema-evolution conventions, and the catalogue
  document — #667 owns the standard; this owns the mechanism.
- Contract testing in CI for OpenAPI — #710.
- A runtime schema registry, Avro/Protobuf, or broker-side validation. The
  platform publishes JSON through an outbox to NATS JetStream and this does not
  change that.
- Removing PHI from payloads — #774 Tier 2.
