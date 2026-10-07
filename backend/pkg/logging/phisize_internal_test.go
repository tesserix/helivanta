package logging

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sizeDAG struct {
	Name string     `json:"name" helivantalog:"phi"`
	Kids []*sizeDAG `json:"kids,omitempty"`
}

func buildSizeDAG(levels int) *sizeDAG {
	cur := &sizeDAG{Name: "leaf"}
	for range levels {
		cur = &sizeDAG{Name: "node", Kids: []*sizeDAG{cur, cur}}
	}
	return cur
}

// TestRenderBoundStopsAtTheLimit is the assertion #904 asks for in place of a
// wall-clock bound: the walk's own cost is bounded by the limit, not by the
// value. A 2^40-path DAG would render to ~90 TB; the walk must give up having
// touched no more nodes than the limit allows, because every node adds at
// least one byte to the bound.
func TestRenderBoundStopsAtTheLimit(t *testing.T) {
	const limit = 64 << 10

	_, visited, err := renderBound(buildSizeDAG(40), limit)

	require.ErrorIs(t, err, errRenderOversize)
	require.LessOrEqual(t, visited, limit+1,
		"the walk must stop within limit+1 nodes whatever the shape of the value")
}

// TestOversizeIsRefusedWithoutMarshalling proves the refusal happens BEFORE
// json.Marshal, which is the claim the issue makes and the one an
// output-only test cannot distinguish from a post-marshal size check.
func TestOversizeIsRefusedWithoutMarshalling(t *testing.T) {
	// The hook never forwards to the real marshal: if the pre-flight refusal
	// regressed, forwarding a 2^40-path value would exhaust memory instead of
	// failing this assertion.
	called := false
	orig := marshalForMask
	marshalForMask = func(any) ([]byte, error) {
		called = true
		return nil, errors.New("marshal reached")
	}
	t.Cleanup(func() { marshalForMask = orig })

	masked, n, ok := maskPHIValue(buildSizeDAG(40))

	require.True(t, ok)
	require.EqualValues(t, 1, n)
	require.Equal(t, `"[REDACTED:phi-oversize:*logging.sizeDAG]"`, string(masked))
	require.False(t, called, "an oversized value must be refused before it is marshalled")
}

// TestValueWithinTheBoundIsStillMarshalledAndMasked guards the other
// direction: the bound must not refuse ordinary values, or the masking path
// would silently stop logging anything.
func TestValueWithinTheBoundIsStillMarshalledAndMasked(t *testing.T) {
	masked, n, ok := maskPHIValue(buildSizeDAG(3))

	require.True(t, ok)
	require.EqualValues(t, 15, n, "2^4-1 nodes, each with one tagged name")
	require.NotContains(t, string(masked), "leaf")
	require.NotContains(t, string(masked), "phi-oversize")
}

// TestCycleIsNotReportedAsOversize keeps a cycle on the ordinary marker it has
// always had: json.Marshal errors on it, and "oversize" would misdescribe it.
func TestCycleIsNotReportedAsOversize(t *testing.T) {
	type loop struct {
		Name string `json:"name" helivantalog:"phi"`
		Next *loop  `json:"next"`
	}
	l := &loop{Name: "x"}
	l.Next = l

	_, _, err := renderBound(l, maxPHIMarshalBytes)
	require.ErrorIs(t, err, errRenderCycle)

	masked, _, ok := maskPHIValue(l)
	require.True(t, ok)
	require.Equal(t, phiMarkerJSON, string(masked))
}

// --- soundness: the bound is never below what encoding/json emits ----------

type boundEmbedded struct {
	Promoted string `json:"promoted"`
}

type boundTextKey int

func (k boundTextKey) MarshalText() ([]byte, error) {
	return []byte(strings.Repeat("k", int(k))), nil
}

type boundPtrMarshaler struct{ n int }

func (p *boundPtrMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strings.Repeat("p", p.n) + `"`), nil
}

type boundEverything struct {
	boundEmbedded
	Name     string `json:"name" helivantalog:"phi"`
	Renamed  string `json:"a_rather_long_json_name,omitempty"`
	Untagged string // key is the Go name
	//nolint:staticcheck // SA5008: `json:"-,"` names a field "-" rather than
	// omitting it; the bound must count it, so the fixture needs the form.
	Dash      string                  `json:"-,"`
	Skipped   string                  `json:"-"`
	AsString  int64                   `json:"as_string,string"`
	Neg       int64                   `json:"neg"`
	Big       uint64                  `json:"big"`
	Float     float64                 `json:"float"`
	Tiny      float32                 `json:"tiny"`
	Flag      bool                    `json:"flag"`
	Bytes     []byte                  `json:"bytes"`
	When      time.Time               `json:"when"`
	PtrM      boundPtrMarshaler       `json:"ptr_m"` // addressable only via a pointer root
	IntMap    map[int]string          `json:"int_map"`
	TextMap   map[boundTextKey]string `json:"text_map"`
	Any       any                     `json:"any"`
	NilPtr    *boundEmbedded          `json:"nil_ptr"`
	NilSlice  []string                `json:"nil_slice"`
	Array     [3]int8                 `json:"array"`
	Raw       json.RawMessage         `json:"raw"`
	unexposed string
}

func boundCorpus() []any {
	hostile := "quote\" back\\ ctl\x01\n\t bad\xff\xfe sep   देवनागरी <&> 🙂"
	full := &boundEverything{
		boundEmbedded: boundEmbedded{Promoted: hostile},
		Name:          hostile,
		Renamed:       "r",
		Untagged:      hostile,
		Dash:          "d",
		Skipped:       strings.Repeat("never rendered", 100),
		AsString:      math.MinInt64,
		Neg:           math.MinInt64,
		Big:           math.MaxUint64,
		Float:         -math.MaxFloat64,
		Tiny:          math.SmallestNonzeroFloat32,
		Flag:          false,
		Bytes:         []byte(hostile),
		When:          time.Date(2026, 10, 7, 9, 0, 0, 123456789, time.FixedZone("IST", 19800)),
		PtrM:          boundPtrMarshaler{n: 40},
		IntMap:        map[int]string{math.MinInt: hostile, 7: ""},
		TextMap:       map[boundTextKey]string{30: hostile},
		Any:           map[string]any{"nested": []any{1.5, "x", nil, true}},
		Array:         [3]int8{-128, 0, 127},
		Raw:           json.RawMessage(`{ "spaced" : [ 1 , 2 ] }`),
		unexposed:     strings.Repeat("u", 1000),
	}
	return []any{
		full,
		*full, // by value: PtrM is no longer addressable and renders as a struct
		[]*boundEverything{full, full, nil},
		buildSizeDAG(6),
		map[string]*boundEverything{hostile: full},
		&struct {
			Name string `json:"name" helivantalog:"phi"`
		}{Name: ""},
		// Dominated by self-rendering values, so a walk that guessed their
		// size instead of asking their marshaller could not hide behind the
		// slack in every other node's cost.
		&struct {
			Name  string             `json:"name" helivantalog:"phi"`
			Times []time.Time        `json:"t"`
			M     *boundPtrMarshaler `json:"m"`
			// Addressable through the pointer root, so encoding/json calls
			// the POINTER-receiver marshaller on this value field.
			P boundPtrMarshaler `json:"p"`
		}{Name: "x", Times: make([]time.Time, 200), M: &boundPtrMarshaler{n: 5000}, P: boundPtrMarshaler{n: 5000}},
	}
}

// TestRenderBoundIsAnUpperBound is the property every guarantee in
// phisize.go rests on: for each value, the bound is at least the length of
// what encoding/json actually emits (rendered the way maskPHIValue renders
// it). A bound below the real length would let an oversized value through to
// json.Marshal — the failure #904 exists to prevent.
func TestRenderBoundIsAnUpperBound(t *testing.T) {
	for i, v := range boundCorpus() {
		raw, err := marshalNoHTMLEscape(v)
		require.NoError(t, err, "corpus value %d must be marshalable", i)

		bound, _, err := renderBound(v, math.MaxInt)
		require.NoError(t, err)

		require.GreaterOrEqual(t, bound, len(raw),
			"corpus value %d (%T): bound %d is below the real rendering %d", i, v, bound, len(raw))
	}
}

// TestRenderBoundIsNotWildlyPessimistic guards the over-counting direction:
// a bound many times the real size would mask ordinary values as oversize.
// Plain ASCII and Indic text must be counted at (near) their real width, not
// at a flat worst-case multiplier.
func TestRenderBoundIsNotWildlyPessimistic(t *testing.T) {
	type note struct {
		Text string `json:"text" helivantalog:"phi"`
	}
	for _, s := range []string{
		strings.Repeat("ordinary ascii clinical note ", 2000),
		strings.Repeat("रोगी की स्थिति स्थिर है ", 2000),
	} {
		v := note{Text: s}
		raw, err := marshalNoHTMLEscape(v)
		require.NoError(t, err)
		bound, _, err := renderBound(v, math.MaxInt)
		require.NoError(t, err)
		require.LessOrEqual(t, bound, len(raw)+64,
			"a printable string must be bounded at its real width, plus key overhead")
	}
}

func TestStringCostIsExactForPrintableUTF8AndCoversEveryEscape(t *testing.T) {
	for _, s := range []string{"", "plain", "देवनागरी", "🙂", "\"", "\\", "\x00", "\n", "\xff", " ", "<&>"} {
		raw, err := marshalNoHTMLEscape(s)
		require.NoError(t, err)
		require.GreaterOrEqual(t, stringCost(s), len(raw), "%q", s)
	}
	require.Equal(t, len(`"plain"`), stringCost("plain"))
	require.Equal(t, len(`"देवनागरी"`), stringCost("देवनागरी"))
}

// TestFailingMarshallerInsideTheWalkFailsClosed: a marshaller that errors
// during the walk takes the ordinary error path, as json.Marshal would.
func TestFailingMarshallerInsideTheWalkFailsClosed(t *testing.T) {
	type withBoom struct {
		Name string      `json:"name" helivantalog:"phi"`
		Bad  sizeBoomish `json:"bad"`
	}
	_, _, err := renderBound(withBoom{Name: "x"}, maxPHIMarshalBytes)
	require.Error(t, err)
	require.False(t, errors.Is(err, errRenderOversize))

	masked, _, ok := maskPHIValue(withBoom{Name: "x"})
	require.True(t, ok)
	require.Equal(t, phiMarkerJSON, string(masked))
}

type sizeBoomish struct{}

func (sizeBoomish) MarshalJSON() ([]byte, error) { return nil, errors.New("boom") }
