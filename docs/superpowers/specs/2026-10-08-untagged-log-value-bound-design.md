# Every log value is bounded, not only the PHI-tagged ones

**Issue:** [#943](https://github.com/tesserix/helivanta/issues/943)
**Completes:** `2026-10-07-phi-mask-render-bound-design.md` (#904), whose
"Not covered" section filed this.

## The problem, stated precisely

#904 bounds what the PHI masking path marshals: `renderBound` decides before
marshalling, and a tagged value over 1 MiB is replaced by a marker. A value
with **no** `helivantalog:"phi"` tag never reaches that path. It goes straight
to `slog`'s JSON handler, which marshals it with no bound. An untagged
shared-reference struct therefore still expands exponentially: the depth-20
`buildDAG` fixture rendered 88 MB, and depth 40 would be ~90 TB. A `KindString`
attribute or an error with a huge `Error()` text is equally unbounded, because
slog writes those itself without `encoding/json`.

## Decisions

### D1 — The bound applies to every attribute value slog would render

The tag handler already sees every attribute: it resolves `LogValuer`s and
walks groups and `WithAttrs`. It now also checks each value's size, mirroring
how `log/slog`'s JSON handler renders that kind (`appendJSONValue`):

| Kind | How slog renders it | How it is measured |
|---|---|---|
| `KindString` | JSON string | `stringCost` |
| `KindAny`, an `error` that is not a `json.Marshaler` | its `Error()` as a JSON string | `stringCost(err.Error())` |
| `KindAny`, anything else | `encoding/json` | `renderBound` (#904) |

Numbers, bools, durations and times have a fixed small size and are not
checked. A tagged value keeps #904's path unchanged.

### D2 — Within the bound, nothing changes; over it, a visible omission

A value within the bound is not touched. The record passes through exactly as
before, so the narrowness invariant (an untagged value renders byte-identically
to an unwrapped logger) still holds for every value that fits. The
`phitag.go` package doc records this as the invariant's only exception, by size
alone.

A value over the bound is replaced by `"[OMITTED:oversize:<Go type>]"` and
counted in `LogValueOversizeCount()`. It is deliberately not `[REDACTED:...]`:
this is not PHI masking, and an incident reader must not mistake a dropped
debug blob for a masked field. The rest of the record (`request_id`,
`tenant_id`, the message) is emitted intact.

A cycle or a failing marshaller is **not** treated as oversize. slog already
reports those, and this change only stops the size.

### D3 — The limit is the same 1 MiB

The same limit (`maxPHIMarshalBytes`) as #904. A second number would only be a
second thing to tune, with no evidence that an untagged value deserves a
different ceiling.

### D4 — The cost, measured

`BenchmarkLogUntaggedStruct` (a 50-element untagged struct with nested slice and
map, `-benchtime=2000x -count=3`): **~870 µs/op on `main`, ~1,000 µs/op with
this change (+~13%), allocations +1.6%**. The walk is bounded by the limit, not
the value, and a typical log value is far smaller than this fixture. Paying
this on every structured log call buys the guarantee that no single call can
emit an unbounded line.

## Not covered

- The log **message** (`r.Message`) is not bounded. It is a developer-written
  literal on every call site in this repo. Bounding it would need a separate
  argument, and nothing observed motivates it.
- A custom `json.Marshaler` that itself returns megabytes allocates them during
  the walk, before its size is known. This is the same residual #904 states.

## Tests

- An untagged depth-40 DAG is omitted with a bounded line, the type named, the
  rest of the record intact, no `[REDACTED:` marker, and the counter
  incremented. If the bound did not apply, the test would exhaust memory rather
  than pass.
- An oversized `KindString` and an oversized `error` (measured by `Error()`,
  not its fields) are omitted.
- A 900 KiB value, as a struct, a string and an error, renders byte-identically
  to an unwrapped logger.
- Groups, `WithAttrs` and a `LogValuer` resolving to an oversized value are all
  bounded.
- The existing byte-identity tests are unchanged and pass.
- Mutations (each fails the suite): the string kind unbounded; an error
  measured by its fields; a bound so tight it omits legal values.
