package logging

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveFieldName's ambiguous branch is unobservable through the logger by
// construction: the shapes that reach it are exactly the ones encoding/json
// drops from its output, so nothing is emitted at that key to inspect. It is a
// backstop for the case where this package's mirror of encoding/json's
// conflict rule is wrong, and a backstop nobody asserts is a backstop nobody
// notices deleting. These tests drive it directly.
func TestResolveFieldName(t *testing.T) {
	str := reflect.TypeFor[string]()
	num := reflect.TypeFor[int]()

	for _, tc := range []struct {
		name  string
		in    []fieldCandidate
		want  fieldCandidate
		about string
	}{
		{
			name:  "single candidate",
			in:    []fieldCandidate{{name: "a", depth: 0, typ: str}},
			want:  fieldCandidate{name: "a", depth: 0, typ: str},
			about: "nothing to resolve",
		},
		{
			name: "shallower depth wins over a tagged deeper one",
			in: []fieldCandidate{
				{name: "a", depth: 1, typ: num, fromTag: true, phi: true},
				{name: "a", depth: 0, typ: str},
			},
			want:  fieldCandidate{name: "a", depth: 0, typ: str},
			about: "depth is decided before tag status, as in encoding/json",
		},
		{
			name: "exactly one tagged wins at equal depth",
			in: []fieldCandidate{
				{name: "a", depth: 0, typ: str},
				{name: "a", depth: 0, typ: num, fromTag: true, phi: true},
			},
			want:  fieldCandidate{name: "a", depth: 0, typ: num, fromTag: true, phi: true},
			about: "the Critical: keeping the first-seen candidate bound the key to the wrong type",
		},
		{
			name: "two tagged at equal depth with different types fails closed",
			in: []fieldCandidate{
				{name: "a", depth: 0, typ: str, fromTag: true},
				{name: "a", depth: 0, typ: num, fromTag: true, phi: true},
			},
			want:  fieldCandidate{name: "a", depth: 0, typ: nil, fromTag: true, phi: true},
			about: "encoding/json drops the name; if it did not, this must mask",
		},
		{
			name: "two tagged at equal depth, same phi but different types, fails closed",
			in: []fieldCandidate{
				{name: "a", depth: 0, typ: str, fromTag: true},
				{name: "a", depth: 0, typ: num, fromTag: true},
			},
			want: fieldCandidate{name: "a", depth: 0, typ: nil, fromTag: true, phi: true},
			about: "neither is tagged PHI, but one of the two types may contain PHI further down " +
				"and this function cannot tell which one encoding/json would have bound the key to",
		},
		{
			name: "no tagged candidate, differing phi, fails closed",
			in: []fieldCandidate{
				{name: "a", depth: 0, typ: str},
				{name: "a", depth: 0, typ: str, phi: true},
			},
			want:  fieldCandidate{name: "a", depth: 0, typ: nil, phi: true},
			about: "disagreement about PHI is resolved toward masking",
		},
		{
			name: "identical tied candidates need no fail-closed",
			in: []fieldCandidate{
				{name: "a", depth: 0, typ: str, phi: true},
				{name: "a", depth: 0, typ: str, phi: true},
			},
			want:  fieldCandidate{name: "a", depth: 0, typ: str, phi: true},
			about: "there is nothing to get wrong, so the type is preserved",
		},
		{
			name: "three candidates, one tagged at the shallowest depth",
			in: []fieldCandidate{
				{name: "a", depth: 2, typ: num, fromTag: true},
				{name: "a", depth: 1, typ: str, fromTag: true, phi: true},
				{name: "a", depth: 1, typ: str},
			},
			want:  fieldCandidate{name: "a", depth: 1, typ: str, fromTag: true, phi: true},
			about: "deeper candidates are discarded before the tag tie-break runs",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveFieldName(tc.in), tc.about)
		})
	}
}

// jsonEmittedName is the oracle that replaced guessing encoding/json's tag
// validity rule. These assertions are pinned to observed Go 1.26 behaviour; if
// a future Go changes which names it honours, the oracle follows it and only
// the expectations here need revisiting — which is the point of asking rather
// than mirroring.
func TestJSONEmittedName(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want string
	}{
		{"", ""},
		{",omitempty", ""},
		{"name", "name"},
		{"name,omitempty", "name"},
		{"-,", "-"},
		{"a<b>&c", "a<b>&c"},
		{"aé", "aé"},
		{"имя", "имя"},
		{"名前", "名前"},
		{"नाम", ""},
		{"पता", ""},
		{"🙂", ""},
		{"a b", "a b"},
		{`a"b`, ""},
		{`a\b`, ""},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			require.Equal(t, tc.want, jsonEmittedName(tc.tag))
			require.Equal(t, tc.want, jsonEmittedName(tc.tag), "cached answer must match")
		})
	}
}

// The oracle must agree with encoding/json on the actual struct, not just on
// its synthetic probe. This checks the two against each other directly.
func TestJSONEmittedNameAgreesWithEncodingJSON(t *testing.T) {
	for _, tag := range []string{"name", "नाम", "aé", "имя", "🙂", "a b", "-,", "alpha,omitempty", `a"b`} {
		t.Run(tag, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "RealField",
				Type: reflect.TypeFor[string](),
				// strconv.Quote, exactly as the oracle does: writing the tag
				// value in unescaped would produce a malformed struct tag for
				// any value containing a quote, and then this harness would be
				// testing its own bug rather than the oracle.
				Tag: reflect.StructTag("json:" + strconv.Quote(tag)),
			}})
			v := reflect.New(typ).Elem()
			v.Field(0).SetString("x")
			raw, err := marshalNoHTMLEscape(v.Interface())
			require.NoError(t, err)

			want := jsonEmittedName(tag)
			if want == "" {
				want = "RealField"
			}
			require.Contains(t, string(raw), `"`+want+`":`,
				"the oracle said %q but encoding/json emitted %s", want, raw)
		})
	}
}
