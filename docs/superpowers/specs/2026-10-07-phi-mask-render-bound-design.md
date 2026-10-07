# PHI masking must refuse an oversized value before marshalling it

**Issue:** [#904](https://github.com/tesserix/helivanta/issues/904)
**Builds on:** the marshal-then-mask design of #778 (`pkg/logging/phitag.go`),
unchanged.
**Related, separately tracked:** #897 (the wall-clock test this replaces),
#840 (_what_ the redaction path masks, not how much it emits).

## The problem, stated precisely

`maskPHIValue` marshals a tagged value and then masks the tagged paths. It
already had a size guard (`maxPHIMarshalBytes`, 1 MiB), but checked it on
`json.Marshal`'s **output**. `encoding/json` builds the whole rendering in one
buffer before returning, so the check fired only after the allocation, the
latency and the CPU cost had already been paid.

JSON has no reference sharing, so a value whose pointers share targets renders
as a tree, exponentially in sharing depth. #904 measured `buildDAG(20)` at
88 MB and 160 ms from a few kilobytes of Go; depth 30 would be about 90 GB.
The old guard's own comment said `json.Marshal` handled shared references
"linearly", which is the false premise this change corrects.

## Decisions

### D1 — A pre-flight upper bound, decided before any marshalling

`renderBound` (`pkg/logging/phisize.go`) walks the value by reflection and
computes an **upper bound** on the length of `encoding/json`'s output. It
stops the moment the bound exceeds the limit. `maskPHIValue` calls it first. A
value over the limit is never marshalled.

This is not the reconstruction #778 withdrew. Nothing the walk produces is
emitted, so a rule it models imprecisely cannot leak anything. Its error
directions have stated costs:

- **Over-count** (the direction it is built to err in): a value that would
  have fit is replaced by the oversize marker. The log loses a value, and the
  marker says so. No PHI is exposed.
- **Under-count**: `json.Marshal` runs on a value that is too large, which is
  the behaviour before this change. The post-marshal check stays as a
  backstop and still masks it.

Strings are counted exactly for valid printable UTF-8, and at the widest
escape (6 bytes) for everything `encoding/json` escapes. A flat multiplier
would over-count Indic script, at three bytes per rune, badly enough to mask
ordinary clinical notes. Numbers are counted at their widest. A field's key is
counted as the Go name plus the tag name, which needs no knowledge of which
one `encoding/json` picks. Fields tagged exactly `json:"-"` and unexported
non-embedded fields are skipped, because they are never rendered. Counting
them would only over-count, but an unexported cache could then mask an
ordinary value for no reason.

### D2 — The walk's cost is bounded by the limit, not by the value

Every node adds at least one byte to the bound, so the walk visits at most
`limit + 1` nodes whatever the value's shape. A 2^40-path DAG costs the same
as a 2^20 one. Measured with `BenchmarkMaskSharedReferenceDAG` (depth 20):
792 ms and 142 MB per call on `main`, about 17 ms and 1.7 KB with this change.

### D3 — Self-rendering types are asked, not guessed

A type implementing `json.Marshaler` or `encoding.TextMarshaler`, such as
`time.Time`, renders however its own code decides. The walk calls that
marshaller and counts its output, following `encoding/json`'s
value-receiver/addressable-pointer-receiver rule. That keeps the bound sound
for ordinary leaves, at the cost of running those marshallers twice.

The residual, stated rather than discovered later: a custom marshaller that
itself emits megabytes allocates them during the walk, before its size is
known. That is the boundary `phitag.go` already documents for this layer: a
type that renders itself is beyond its reach by construction.

### D4 — The drop is visible: a distinct marker, and a counter

#904 rules out silently emitting nothing. An oversized value becomes
`"[REDACTED:phi-oversize:<Go type>]"`, which names the reason and the type.
The type name is a property of the code, not of the patient. It is counted in
`RedactionCount` and also in a new `PHIOversizeCount()`, because "something
is logging a value too large to log" is an operational signal that the
ordinary PHI count would bury.

A cycle is detected separately (a pointer or map already on the current path)
and keeps the ordinary `[REDACTED:phi]` marker it always had. `json.Marshal`
errors on a cycle, and "oversize" would misdescribe it.

### D5 — Assertions on the bound, not on time

`TestSharedReferenceDAGCompletesInBoundedTime` asserted elapsed time and was
cut from depth 20 to 17 to stop failing on CI (#897). It is replaced by:

- `TestSharedReferenceDAGIsRefusedBeforeMarshalling`: a depth-**40** DAG,
  asserting the emitted bytes. If the pre-flight regressed, this would
  exhaust memory rather than pass.
- `TestRenderBoundStopsAtTheLimit`: the walk visits at most `limit + 1`
  nodes.
- `TestOversizeIsRefusedWithoutMarshalling`: swaps the marshal for a hook
  that records the call and does not forward it, and asserts it was never
  reached. Only this test can tell a pre-flight refusal from a post-marshal
  one.
- `TestRenderBoundIsAnUpperBound`: over a corpus of hostile strings, extreme
  numbers, `,string`, `-`/`-,`, embedded promotion, int and `TextMarshaler`
  map keys, `[]byte`, `json.RawMessage`, `time.Time`, and both
  marshaller-receiver paths, the bound is at least the real rendering.
- `TestRenderBoundIsNotWildlyPessimistic`: ASCII and Devanagari notes are
  bounded at their real width plus key overhead.

Each was shown able to fail by mutation: escapes counted as 1 byte,
self-rendering values guessed (both receiver paths), the limit ignored, and
the pre-flight disabled.

## Not covered by this slice

- **Untagged values.** A value with no `helivantalog:"phi"` tag anywhere in
  its type never reaches this layer. The narrowness invariant requires it to
  render byte-identically to an unwrapped logger, so `slog`'s JSON handler
  marshals it with no bound. #904 scopes itself to the masking path, and
  bounding every attribute is a logger-wide change to that invariant. Filed
  as [#943](https://github.com/tesserix/helivanta/issues/943).
- Reference-sharing detection that would change the emitted JSON shape
  (#904's own out-of-scope).
