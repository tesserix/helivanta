package logging

// Tag redaction: masking struct fields tagged `helivantalog:"phi"` before slog
// serialises them.
//
// This is the half of PHI redaction that patterns cannot do. A name, a date of
// birth or an address has no shape to match, so the only thing that can
// identify it is the developer who declared the field. The byte layer in
// redact.go screens the serialised line for Aadhaar/ABHA/mobile shapes; this
// layer screens the Go value for declared PHI. They compose: tags mask while
// the value is still a Go value, patterns mask the bytes afterwards.
//
// # Marshal first, then mask
//
// The obvious implementation — walk the value with reflection and rebuild it
// with the tagged fields replaced — was written, reviewed twice and withdrawn
// (see issue #778). Every defect it produced came from the same root cause:
// rebuilding a value's JSON rendering means reimplementing encoding/json, and
// every rule the rebuild failed to reproduce became a leak. `json:"-"` fields
// were *published* by the redactor. A `[]patient` inside an untagged wrapper —
// the ordinary list-response DTO — leaked every name because collection
// element types were never traversed. A depth cap failed open and emitted
// plaintext past the limit.
//
// So this implementation never reconstructs anything:
//
//  1. json.Marshal produces the type's *true* rendering. json tags, omitempty,
//     `-`, embedded promotion, custom marshallers and cycle detection are all
//     encoding/json's problem, already solved, and cannot drift from what is
//     actually emitted because it *is* what is actually emitted.
//  2. Per type, cached, we compute the set of JSON *paths* that carry the phi
//     tag. This is the only thing reimplemented here, and it reimplements
//     encoding/json's field *naming* rules rather than its rendering — a far
//     smaller surface — but not a free one. Getting a name wrong in the
//     under-masking direction emits PHI in plaintext, which is how both
//     name-resolution defects on this feature happened. So the two parts of
//     naming that encoding/json decides by unexported rules are not
//     reimplemented at all: which tag names it honours is answered by *asking
//     it* (jsonEmittedName), and its depth/tag conflict rule is mirrored
//     explicitly (resolveFieldName) with a fail-closed branch where the mirror
//     cannot be certain. The differential fuzz in phitag_conflict_test.go is
//     the mechanical guard on the class.
//  3. The marshalled bytes are streamed through encoding/json's token reader
//     and re-encoded, with the values at those paths replaced by the marker.
//
// The masked bytes go back to slog as a value that marshals to itself, so
// slog's JSON handler emits them verbatim.
//
// # The narrowness invariant
//
// A value whose type carries no phi tag anywhere in its type graph must render
// byte-identically to a logger without this handler installed. That is what
// phitag_test.go's byte-diff test guards, and it is why every decision here is
// gated on "does this type have a phi path at all" before anything is
// marshalled, walked or replaced. The withdrawn design's failure mode was
// breadth — reflecting over values it had no business touching.
//
// # Fail closed
//
// This layer only ever touches values already known to carry PHI. So when
// anything goes wrong — json.Marshal errors on a cycle or an unsupported type,
// the rendering exceeds the size guard, or the masking walk cannot complete —
// the *entire* attribute value is replaced by the marker. Emitting the value
// unmasked would publish exactly the field the tag exists to protect, and
// emitting a partially masked structure would publish the part that came after
// the failure.
//
// # Known limitations, stated rather than discovered later
//
//   - Interface-typed elements (`[]any`, `map[string]any`) carry no tag on the
//     static element type, so no path is computed for them. Do not log PHI
//     through `any` collections. An `any`-typed *field* that is itself tagged
//     is masked, because the tag is on the field, not the element.
//   - A type implementing json.Marshaler or encoding.TextMarshaler renders
//     however it likes, so no paths are computed beneath it. A tagged field
//     *of* such a type is still masked in full. A custom marshaller that emits
//     PHI of its own is beyond this layer's reach by construction.
//   - The masked value is emitted as pre-rendered JSON, so it is designed for
//     slog's JSON handler. Under a text handler it renders as the JSON text
//     rather than Go's `%+v` form — different, but never unmasked.

import (
	"bytes"
	"cmp"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

const (
	// phiTagKey/phiTagValue are the struct tag this layer reads:
	// `helivantalog:"phi"`.
	phiTagKey   = "helivantalog"
	phiTagValue = "phi"

	// phiMarkerJSON is the replacement, already JSON-encoded. It matches the
	// `[REDACTED:<kind>]` shape the byte layer uses so a log consumer sees one
	// vocabulary regardless of which layer masked a value.
	phiMarkerJSON = `"[REDACTED:phi]"`

	// maxPHIMarshalBytes bounds the rendering this layer is willing to walk.
	// A value that exceeds it is masked whole rather than walked, so a
	// pathologically large graph costs one marshal and no walk. json.Marshal
	// itself already handles cycles (it errors) and shared references
	// (linearly), so this is a backstop for size, not for shape.
	maxPHIMarshalBytes = 1 << 20
)

var errPHIWalk = errors.New("logging: phi mask walk failed")

// phiNode is one position in a type's JSON shape. The tree is a graph rather
// than a strict tree: a recursive type's node references itself, which is what
// lets a finite chain of any depth be masked at every level without a depth
// cap. The withdrawn implementation's depth cap failed *open*, emitting
// plaintext past the limit; there is no cap here to fail, because the thing
// being walked is a finite marshalled document rather than an unbounded type
// graph.
type phiNode struct {
	// redact marks a position whose entire value — scalar or whole subtree —
	// is replaced by the marker.
	redact bool
	// fields maps a JSON object key to the node for its value.
	fields map[string]*phiNode
	// wild is the node for "any array element" or "any map value". Collection
	// element types not being traversed is precisely what leaked
	// `{"Rows":[{"Name":"SECRET-NAME"}]}` in the withdrawn implementation.
	wild *phiNode
}

// field returns the node for one object key. A JSON object is either a struct
// (matched by name) or a map (every key matched by the element node), and the
// walk cannot tell them apart from the bytes alone — so both are consulted.
// Missing this fallback left `map[string]patient` unmasked while the slice
// beside it was masked.
func (n *phiNode) field(key string) *phiNode {
	if n == nil {
		return nil
	}
	if child, ok := n.fields[key]; ok {
		return child
	}
	return n.wild
}

func (n *phiNode) element() *phiNode {
	if n == nil {
		return nil
	}
	return n.wild
}

// phiTreeCache memoises the computed shape per reflect.Type.
//
// Only the type the handler actually asked about is cached, and only after its
// computation has completed. Intermediate types reached during a computation
// are memoised *within* that computation and then discarded. That is
// deliberate: the withdrawn implementation seeded a shared map with a
// provisional answer before recursing, so an outer type could observe the seed
// and cache "no phi here" permanently — a race whose outcome depended on which
// of two mutually recursive types was logged first. Publishing only completed
// results makes concurrent computation harmless: two goroutines may both
// compute, and both compute the same complete answer.
var phiTreeCache sync.Map // reflect.Type -> *phiNode (possibly nil)

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// rendersItself reports whether t controls its own JSON rendering, in which
// case nothing can be said about the shape beneath it.
//
// This checks t exactly as given rather than also checking reflect.PointerTo(t):
// encoding/json uses a pointer receiver's marshaller only when the value is
// addressable, so a struct type whose MarshalJSON has a pointer receiver
// renders as a plain struct when logged by value. Declaring it opaque on the
// strength of a method json will not call would silently stop masking the
// tagged fields inside it — under-masking, which is the one direction this
// package must not be wrong in. A pointer type reaching here (*T with a
// pointer-receiver marshaller) does implement the interface directly and is
// correctly treated as opaque.
func rendersItself(t reflect.Type) bool {
	return t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType)
}

// phiTreeFor returns the phi shape of t, or nil when t carries no phi tag
// anywhere in its type graph. nil is the narrowness invariant's guarantee: the
// caller then leaves the value strictly alone.
func phiTreeFor(t reflect.Type) *phiNode {
	if cached, ok := phiTreeCache.Load(t); ok {
		tree, _ := cached.(*phiNode)
		return tree
	}
	tree := buildPHITree(t, map[reflect.Type]*phiNode{})
	if !reachesRedact(tree, map[*phiNode]bool{}) {
		// No tagged field is reachable. Store nil so the next lookup is a
		// single map hit and the value is never touched.
		tree = nil
	}
	phiTreeCache.Store(t, tree)
	return tree
}

// reachesRedact reports whether any position in the graph is masked. The
// visited set is required, not defensive: the graph is deliberately cyclic for
// recursive types.
func reachesRedact(n *phiNode, visited map[*phiNode]bool) bool {
	if n == nil || visited[n] {
		return false
	}
	visited[n] = true
	if n.redact {
		return true
	}
	if reachesRedact(n.wild, visited) {
		return true
	}
	for _, child := range n.fields {
		if reachesRedact(child, visited) {
			return true
		}
	}
	return false
}

// buildPHITree computes the JSON shape of t. seen memoises types within this
// one computation, which both terminates recursive types and makes their nodes
// self-referential so every level of a finite chain matches.
func buildPHITree(t reflect.Type, seen map[reflect.Type]*phiNode) *phiNode {
	if t == nil {
		return nil
	}
	for {
		if rendersItself(t) {
			return nil
		}
		if t.Kind() != reflect.Pointer {
			break
		}
		t = t.Elem()
	}
	if node, ok := seen[t]; ok {
		return node
	}

	switch t.Kind() {
	case reflect.Struct:
		node := &phiNode{fields: map[string]*phiNode{}}
		seen[t] = node
		addStructFields(t, node, seen, false, map[reflect.Type]bool{})
		return node
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			// []byte renders as a base64 string, not an array. There is no
			// element position to match.
			return nil
		}
		node := &phiNode{}
		seen[t] = node
		node.wild = buildPHITree(t.Elem(), seen)
		return node
	case reflect.Map:
		node := &phiNode{}
		seen[t] = node
		node.wild = buildPHITree(t.Elem(), seen)
		return node
	default:
		// Scalars, interfaces, channels, funcs. An interface's dynamic type is
		// unknowable from the static type; see the limitation noted above.
		return nil
	}
}

// fieldCandidate is one serialisable position discovered while walking a
// struct and everything it embeds, tagged with the embedding depth at which it
// was found so encoding/json's shallowest-wins rule can be applied afterwards.
type fieldCandidate struct {
	name  string
	depth int
	phi   bool
	typ   reflect.Type
	// fromTag records that name came from a json tag rather than the Go field
	// name. encoding/json breaks an equal-depth tie in favour of the single
	// tag-named candidate, so this is not decoration — it decides which
	// field's type the emitted key is bound to.
	fromTag bool
}

// addStructFields mirrors encoding/json's field rules for one struct type,
// writing the resulting positions into node.
//
// It collects candidates across every embedding level first and resolves name
// conflicts afterwards, because encoding/json resolves them by depth: an outer
// struct's own field hides a field of the same JSON name promoted from
// something it embeds. Recording positions as they were encountered instead
// let a promoted field win over the outer field that actually renders — which
// masked an untagged `ward` because an embedded type happened to have one too.
func addStructFields(t reflect.Type, node *phiNode, seen map[reflect.Type]*phiNode, force bool, promoting map[reflect.Type]bool) {
	var candidates []fieldCandidate
	collectFields(t, 0, force, promoting, &candidates)

	byName := map[string][]fieldCandidate{}
	for _, c := range candidates {
		byName[c.name] = append(byName[c.name], c)
	}
	best := make(map[string]fieldCandidate, len(byName))
	for name, group := range byName {
		best[name] = resolveFieldName(group)
	}

	for name, c := range best {
		if c.phi {
			// A fresh node, never a shared one: marking redact on a node
			// reached from the memo map would mask that type everywhere it
			// appears, tagged or not.
			node.fields[name] = &phiNode{redact: true}
			continue
		}
		if child := buildPHITree(c.typ, seen); child != nil {
			node.fields[name] = child
		}
	}
}

// jsonEmittedName reports the JSON name encoding/json will use for a field
// carrying this json struct tag, or "" when the field renders under its Go
// field name instead.
//
// It answers the question by *asking encoding/json*, not by reimplementing it.
// encoding/json silently ignores a tag name it considers invalid — Go 1.26
// honours `json:"aé"`, `json:"имя"` and `json:"名前"` but rejects `json:"नाम"`
// and `json:"पता"` — and the rule behind that (isValidTag) is unexported and
// has moved between releases. Two attempts to work around not knowing it both
// leaked: guessing the rule under-masks whenever the guess is wrong, and
// hedging by *also* claiming the Go field name let a field claim a key it
// never emits and outrank the field that does. A Devanagari field name on a
// patient DTO is an ordinary thing to write in an Indian hospital system, so
// neither failure was exotic.
//
// Asking removes the guess: build a one-field struct carrying the same tag,
// marshal it, and read which key came out. That is exactly the rule
// encoding/json will apply to the real field, whatever release it comes from,
// and it makes the Go-field-name fallback — and the entire class of defect it
// created — unnecessary.
func jsonEmittedName(tag string) string {
	declared, _, _ := strings.Cut(tag, ",")
	if declared == "" {
		return ""
	}
	if cached, ok := jsonTagNameCache.Load(tag); ok {
		name, _ := cached.(string)
		return name
	}
	name := probeJSONTagName(tag, declared)
	jsonTagNameCache.Store(tag, name)
	return name
}

var jsonTagNameCache sync.Map // json tag string -> emitted name ("" = Go field name)

// probeTagFieldName is the Go field name used by the probe struct. It is
// deliberately unlikely to collide with a real tag name; if it ever did, both
// answers coincide anyway.
const probeTagFieldName = "HMSLogProbeField"

// probeJSONTagName marshals a synthetic single-field struct carrying tag and
// reports the emitted key if it is the declared name, or "" if encoding/json
// fell back to the field name.
//
// Anything unexpected — a panic from reflect.StructOf, a marshal error, a key
// that is neither candidate — returns "" as well, which routes the field to
// its Go field name. That is the same answer encoding/json gives for a
// rejected tag, so the failure mode is the common case rather than a new one.
func probeJSONTagName(tag, declared string) (name string) {
	defer func() {
		if recover() != nil {
			name = ""
		}
	}()

	probe := reflect.StructOf([]reflect.StructField{{
		Name: probeTagFieldName,
		Type: reflect.TypeFor[string](),
		// strconv.Quote so a tag containing a quote or backslash survives being
		// re-embedded in a struct tag literal exactly as it was written.
		Tag: reflect.StructTag("json:" + strconv.Quote(tag)),
	}})
	val := reflect.New(probe).Elem()
	// A non-empty value, so `omitempty` cannot omit the field and hide the
	// answer.
	val.Field(0).SetString("x")

	raw, err := json.Marshal(val.Interface())
	if err != nil {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if _, ok := obj[declared]; ok {
		return declared
	}
	return ""
}

// resolveFieldName picks the candidate encoding/json would bind a JSON name to.
//
// The rule, from encoding/json's dominantField: the shallowest depth wins, and
// among equally shallow candidates the single one whose name came from a json
// tag wins; if that is still ambiguous the name is dropped from the output
// entirely. Keeping the first-seen candidate instead — which is what this
// function replaced — bound the emitted key to the losing field's type
// whenever the tagged field happened to be declared second, so no path matched
// and tagged PHI went out in plaintext. It was declaration-order dependent,
// which is the same order-dependence class that sank the withdrawn design.
//
// One deliberate deviation from encoding/json: where the winner is genuinely
// ambiguous and the tied candidates disagree — different types, or different
// tag status — the position is marked as PHI. encoding/json drops such a name
// entirely, so in the ordinary case nothing is emitted there and the mark is
// inert; it exists so that if this mirror of the rule is ever wrong, the error
// lands on the masking side. resolveFieldName is unit-tested directly in
// phitag_internal_test.go, because that branch is unobservable through the
// logger by construction and would otherwise go unasserted.
func resolveFieldName(group []fieldCandidate) fieldCandidate {
	pool := group

	minDepth := pool[0].depth
	for _, c := range pool {
		minDepth = min(minDepth, c.depth)
	}
	var shallowest []fieldCandidate
	for _, c := range pool {
		if c.depth == minDepth {
			shallowest = append(shallowest, c)
		}
	}
	if len(shallowest) == 1 {
		return shallowest[0]
	}

	var tagged []fieldCandidate
	for _, c := range shallowest {
		if c.fromTag {
			tagged = append(tagged, c)
		}
	}
	if len(tagged) == 1 {
		return tagged[0]
	}

	// Ambiguous. If every tied candidate says the same thing there is nothing
	// to get wrong; otherwise fail closed on the position.
	winner := shallowest[0]
	for _, c := range shallowest[1:] {
		if c.typ != winner.typ || c.phi != winner.phi {
			winner.phi = true
			winner.typ = nil
			return winner
		}
	}
	return winner
}

// collectFields walks t and everything it anonymously embeds, appending one
// candidate per serialisable position.
//
// force is set while descending into an embedded field that itself carries the
// phi tag: its fields are promoted into the parent object, so "mask that
// field" means "mask every position it promotes". promoting guards against an
// embedding cycle (`type a struct { *a }`), which Go permits through a
// pointer.
func collectFields(t reflect.Type, depth int, force bool, promoting map[reflect.Type]bool, out *[]fieldCandidate) {
	if promoting[t] {
		return
	}
	promoting[t] = true
	defer delete(promoting, t)

	for i := range t.NumField() {
		sf := t.Field(i)

		embedded := sf.Type
		for embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		if sf.Anonymous {
			// encoding/json ignores an embedded field of an unexported
			// *non-struct* type, but promotes an embedded struct even when the
			// struct's own type name is unexported. Getting that backwards is
			// what published names through an unexported embedded type in the
			// withdrawn implementation.
			if !sf.IsExported() && embedded.Kind() != reflect.Struct {
				continue
			}
		} else if !sf.IsExported() {
			// Unexported fields are never serialised, so they have no path and
			// cannot be masked — or published.
			continue
		}

		tag := sf.Tag.Get("json")
		if tag == "-" {
			// Exactly "-" means "never serialise". `json:"-,"` means a field
			// literally named "-", which is the next branch's business.
			continue
		}
		name := jsonEmittedName(tag)

		// The phi tag is read as a comma-separated option list so a future
		// `helivantalog:"phi,<option>"` still masks. Comparing the whole tag against
		// "phi" made `helivantalog:"phi,strict"` silently mask nothing — a tag that
		// looks like it is doing its job and is not.
		tagOpt, _, _ := strings.Cut(sf.Tag.Get(phiTagKey), ",")
		phi := force || tagOpt == phiTagValue

		// The promotion test uses the *effective* name, matching
		// encoding/json: an embedded struct whose tag name was rejected has no
		// name, so it is promoted rather than nested.
		if sf.Anonymous && name == "" && embedded.Kind() == reflect.Struct && !rendersItself(sf.Type) && !rendersItself(embedded) {
			collectFields(embedded, depth+1, phi, promoting, out)
			continue
		}

		*out = append(*out, fieldCandidate{
			name: cmp.Or(name, sf.Name), depth: depth, phi: phi, typ: sf.Type, fromTag: name != "",
		})
	}
}

// maskPHIValue renders v and masks every tagged position in the rendering.
//
// It reports ok=false when v's type carries no phi tag, which is the signal to
// leave the value strictly untouched. When the type *does* carry a tag but
// rendering or walking fails, it returns the marker for the whole value: at
// that point something known to contain PHI cannot be safely rendered, and the
// only answer that cannot leak is to emit none of it.
func maskPHIValue(v any) (masked redactedJSON, count uint64, ok bool) {
	if v == nil {
		return nil, 0, false
	}
	tree := phiTreeFor(reflect.TypeOf(v))
	if tree == nil {
		return nil, 0, false
	}

	raw, err := marshalNoHTMLEscape(v)
	if err != nil || len(raw) > maxPHIMarshalBytes {
		return redactedJSON(phiMarkerJSON), 1, true
	}
	out, n, err := maskJSONPaths(raw, tree)
	if err != nil {
		return redactedJSON(phiMarkerJSON), 1, true
	}
	return redactedJSON(out), n, true
}

// marshalNoHTMLEscape renders v the way slog's JSON handler would, which is
// with HTML escaping off. json.Marshal escapes `<`, `>` and `&`; slog does
// not, and a masked value rendering its untagged neighbours differently from
// an unmasked one would break byte-level log tooling for no reason.
func marshalNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("logging: marshal for phi masking: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// redactedJSON is pre-rendered JSON handed back to slog. It marshals to
// itself, so slog's JSON handler emits the masked bytes verbatim, and it
// stringifies to the same text so a text handler shows masked output rather
// than a byte array.
type redactedJSON []byte

func (r redactedJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }
func (r redactedJSON) String() string               { return string(r) }

// maskJSONPaths streams data through encoding/json's token reader and
// re-encodes it, replacing the value at every position the tree marks.
//
// It re-encodes from tokens rather than decoding into map[string]any because
// key order must survive — a Go map has none, and a log line whose fields
// shuffle between records is a different line to every tool that reads it.
// Numbers are carried as json.Number and re-emitted literally, so `1.0` does
// not become `1` and a large integer does not acquire an exponent.
//
// Any structural surprise returns an error rather than partial output, which
// the caller turns into a fully masked value.
func maskJSONPaths(data []byte, root *phiNode) ([]byte, uint64, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	// masked counts replacements for RedactionCount, in the same unit the byte
	// layer uses.
	var masked uint64
	var out bytes.Buffer
	out.Grow(len(data))

	var strBuf bytes.Buffer
	strEnc := json.NewEncoder(&strBuf)
	strEnc.SetEscapeHTML(false)
	writeString := func(s string) error {
		strBuf.Reset()
		if err := strEnc.Encode(s); err != nil {
			return fmt.Errorf("%w: encode string: %w", errPHIWalk, err)
		}
		out.Write(bytes.TrimSuffix(strBuf.Bytes(), []byte("\n")))
		return nil
	}

	// frame tracks one open container so commas and object key/value
	// alternation land in the right place, and so the node describing the
	// container's children travels with it.
	type frame struct {
		array     bool
		count     int
		expectKey bool
		node      *phiNode // node for this container
		cur       *phiNode // node for the value about to be written (objects)
	}
	var stack []frame

	// nextNode is the node describing the value that is about to be emitted.
	nextNode := func() *phiNode {
		if len(stack) == 0 {
			return root
		}
		top := stack[len(stack)-1]
		if top.array {
			return top.node.element()
		}
		return top.cur
	}
	beforeValue := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		if top.array && top.count > 0 {
			out.WriteByte(',')
		}
	}
	afterValue := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		top.count++
		if !top.array {
			top.expectKey = true
			top.cur = nil
		}
	}

	done := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", errPHIWalk, err)
		}
		if done {
			// A second top-level value. json.Marshal never produces one; if
			// something did, refuse rather than concatenate.
			return nil, 0, fmt.Errorf("%w: trailing value", errPHIWalk)
		}

		// A closing delimiter is neither a value nor a key, so it is handled
		// before anything consults the node for "the next value". Letting it
		// reach that decision made an object close on the marker left behind
		// by its own last key, which unbalanced the walk.
		if d, isDelim := tok.(json.Delim); isDelim && (d == '}' || d == ']') {
			if len(stack) == 0 {
				return nil, 0, fmt.Errorf("%w: unbalanced delimiter", errPHIWalk)
			}
			if d == '}' {
				out.WriteByte('}')
			} else {
				out.WriteByte(']')
			}
			stack = stack[:len(stack)-1]
			afterValue()
			if len(stack) == 0 {
				done = true
			}
			continue
		}

		// Object keys are handled next: they select the node for the value
		// that follows and are never themselves masked.
		if s, isStr := tok.(string); isStr && len(stack) > 0 {
			if top := &stack[len(stack)-1]; !top.array && top.expectKey {
				if top.count > 0 {
					out.WriteByte(',')
				}
				if err := writeString(s); err != nil {
					return nil, 0, err
				}
				out.WriteByte(':')
				top.expectKey = false
				top.cur = top.node.field(s)
				continue
			}
		}

		if n := nextNode(); n != nil && n.redact {
			beforeValue()
			out.WriteString(phiMarkerJSON)
			masked++
			if err := skipValue(dec, tok); err != nil {
				return nil, 0, err
			}
			afterValue()
			if len(stack) == 0 {
				done = true
			}
			continue
		}

		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				node := nextNode()
				beforeValue()
				out.WriteByte('{')
				stack = append(stack, frame{expectKey: true, node: node})
			case '[':
				node := nextNode()
				beforeValue()
				out.WriteByte('[')
				stack = append(stack, frame{array: true, node: node})
			default:
				// '}' and ']' are handled above, before the node lookup.
				return nil, 0, fmt.Errorf("%w: unexpected delimiter %q", errPHIWalk, t)
			}
		case string:
			beforeValue()
			if err := writeString(t); err != nil {
				return nil, 0, err
			}
			afterValue()
		case json.Number:
			beforeValue()
			out.WriteString(t.String())
			afterValue()
		case bool:
			beforeValue()
			if t {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
			afterValue()
		case nil:
			beforeValue()
			out.WriteString("null")
			afterValue()
		default:
			return nil, 0, fmt.Errorf("%w: unexpected token %T", errPHIWalk, tok)
		}

		if len(stack) == 0 {
			done = true
		}
	}

	if len(stack) != 0 || !done {
		return nil, 0, fmt.Errorf("%w: incomplete document", errPHIWalk)
	}
	return out.Bytes(), masked, nil
}

// skipValue consumes the remainder of the value whose first token is tok, so a
// masked subtree's contents never reach the output.
func skipValue(dec *json.Decoder, tok json.Token) error {
	d, isDelim := tok.(json.Delim)
	if !isDelim {
		return nil
	}
	if d == '}' || d == ']' {
		return fmt.Errorf("%w: unbalanced delimiter", errPHIWalk)
	}
	depth := 1
	for depth > 0 {
		next, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: skip subtree: %w", errPHIWalk, err)
		}
		if dd, ok := next.(json.Delim); ok {
			switch dd {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// NewPHITagHandler wraps inner so that attribute values whose type declares
// `helivantalog:"phi"` fields are masked before inner sees them.
//
// Everything else passes through untouched — same record, same attributes,
// same values — so a logger with this handler installed and one without emit
// byte-identical output for any value that carries no tag.
func NewPHITagHandler(inner slog.Handler) slog.Handler {
	return &phiTagHandler{inner: inner}
}

type phiTagHandler struct{ inner slog.Handler }

func (h *phiTagHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *phiTagHandler) Handle(ctx context.Context, r slog.Record) error {
	// Rebuild the record only when something actually needs masking. A record
	// that is passed through unchanged cannot differ from the unwrapped
	// handler's output in any respect — not attribute order, not the PC, not
	// the resolution of a LogValuer — which is a stronger guarantee than
	// rebuilding it faithfully.
	needs := false
	r.Attrs(func(a slog.Attr) bool {
		if attrNeedsMask(a) {
			needs = true
			return false
		}
		return true
	})
	if !needs {
		return h.inner.Handle(ctx, r)
	}

	masked := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		masked.AddAttrs(maskAttr(a))
		return true
	})
	return h.inner.Handle(ctx, masked)
}

func (h *phiTagHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := attrs
	copied := false
	for i, a := range attrs {
		if !attrNeedsMask(a) {
			continue
		}
		if !copied {
			out = make([]slog.Attr, len(attrs))
			copy(out, attrs)
			copied = true
		}
		out[i] = maskAttr(a)
	}
	return &phiTagHandler{inner: h.inner.WithAttrs(out)}
}

func (h *phiTagHandler) WithGroup(name string) slog.Handler {
	return &phiTagHandler{inner: h.inner.WithGroup(name)}
}

// attrNeedsMask reports whether a — or, for a group, anything inside it —
// holds a value whose type declares a phi field. It resolves a LogValuer
// because slog will resolve it too, and the resolved value is what gets
// serialised.
func attrNeedsMask(a slog.Attr) bool {
	switch a.Value.Kind() {
	case slog.KindGroup:
		for _, g := range a.Value.Group() {
			if attrNeedsMask(g) {
				return true
			}
		}
		return false
	case slog.KindAny:
		return phiTreeOf(a.Value.Any()) != nil
	case slog.KindLogValuer:
		return attrNeedsMask(slog.Attr{Key: a.Key, Value: a.Value.Resolve()})
	default:
		return false
	}
}

func phiTreeOf(v any) *phiNode {
	if v == nil {
		return nil
	}
	return phiTreeFor(reflect.TypeOf(v))
}

// maskAttr returns a with every tagged value inside it masked. It is only
// called for attributes attrNeedsMask has already accepted, so it never
// rewrites a value that had nothing to mask.
func maskAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, g := range group {
			if attrNeedsMask(g) {
				out[i] = maskAttr(g)
				continue
			}
			out[i] = g
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindLogValuer:
		return maskAttr(slog.Attr{Key: a.Key, Value: a.Value.Resolve()})
	case slog.KindAny:
		out, n, ok := maskPHIValue(a.Value.Any())
		if !ok {
			return a
		}
		redactions.Add(n)
		return slog.Attr{Key: a.Key, Value: slog.AnyValue(out)}
	default:
		return a
	}
}
