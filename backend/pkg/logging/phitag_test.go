package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

const phiMarker = "[REDACTED:phi]"

// secretName is deliberately distinctive and contains no digits, so the only
// thing that can mask it is the tag layer — the byte layer's patterns cannot
// see it. Every leak assertion in this file is "this string must not appear".
const secretName = "SECRET-NAME-Anandi-Gopal"

// --- fixtures ---------------------------------------------------------------

type patient struct {
	ID     string `json:"id"`
	Name   string `json:"name" helivantalog:"phi"`
	DOB    string `json:"dob,omitempty" helivantalog:"phi"`
	Ward   string `json:"ward"`
	Secret string `json:"-"`
}

func samplePatient() patient {
	return patient{ID: "p-1", Name: secretName, DOB: "1865-03-31", Ward: "A", Secret: "HIDDEN-BY-JSON-DASH"}
}

// rows is the untagged wrapper that leaked every name in the withdrawn
// implementation: nothing on this type is tagged, and it is only reachable to
// masking because collection *element* types are traversed.
type rows struct {
	Total int                `json:"total"`
	Rows  []patient          `json:"rows"`
	ByID  map[string]patient `json:"by_id"`
	Ptrs  []*patient         `json:"ptrs"`
	Deep  [][]patient        `json:"deep"`
}

// --- helpers ----------------------------------------------------------------

// wrapped and plain build the same JSON logger with and without the tag
// handler, both writing through the shipped redacting writer, and both with
// the time attribute dropped so two runs are comparable byte for byte.
func dropTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

func wrapped(w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(logging.NewRedactingWriter(w), &slog.HandlerOptions{ReplaceAttr: dropTime})
	return slog.New(logging.NewPHITagHandler(h))
}

func plain(w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(logging.NewRedactingWriter(w), &slog.HandlerOptions{ReplaceAttr: dropTime})
	return slog.New(h)
}

// logged returns the single line the wrapped logger emits for one attribute.
func logged(t *testing.T, key string, v any) string {
	t.Helper()
	var buf bytes.Buffer
	wrapped(&buf).Info("event", key, v)
	line := buf.String()
	require.True(t, json.Valid([]byte(strings.TrimSpace(line))), "emitted line must be valid JSON: %s", line)
	return line
}

func requireMasked(t *testing.T, line string) {
	t.Helper()
	require.NotContains(t, line, secretName, "tagged field leaked in plaintext")
	require.Contains(t, line, phiMarker, "expected the phi marker")
}

// --- criterion: a tagged struct as the attribute value -----------------------

func TestTaggedStructIsMasked(t *testing.T) {
	line := logged(t, "patient", samplePatient())
	requireMasked(t, line)

	var rec struct {
		Patient patient `json:"-"`
		Raw     json.RawMessage
	}
	_ = rec
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &got))
	p := got["patient"].(map[string]any)
	require.Equal(t, phiMarker, p["name"])
	require.Equal(t, phiMarker, p["dob"])
	require.Equal(t, "p-1", p["id"], "untagged siblings must survive")
	require.Equal(t, "A", p["ward"])
}

func TestTaggedPointerToStructIsMasked(t *testing.T) {
	p := samplePatient()
	requireMasked(t, logged(t, "patient", &p))
}

// --- criterion: collections --------------------------------------------------

func TestTaggedCollectionsAreMasked(t *testing.T) {
	p := samplePatient()
	for name, v := range map[string]any{
		"slice":        []patient{p, p},
		"map":          map[string]patient{"a": p},
		"slice_ptr":    []*patient{&p, nil, &p},
		"array":        [2]patient{p, p},
		"map_of_ptr":   map[string]*patient{"a": &p},
		"nested":       [][]patient{{p}, {p, p}},
		"map_of_slice": map[string][]patient{"x": {p}},
	} {
		t.Run(name, func(t *testing.T) {
			requireMasked(t, logged(t, "v", v))
		})
	}
}

// --- criterion: the untagged wrapper, the shape that leaked ------------------

func TestUntaggedWrapperHoldingTaggedCollectionsIsMasked(t *testing.T) {
	p := samplePatient()
	line := logged(t, "page", rows{
		Total: 2,
		Rows:  []patient{p, p},
		ByID:  map[string]patient{"p-1": p},
		Ptrs:  []*patient{&p},
		Deep:  [][]patient{{p}},
	})
	requireMasked(t, line)
	require.Equal(t, 10, strings.Count(line, phiMarker),
		"every name and dob across every collection must be masked: %s", line)
	require.Contains(t, line, `"total":2`, "untagged scalars must survive")
}

// --- a tagged field whose value is a whole subtree ---------------------------

type record struct {
	Meta     string            `json:"meta"`
	Patient  patient           `json:"patient" helivantalog:"phi"`
	Contacts []patient         `json:"contacts" helivantalog:"phi"`
	Extra    map[string]string `json:"extra" helivantalog:"phi"`
	Opaque   any               `json:"opaque" helivantalog:"phi"`
}

func TestTaggedSubtreesAreReplacedWholeAndTheirContentsSkipped(t *testing.T) {
	p := samplePatient()
	line := logged(t, "v", record{
		Meta:     "keep",
		Patient:  p,
		Contacts: []patient{p, p},
		Extra:    map[string]string{"addr": secretName},
		Opaque:   []any{secretName, map[string]any{"k": secretName}},
	})
	requireMasked(t, line)
	require.NotContains(t, line, "p-1", "a masked subtree must be replaced, not annotated")
	require.Equal(t, 4, strings.Count(line, phiMarker), line)
	require.Contains(t, line, `"meta":"keep"`)
	require.Contains(t, line,
		`{"meta":"keep","patient":"`+phiMarker+`","contacts":"`+phiMarker+`","extra":"`+phiMarker+`","opaque":"`+phiMarker+`"}`,
		"key order and structure must survive whole-subtree masking: %s", line)
}

// --- criterion: json:"-" is never published ---------------------------------

func TestJSONDashFieldIsNeverPublished(t *testing.T) {
	line := logged(t, "patient", samplePatient())
	require.NotContains(t, line, "HIDDEN-BY-JSON-DASH",
		"the tag layer must never publish what the type withheld")
	require.NotContains(t, line, "Secret")
}

type dashNamedField struct {
	//nolint:staticcheck // SA5008: the point of this fixture is the `json:"-,"`
	// form, which names a field "-" rather than omitting it. Exercising that
	// distinction is the test.
	Real string `json:"-,"`
	Name string `json:"name" helivantalog:"phi"`
}

func TestFieldLiterallyNamedDashSurvives(t *testing.T) {
	line := logged(t, "v", dashNamedField{Real: "KEEP-ME", Name: secretName})
	requireMasked(t, line)
	require.Contains(t, line, `"-":"KEEP-ME"`, `json:"-," names a field "-" and must still render`)
}

// --- criterion: renaming and omitempty --------------------------------------

func TestRenamingAndOmitemptyAreHonoured(t *testing.T) {
	p := samplePatient()
	p.DOB = ""
	line := logged(t, "patient", p)
	requireMasked(t, line)
	require.NotContains(t, line, `"dob"`, "omitempty must still omit")
	require.NotContains(t, line, `"Name"`, "the Go field name must never appear; json renamed it")
	require.Contains(t, line, `"name":"`+phiMarker+`"`)
}

type untaggedJSONName struct {
	Whatever string `json:"name"`
}

func TestRenamingDoesNotMaskAnUnrelatedTypeWithTheSameKey(t *testing.T) {
	line := logged(t, "v", untaggedJSONName{Whatever: "public"})
	require.Contains(t, line, `"name":"public"`, "paths are per type, not per key name")
	require.NotContains(t, line, phiMarker)
}

// --- naming rules encoding/json owns and this layer must not diverge from ----

type optionTag struct {
	Name string `json:"name" helivantalog:"phi,strict"`
}

func TestPHITagWithOptionsStillMasks(t *testing.T) {
	requireMasked(t, logged(t, "v", optionTag{Name: secretName}))
}

type rejectedTagName struct {
	// encoding/json rejects this tag name and falls back to the Go field name
	// "Name". A path computed only from the tag would never match the key that
	// is actually emitted.
	Name string `json:"नाम" helivantalog:"phi"`
}

func TestTagNameRejectedByEncodingJSONStillMasks(t *testing.T) {
	line := logged(t, "v", rejectedTagName{Name: secretName})
	requireMasked(t, line)
}

type htmlishTagName struct {
	Weird string `json:"a<b>&c" helivantalog:"phi"`
	Ok    string `json:"ok"`
}

func TestPunctuationHeavyTagNamesAreMatchedAsSlogWritesThem(t *testing.T) {
	line := logged(t, "v", htmlishTagName{Weird: secretName, Ok: "<fine>"})
	requireMasked(t, line)
	require.Contains(t, line, `"a<b>&c":"`+phiMarker+`"`, "keys must not acquire HTML escaping: %s", line)
	require.Contains(t, line, `"ok":"<fine>"`)
}

type jsonStringOption struct {
	N    int64  `json:"n,string"`
	Name string `json:"name" helivantalog:"phi"`
}

func TestJSONStringOptionIsUntouched(t *testing.T) {
	line := logged(t, "v", jsonStringOption{N: 12, Name: secretName})
	requireMasked(t, line)
	require.Contains(t, line, `"n":"12"`)
}

// --- criterion: embedded structs, including unexported type names -----------

type exportedEmbed struct {
	Name string `json:"name" helivantalog:"phi"`
}

type unexportedEmbed struct {
	Alias string `json:"alias" helivantalog:"phi"`
}

type withEmbeds struct {
	exportedEmbedAliasFree
	unexportedEmbed
	Ward string `json:"ward"`
}

type exportedEmbedAliasFree = exportedEmbed

type namedEmbed struct {
	exportedEmbed `json:"inner"`
	Ward          string `json:"ward"`
}

type embedTaggedWhole struct {
	patient `helivantalog:"phi"`
	Ward    string `json:"ward"`
}

func TestEmbeddedStructsArePromotedAndMasked(t *testing.T) {
	line := logged(t, "v", withEmbeds{
		exportedEmbedAliasFree: exportedEmbed{Name: secretName},
		unexportedEmbed:        unexportedEmbed{Alias: secretName},
		Ward:                   "B",
	})
	requireMasked(t, line)
	require.Equal(t, 2, strings.Count(line, phiMarker),
		"both promoted fields must be masked, including the one under an unexported type name: %s", line)
	require.Contains(t, line, `"ward":"B"`)
}

func TestEmbeddedStructWithAJSONNameIsNested(t *testing.T) {
	line := logged(t, "v", namedEmbed{exportedEmbed: exportedEmbed{Name: secretName}, Ward: "B"})
	requireMasked(t, line)
	require.Contains(t, line, `"inner":{"name":"`+phiMarker+`"}`)
}

func TestTaggedEmbeddedStructMasksEveryPromotedField(t *testing.T) {
	line := logged(t, "v", embedTaggedWhole{patient: samplePatient(), Ward: "B"})
	requireMasked(t, line)
	require.NotContains(t, line, "p-1", "a tagged embedded field masks everything it promotes")
	require.Contains(t, line, `"ward":"B"`)
}

// --- criterion: custom marshallers ------------------------------------------

type codedID struct{ raw string }

func (c codedID) MarshalJSON() ([]byte, error) { return json.Marshal("CODE:" + c.raw) }

type textID struct{ raw string }

func (t textID) MarshalText() ([]byte, error) { return []byte("TEXT:" + t.raw), nil }

type withMarshallers struct {
	Code   codedID `json:"code"`
	Text   textID  `json:"text"`
	Hidden codedID `json:"hidden" helivantalog:"phi"`
	Name   string  `json:"name" helivantalog:"phi"`
}

func TestCustomMarshallersAreNotReshapedButTaggedOnesAreMasked(t *testing.T) {
	line := logged(t, "v", withMarshallers{
		Code:   codedID{raw: "abc"},
		Text:   textID{raw: "def"},
		Hidden: codedID{raw: "PRIVATE-CODE"},
		Name:   secretName,
	})
	requireMasked(t, line)
	require.Contains(t, line, `"code":"CODE:abc"`, "a json.Marshaler must render exactly as it chose")
	require.Contains(t, line, `"text":"TEXT:def"`, "an encoding.TextMarshaler must render exactly as it chose")
	require.NotContains(t, line, "PRIVATE-CODE", "a tagged field of a self-marshalling type is masked whole")
	require.Contains(t, line, `"hidden":"`+phiMarker+`"`)
}

// --- criterion: deep chains fail closed --------------------------------------

type chain struct {
	Name string `json:"name" helivantalog:"phi"`
	Next *chain `json:"next,omitempty"`
}

func buildChain(depth int) *chain {
	head := &chain{Name: secretName}
	cur := head
	for range depth - 1 {
		cur.Next = &chain{Name: secretName}
		cur = cur.Next
	}
	return head
}

func TestDeepFiniteChainIsMaskedAtEveryLevel(t *testing.T) {
	const depth = 500
	line := logged(t, "v", buildChain(depth))
	requireMasked(t, line)
	require.Equal(t, depth, strings.Count(line, phiMarker),
		"a recursive type must mask at every level, with no depth cap to fail open")
}

func TestOversizedValueFailsClosed(t *testing.T) {
	// Past the size guard the value is not walked at all; the whole attribute
	// is masked rather than partially walked and partially emitted.
	line := logged(t, "v", buildChain(60000))
	require.NotContains(t, line, secretName)
	require.Contains(t, line, `"v":"`+phiMarker+`"`)
}

type cyclic struct {
	Name string  `json:"name" helivantalog:"phi"`
	Self *cyclic `json:"self,omitempty"`
}

func TestCycleFailsClosed(t *testing.T) {
	c := &cyclic{Name: secretName}
	c.Self = c
	line := logged(t, "v", c)
	require.NotContains(t, line, secretName, "json.Marshal errors on a cycle; the value must be masked whole")
	require.Contains(t, line, `"v":"`+phiMarker+`"`)
}

type unmarshallable struct {
	Name string     `json:"name" helivantalog:"phi"`
	Fn   func() int `json:"fn"`
}

func TestUnsupportedTypeFailsClosed(t *testing.T) {
	line := logged(t, "v", unmarshallable{Name: secretName, Fn: func() int { return 1 }})
	require.NotContains(t, line, secretName)
	require.Contains(t, line, `"v":"`+phiMarker+`"`)
}

type explodingMarshaller struct {
	Name string `json:"name" helivantalog:"phi"`
	Bad  boom   `json:"bad"`
}

type boom struct{}

func (boom) MarshalJSON() ([]byte, error) { return nil, errors.New("nope") }

func TestFailingMarshallerFailsClosed(t *testing.T) {
	line := logged(t, "v", explodingMarshaller{Name: secretName})
	require.NotContains(t, line, secretName)
	require.Contains(t, line, `"v":"`+phiMarker+`"`)
}

// --- criterion: shared-reference DAG ----------------------------------------

type dagNode struct {
	Name string     `json:"name" helivantalog:"phi"`
	Kids []*dagNode `json:"kids,omitempty"`
}

func buildDAG(levels int) *dagNode {
	cur := &dagNode{Name: secretName}
	for i := range levels {
		cur = &dagNode{Name: fmt.Sprintf("n%d-%s", i, secretName), Kids: []*dagNode{cur, cur}}
	}
	return cur
}

func TestSharedReferenceDAGCompletesInBoundedTime(t *testing.T) {
	start := time.Now()
	line := logged(t, "v", buildDAG(20))
	elapsed := time.Since(start)
	require.NotContains(t, line, secretName)
	require.Contains(t, line, `"v":"`+phiMarker+`"`)
	require.Less(t, elapsed, 10*time.Second, "a 20-node shared-reference DAG must not wedge the caller")
	t.Logf("20-node DAG masked in %s", elapsed)
}

func TestLargeSliceCompletesInBoundedTime(t *testing.T) {
	list := make([]patient, 1000)
	for i := range list {
		list[i] = samplePatient()
	}
	start := time.Now()
	line := logged(t, "v", list)
	require.NotContains(t, line, secretName)
	require.Equal(t, 2000, strings.Count(line, phiMarker))
	t.Logf("1000-element slice masked in %s", time.Since(start))
}

// --- criterion: mutual recursion, either order, under -race ------------------

type mutualA struct {
	Name string   `json:"name" helivantalog:"phi"`
	B    *mutualB `json:"b,omitempty"`
}
type mutualB struct {
	Label string   `json:"label" helivantalog:"phi"`
	A     *mutualA `json:"a,omitempty"`
}

// A second, structurally identical pair. The type cache is process-wide, so
// testing "either order" needs two pairs: one exercised A-first, one B-first.
type mutualC struct {
	Name string   `json:"name" helivantalog:"phi"`
	D    *mutualD `json:"d,omitempty"`
}
type mutualD struct {
	Label string   `json:"label" helivantalog:"phi"`
	C     *mutualC `json:"c,omitempty"`
}

func TestMutuallyRecursiveTypesMaskRegardlessOfOrder(t *testing.T) {
	a := &mutualA{Name: secretName, B: &mutualB{Label: secretName, A: &mutualA{Name: secretName}}}
	b := &mutualB{Label: secretName, A: &mutualA{Name: secretName, B: &mutualB{Label: secretName}}}
	lineA := logged(t, "v", a)
	requireMasked(t, lineA)
	require.Equal(t, 3, strings.Count(lineA, phiMarker), lineA)
	lineB := logged(t, "v", b)
	requireMasked(t, lineB)
	require.Equal(t, 3, strings.Count(lineB, phiMarker), lineB)

	// Now the other order, on the untouched pair.
	d := &mutualD{Label: secretName, C: &mutualC{Name: secretName, D: &mutualD{Label: secretName}}}
	lineD := logged(t, "v", d)
	requireMasked(t, lineD)
	require.Equal(t, 3, strings.Count(lineD, phiMarker), lineD)
	c := &mutualC{Name: secretName, D: &mutualD{Label: secretName}}
	lineC := logged(t, "v", c)
	requireMasked(t, lineC)
	require.Equal(t, 2, strings.Count(lineC, phiMarker), lineC)
}

type raceA struct {
	Name string `json:"name" helivantalog:"phi"`
	B    *raceB `json:"b,omitempty"`
}
type raceB struct {
	Label string `json:"label" helivantalog:"phi"`
	A     *raceA `json:"a,omitempty"`
}

func TestConcurrentFirstUseOfMutuallyRecursiveTypes(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	l := wrapped(&phiLockedWriter{mu: &mu, w: &buf})

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				l.Info("e", "v", &raceA{Name: secretName, B: &raceB{Label: secretName}})
				return
			}
			l.Info("e", "v", &raceB{Label: secretName, A: &raceA{Name: secretName}})
		}()
	}
	wg.Wait()

	out := buf.String()
	require.NotContains(t, out, secretName, "no goroutine may observe a partial type computation")
	require.Equal(t, 64, strings.Count(out, phiMarker))
	require.Equal(t, 32, strings.Count(out, "\n"))
}

type phiLockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (lw *phiLockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

// --- criterion: the narrowness invariant ------------------------------------

type untaggedStruct struct {
	A string            `json:"a"`
	B int               `json:"b"`
	C []string          `json:"c"`
	D map[string]string `json:"d"`
	E *untaggedStruct   `json:"e,omitempty"`
	F time.Time         `json:"f"`
}

type stringerType struct{ s string }

func (s stringerType) String() string { return "stringer:" + s.s }

type valuerType struct{ s string }

func (v valuerType) LogValue() slog.Value { return slog.StringValue("valued:" + v.s) }

func TestUntaggedValuesRenderByteIdenticallyToAnUnwrappedLogger(t *testing.T) {
	fixed := time.Date(2026, 8, 13, 10, 30, 0, 0, time.UTC)
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")

	values := map[string]any{
		"nil":            nil,
		"string":         "plain <text> & more",
		"int":            42,
		"float":          1.5,
		"bool":           true,
		"struct":         untaggedStruct{A: "a", B: 1, C: []string{"x"}, D: map[string]string{"k": "v"}, F: fixed},
		"struct_ptr":     &untaggedStruct{A: "a", E: &untaggedStruct{A: "b"}},
		"slice":          []untaggedStruct{{A: "a"}, {A: "b"}},
		"map":            map[string]untaggedStruct{"k": {A: "a"}},
		"time":           fixed,
		"uuid":           id,
		"uuid_slice":     []uuid.UUID{id},
		"ip":             net.ParseIP("192.168.0.1"),
		"json_number":    json.Number("12345678901234"),
		"bytes":          []byte("hello"),
		"error":          errors.New("boom <&>"),
		"joined_error":   errors.Join(errors.New("a"), errors.New("b")),
		"stringer":       stringerType{s: "s"},
		"logvaluer":      valuerType{s: "v"},
		"big_int":        big.NewInt(9876543210),
		"raw_json":       json.RawMessage(`{"k":[1,2,3]}`),
		"empty_struct":   struct{}{},
		"nested_any":     map[string]any{"x": []any{1, "two", nil}},
		"duration":       3 * time.Second,
		"slice_of_slice": [][]int{{1, 2}, {3}},
	}

	for name, v := range values {
		t.Run(name, func(t *testing.T) {
			var withTag, without bytes.Buffer
			wrapped(&withTag).Info("event", "v", v, "request_id", "r-1")
			plain(&without).Info("event", "v", v, "request_id", "r-1")
			require.Equal(t, without.String(), withTag.String(),
				"an untagged value must render byte-identically with and without the tag handler")
			require.NotContains(t, withTag.String(), phiMarker)
		})
	}
}

func TestUntaggedValuesAreByteIdenticalInsideGroupsAndWithAttrs(t *testing.T) {
	build := func(l *slog.Logger) {
		l.With("svc", "hms").WithGroup("g").Info("event",
			"s", untaggedStruct{A: "a"},
			slog.Group("inner", "n", []untaggedStruct{{A: "b"}}),
		)
	}
	var withTag, without bytes.Buffer
	build(wrapped(&withTag))
	build(plain(&without))
	require.Equal(t, without.String(), withTag.String())
}

// --- criterion: composition with the byte layer ------------------------------

type contactable struct {
	Name  string `json:"name" helivantalog:"phi"`
	Phone string `json:"phone"`
	Ward  string `json:"ward"`
}

func TestTagAndPatternLayersBothApplyToOneValue(t *testing.T) {
	var buf bytes.Buffer
	wrapped(&buf).Info("admitted",
		"patient", contactable{Name: secretName, Phone: "9876543210", Ward: "A"},
		"request_id", "r-1",
		"tenant_id", "11111111-1111-1111-1111-111111111111",
		"subject", "gip-uid-7",
	)
	line := buf.String()

	require.Equal(t, 1, strings.Count(line, "\n"), "exactly one record")
	require.True(t, json.Valid([]byte(strings.TrimSpace(line))), "output must stay valid JSON: %s", line)
	require.NotContains(t, line, secretName, "tag layer")
	require.NotContains(t, line, "9876543210", "pattern layer")
	require.Contains(t, line, phiMarker)
	require.Contains(t, line, "[REDACTED:mobile]")

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &got))
	require.Equal(t, "r-1", got["request_id"], "correlation fields must survive both layers")
	require.Equal(t, "11111111-1111-1111-1111-111111111111", got["tenant_id"])
	require.Equal(t, "gip-uid-7", got["subject"])
	require.Equal(t, "A", got["patient"].(map[string]any)["ward"])
}

func TestKeyOrderIsPreservedThroughMasking(t *testing.T) {
	line := logged(t, "patient", samplePatient())
	require.Contains(t, line, `"patient":{"id":"p-1","name":"`+phiMarker+`","dob":"`+phiMarker+`","ward":"A"}`,
		"field order must survive the mask walk: %s", line)
}

func TestNumbersAreNotReformattedByMasking(t *testing.T) {
	type numbers struct {
		Name  string  `json:"name" helivantalog:"phi"`
		Whole float64 `json:"whole"`
		Big   int64   `json:"big"`
		Frac  float64 `json:"frac"`
	}
	line := logged(t, "v", numbers{Name: secretName, Whole: 1, Big: 4503599627370496, Frac: 0.125})
	requireMasked(t, line)
	require.Contains(t, line, `"whole":1`)
	require.Contains(t, line, `"big":4503599627370496`)
	require.Contains(t, line, `"frac":0.125`)
}

// --- groups, With, and the process logger -----------------------------------

func TestMaskingWorksInsideGroupsAndWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := wrapped(&buf).With("patient", samplePatient())
	l.Info("event", slog.Group("payload", "row", samplePatient()))
	line := buf.String()
	requireMasked(t, line)
	require.Equal(t, 4, strings.Count(line, phiMarker), line)
}

func TestProcessLoggerMasksTags(t *testing.T) {
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("admitted", "patient", samplePatient())
	line := buf.String()
	require.NotContains(t, line, secretName)
	require.Contains(t, line, phiMarker)
	require.Contains(t, line, `"time":`, "the real logger keeps its standard fields")
}

func TestMaskingCountsTowardsRedactionCount(t *testing.T) {
	before := logging.RedactionCount()
	var buf bytes.Buffer
	wrapped(&buf).Info("event", "patient", samplePatient())
	require.Greater(t, logging.RedactionCount(), before)
}

func TestHandlerRespectsInnerEnabled(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	l := slog.New(logging.NewPHITagHandler(h))
	l.Info("suppressed", "patient", samplePatient())
	require.Empty(t, buf.String())
	l.Warn("emitted", "patient", samplePatient())
	require.Contains(t, buf.String(), phiMarker)
}

// --- benchmarks --------------------------------------------------------------

func BenchmarkMaskTaggedStruct(b *testing.B) {
	l := wrapped(io.Discard)
	p := samplePatient()
	b.ReportAllocs()
	for b.Loop() {
		l.Info("event", "patient", p)
	}
}

func BenchmarkUntaggedPassthrough(b *testing.B) {
	l := wrapped(io.Discard)
	v := untaggedStruct{A: "a", B: 1}
	b.ReportAllocs()
	for b.Loop() {
		l.Info("event", "v", v)
	}
}

func BenchmarkMaskLargeSlice(b *testing.B) {
	list := make([]patient, 1000)
	for i := range list {
		list[i] = samplePatient()
	}
	l := wrapped(io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		l.Info("event", "v", list)
	}
}

func BenchmarkMaskSharedReferenceDAG(b *testing.B) {
	dag := buildDAG(20)
	l := wrapped(io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		l.Info("event", "v", dag)
	}
}
