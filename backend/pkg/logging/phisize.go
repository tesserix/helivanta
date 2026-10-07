package logging

import (
	"encoding"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// Rendering bound: refusing to marshal a value whose JSON rendering would be
// too large, decided BEFORE json.Marshal builds a byte of it (#904).
//
// # Why a pre-flight walk, and why it is not the withdrawn reimplementation
//
// JSON has no reference sharing, so a value whose pointers share targets
// renders as a tree, exponentially in sharing depth: buildDAG(20) in the tests
// is 88 MB of JSON from a few kilobytes of Go, buildDAG(30) would be ~90 GB.
// encoding/json renders the whole value into one buffer before returning, so
// a size check on its output (maxPHIMarshalBytes, still applied as a backstop)
// fires only after that buffer has been built and held — the memory, latency
// and pipeline harm #904 describes has already happened by then.
//
// The walk below therefore answers one question ahead of the marshal: is the
// rendering provably no larger than the limit? It computes an UPPER BOUND on
// encoding/json's output, never the output itself. That is deliberately not
// the reconstruction #778 withdrew: nothing produced here is ever emitted, so
// a rule this walk models imprecisely cannot leak anything. Its two possible
// errors have known costs:
//
//   - Over-counting (the direction it is built to err in) masks a value that
//     would have fit — a lost log value, stated loudly by the oversize marker,
//     never PHI in the clear.
//   - Under-counting lets json.Marshal run on a value that is too large, which
//     is exactly today's behaviour, and the post-marshal backstop still masks
//     the result. The one source of under-counting is opaque types (below).
//
// # The walk is bounded by the limit, not by the value
//
// Every node visited adds at least one byte to the running bound and the walk
// stops the moment the bound exceeds the limit, so it visits at most
// limit+1 nodes whatever the value's shape — a 2^40-path DAG costs the same
// as a 2^20 one. That is the property TestRenderBoundStopsAtTheLimit asserts,
// by counting nodes rather than timing anything (#897's lesson: a wall-clock
// bound measures the runner, not the code).
//
// # Self-rendering types
//
// A type implementing json.Marshaler or encoding.TextMarshaler renders however
// its own code decides, so the walk calls that marshaller and counts what it
// returned (escaped, for TextMarshaler). That keeps the bound sound for
// ordinary leaves like time.Time, at the cost of running those marshallers
// twice. It also means such a type's own output is built once, in the walk,
// before its size is known: a custom marshaller that itself emits megabytes
// allocates them here. That is the one place this layer cannot get ahead of
// the allocation, and it is the same boundary phitag.go already states — a
// type that renders itself is beyond this layer's reach by construction. The
// post-marshal check in maskPHIValue stays as the backstop.

// errRenderOversize is returned when the bound exceeds the limit.
var errRenderOversize = errors.New("logging: phi value rendering exceeds the size bound")

// errRenderCycle is returned when the walk meets a pointer or map already on
// its own path. json.Marshal errors on a cycle too; reporting it separately
// keeps a cycle masked with the ordinary marker, as it always was, instead of
// being described as oversize.
var errRenderCycle = errors.New("logging: phi value contains a cycle")

// Per-node byte costs. Each is an upper bound on what encoding/json emits for
// that kind, and each is at least 1, which is what makes the walk's node count
// bounded by the limit.
const (
	nullCost  = len("null")
	boolCost  = len("false")
	intCost   = len("-9223372036854775808")
	uintCost  = len("18446744073709551615")
	floatCost = 32 // strconv 'g'/'e' at 64-bit precision, sign and exponent included
	// opaqueCost is charged for a value the walk cannot inspect: a kind
	// json.Marshal refuses (the value is then masked whole by its error), or
	// a self-rendering value reached through an unexported field, which
	// reflect will not hand out and encoding/json does not render either.
	opaqueCost = 2
	// fieldOverhead covers a key's quotes, the colon, the separating comma,
	// and the two quotes a `,string` option adds around the value.
	fieldOverhead = 6
)

// oversizeRedactions counts values masked because their rendering exceeded
// the bound — a subset of RedactionCount, kept separately because "a value
// was too large to log" is an operational signal (what is logging it, and
// why?) that the ordinary PHI count would bury.
var oversizeRedactions atomic.Uint64

// PHIOversizeCount reports how many tagged values this process has masked
// because their rendering exceeded the size bound. Same in-process seam as
// RedactionCount, for the same reason (#679 is unbuilt).
func PHIOversizeCount() uint64 { return oversizeRedactions.Load() }

// oversizeMarker is the replacement for a value masked because it was too
// large. Unlike the ordinary phi marker it names the reason and the Go type,
// so an incident timeline shows that a value was dropped and which one —
// never only an absence. The type name is not PHI; it is a property of the
// code, not the patient.
func oversizeMarker(t reflect.Type) redactedJSON {
	b, err := json.Marshal("[REDACTED:phi-oversize:" + t.String() + "]")
	if err != nil {
		// A string always marshals; this branch exists so the function has no
		// path that returns nothing.
		return redactedJSON(phiMarkerJSON)
	}
	return redactedJSON(b)
}

// renderBound walks v and returns an upper bound on the length of its JSON
// rendering, or errRenderOversize as soon as that bound exceeds limit, or
// errRenderCycle. visited is the number of nodes the walk touched, returned so
// the bound on the walk's own cost can be asserted rather than timed.
func renderBound(v any, limit int) (size int, visited int, err error) {
	w := boundWalker{limit: limit, onPath: map[uintptr]bool{}}
	err = w.walk(reflect.ValueOf(v))
	return w.size, w.visited, err
}

type boundWalker struct {
	limit   int
	size    int
	visited int
	// onPath holds the pointers and maps on the current path from the root —
	// not every one ever seen. A shared, non-cyclic reference is legitimately
	// rendered once per path that reaches it, and must be counted that way;
	// that expansion is the whole hazard.
	onPath map[uintptr]bool
}

func (w *boundWalker) add(n int) error {
	w.size += n
	if w.size > w.limit {
		return errRenderOversize
	}
	return nil
}

func (w *boundWalker) walk(v reflect.Value) error {
	w.visited++
	if !v.IsValid() {
		return w.add(nullCost)
	}
	t := v.Type()
	// A nil pointer or interface renders as null even when its type has a
	// marshaller; encoding/json checks this before calling one, and so does
	// this walk.
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return w.add(nullCost)
	}
	// Mirrors encoding/json's own choice: a value-receiver marshaller always
	// applies, a pointer-receiver one only when the value is addressable.
	if rendersItself(t) {
		return w.walkSelfRendering(v)
	}
	if v.CanAddr() && rendersItself(reflect.PointerTo(t)) {
		return w.walkSelfRendering(v.Addr())
	}

	switch v.Kind() {
	case reflect.Bool:
		return w.add(boolCost)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return w.add(intCost)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return w.add(uintCost)
	case reflect.Float32, reflect.Float64:
		return w.add(floatCost)
	case reflect.String:
		return w.add(stringCost(v.String()))
	case reflect.Interface:
		if v.IsNil() {
			return w.add(nullCost)
		}
		return w.walk(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return w.add(nullCost)
		}
		return w.walkRef(v.Pointer(), func() error { return w.walk(v.Elem()) })
	case reflect.Map:
		if v.IsNil() {
			return w.add(nullCost)
		}
		return w.walkRef(v.Pointer(), func() error { return w.walkMap(v) })
	case reflect.Slice:
		if v.IsNil() {
			return w.add(nullCost)
		}
		return w.walkSeq(v)
	case reflect.Array:
		return w.walkSeq(v)
	case reflect.Struct:
		return w.walkStruct(v)
	default:
		// Chan, Func, Complex, UnsafePointer: json.Marshal refuses these and
		// the value is masked whole by the existing error path. Count them so
		// the walk still terminates having added at least a byte.
		return w.add(opaqueCost)
	}
}

// walkSelfRendering counts what v's own marshaller returns. json.Marshaler
// takes precedence over encoding.TextMarshaler, as in encoding/json. A
// marshaller error is returned as-is: json.Marshal would fail on the same
// value, and maskPHIValue masks it whole.
func (w *boundWalker) walkSelfRendering(v reflect.Value) error {
	if !v.CanInterface() {
		return w.add(opaqueCost)
	}
	switch m := v.Interface().(type) {
	case json.Marshaler:
		b, err := m.MarshalJSON()
		if err != nil {
			return err
		}
		// encoding/json compacts this output, which only ever removes bytes.
		return w.add(max(len(b), 1))
	case encoding.TextMarshaler:
		b, err := m.MarshalText()
		if err != nil {
			return err
		}
		return w.add(stringCost(string(b)))
	default:
		return w.add(opaqueCost)
	}
}

func (w *boundWalker) walkRef(p uintptr, inner func() error) error {
	if w.onPath[p] {
		return errRenderCycle
	}
	w.onPath[p] = true
	defer delete(w.onPath, p)
	return inner()
}

func (w *boundWalker) walkSeq(v reflect.Value) error {
	// []byte renders as a base64 string, unless its element type renders
	// itself, in which case encoding/json emits an array of elements.
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 &&
		!rendersItself(v.Type().Elem()) && !rendersItself(reflect.PointerTo(v.Type().Elem())) {
		return w.add(2 + 4*((v.Len()+2)/3))
	}
	if err := w.add(2); err != nil { // [ ]
		return err
	}
	for i := range v.Len() {
		if err := w.add(1); err != nil { // separator
			return err
		}
		if err := w.walk(v.Index(i)); err != nil {
			return err
		}
	}
	return nil
}

func (w *boundWalker) walkMap(v reflect.Value) error {
	if err := w.add(2); err != nil { // { }
		return err
	}
	iter := v.MapRange()
	for iter.Next() {
		k := iter.Key()
		keyCost := uintCost + 2 // integer keys render quoted
		switch {
		case k.Kind() == reflect.String:
			keyCost = stringCost(k.String())
		case k.Type().Implements(textMarshalerType) && k.CanInterface():
			// encoding/json uses MarshalText for a non-string key.
			if k.Kind() == reflect.Pointer && k.IsNil() {
				keyCost = 2
				break
			}
			text, err := k.Interface().(encoding.TextMarshaler).MarshalText()
			if err != nil {
				return err
			}
			keyCost = stringCost(string(text))
		}
		if err := w.add(keyCost + fieldOverhead); err != nil {
			return err
		}
		if err := w.walk(iter.Value()); err != nil {
			return err
		}
	}
	return nil
}

func (w *boundWalker) walkStruct(v reflect.Value) error {
	if err := w.add(2); err != nil { // { }
		return err
	}
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		// encoding/json renders exported fields and promotes embedded ones;
		// anything else never appears. An unexported, non-embedded field is
		// therefore skipped — counting it would only over-count, but an
		// unexported cache could then mask an ordinary value for no reason.
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			// Exactly "-" means "never rendered"; "-," names a field "-" and
			// falls through to be counted like any other.
			continue
		}
		// The key is either the Go name or the tag's name; counting both is an
		// upper bound that needs no knowledge of which one encoding/json picks.
		name, _, _ := strings.Cut(tag, ",")
		if err := w.add(stringCost(f.Name) + stringCost(name) + fieldOverhead); err != nil {
			return err
		}
		if err := w.walk(v.Field(i)); err != nil {
			return err
		}
	}
	return nil
}

// stringCost is an upper bound on the bytes encoding/json emits for s,
// quotes included, with HTML escaping off (the way slog and
// marshalNoHTMLEscape both render).
//
// It is exact for valid, printable UTF-8 — which is nearly every real string —
// rather than a flat multiplier, because a multiplier would over-count Indic
// script (three bytes per rune, emitted as three bytes) badly enough to mask
// ordinary values. Every escaped form is counted at its widest, six bytes
// (`\u00XX`, `�`, ` `), which is never less than what is emitted.
func stringCost(s string) int {
	n := 2
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1, // invalid byte -> �
			r < 0x20,            // control -> \n, \t or \u00XX
			r == '"', r == '\\', // -> \" \\
			r == ' ', r == ' ': // ->
			n += 6
		default:
			n += size
		}
		i += size
	}
	return n
}
