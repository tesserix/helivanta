package logging_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/logging"
)

// --- duplicate emitted JSON names -------------------------------------------
//
// encoding/json resolves two fields competing for one JSON name by depth, and
// at equal depth by which one takes its name from a json tag. A path layer that
// binds the emitted key to the losing field computes a path that never matches,
// and the winner goes out in plaintext. It is declaration-order dependent,
// which is the same order-dependence class that sank the withdrawn design.

type tagOnly struct {
	Name string `json:"name" hmslog:"phi"`
}

type conflictTaggedSecond struct {
	Data  string
	Alias tagOnly `json:"Data"`
}

type conflictTaggedFirst struct {
	Alias tagOnly `json:"Data"`
	Data  string
}

type conflictViaGoNameFallback struct {
	Data string  `json:"alpha"`
	Rec  tagOnly `json:"Data"`
}

type embedOne struct{ Data string }
type embedTwo struct {
	Secret tagOnly `json:"Data"`
}
type conflictAcrossEmbeds struct {
	embedOne
	embedTwo
}

func TestDuplicateEmittedNamesResolveAsEncodingJSONDoes(t *testing.T) {
	// want is the exact rendering, not merely "the secret is absent". Failing
	// closed on an ambiguous name would also hide the secret — by replacing the
	// whole object with the marker — so only an exact expectation distinguishes
	// "mirrors encoding/json" from "gave up and masked everything".
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{
			"tagged field declared second",
			conflictTaggedSecond{Data: "public", Alias: tagOnly{Name: secretName}},
			`"v":{"Data":{"name":"` + phiMarker + `"}}`,
		},
		{
			"tagged field declared first",
			conflictTaggedFirst{Alias: tagOnly{Name: secretName}, Data: "public"},
			`"v":{"Data":{"name":"` + phiMarker + `"}}`,
		},
		{
			"go-name fallback outranks the real key",
			conflictViaGoNameFallback{Data: "public", Rec: tagOnly{Name: secretName}},
			`"v":{"alpha":"public","Data":{"name":"` + phiMarker + `"}}`,
		},
		{
			"conflict across two embedded structs",
			conflictAcrossEmbeds{embedOne{Data: "public"}, embedTwo{Secret: tagOnly{Name: secretName}}},
			`"v":{"Data":{"name":"` + phiMarker + `"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.v)
			require.NoError(t, err)
			line := logged(t, "v", tc.v)
			require.NotContains(t, line, secretName,
				"encoding/json renders %s; the mask must follow it", raw)
			require.Contains(t, line, tc.want,
				"encoding/json renders %s; the mask must bind to the same field, not mask the object wholesale", raw)
		})
	}
}

// --- M1: over-masking must not destroy an untagged sibling ------------------

type overMaskSibling struct {
	Alias string `json:"whatever" hmslog:"phi"`
	Other string `json:"Alias"`
}

func TestGoNameFallbackDoesNotMaskARealSiblingKey(t *testing.T) {
	line := logged(t, "v", overMaskSibling{Alias: secretName, Other: "KEEP-ME"})
	requireMasked(t, line)
	require.Contains(t, line, `"Alias":"KEEP-ME"`,
		"a fallback alias must never outrank the field that actually emits that key: %s", line)
}

// --- differential fuzz over generated shapes --------------------------------

// The fuzz is the mechanical guard against the next instance of this class: it
// generates structs whose fields deliberately compete for the same emitted JSON
// name, plants the secret in exactly one tagged position, and asserts the log
// line never carries it. reflect.StructOf is what makes it possible to explore
// declaration orders and tag combinations that no hand-written fixture set
// would cover. The seed is fixed so a failure is reproducible.

// Exported so reflect.StructOf can embed them: StructOf rejects an anonymous
// field whose name is unexported.
type FuzzEmbedPlain struct{ Data string }
type FuzzEmbedTagged struct {
	Alias tagOnly `json:"Data"`
}
type FuzzEmbedPHI struct {
	Note string `json:"Data" hmslog:"phi"`
}

type fuzzKind int

const (
	kindPlainString fuzzKind = iota
	kindPHIString
	kindPTStruct
	kindPTPointer
	kindPTSlice
	kindEmbedPlain
	kindEmbedTagged
	kindEmbedPHI
)

// secretKinds are the field shapes that can carry the planted secret; the rest
// exist to compete for emitted names.
var secretKinds = []fuzzKind{kindPHIString, kindPTStruct, kindPTPointer, kindPTSlice, kindEmbedTagged, kindEmbedPHI}
var allKinds = []fuzzKind{kindPlainString, kindPHIString, kindPTStruct, kindPTPointer, kindPTSlice, kindEmbedPlain, kindEmbedTagged, kindEmbedPHI}

// goNames and jsonNames overlap on purpose: a Go field name that equals another
// field's json tag is exactly the collision the Go-name fallback introduced,
// and "Data" is also the field name inside the embedded fixtures, so embedded
// and top-level candidates compete at different depths.
var (
	fuzzGoNames   = []string{"Data", "Alias", "Rec", "Extra", "Name"}
	fuzzJSONNames = []string{"", "", "Data", "Alias", "name", "alpha", "-", "Extra"}
)

func fuzzField(kind fuzzKind, goName, jsonTag string) (reflect.StructField, []int) {
	switch kind {
	case kindEmbedPlain:
		return reflect.StructField{Name: "FuzzEmbedPlain", Type: reflect.TypeOf(FuzzEmbedPlain{}), Anonymous: true}, nil
	case kindEmbedTagged:
		return reflect.StructField{Name: "FuzzEmbedTagged", Type: reflect.TypeOf(FuzzEmbedTagged{}), Anonymous: true}, []int{0, 0}
	case kindEmbedPHI:
		return reflect.StructField{Name: "FuzzEmbedPHI", Type: reflect.TypeOf(FuzzEmbedPHI{}), Anonymous: true}, []int{0}
	}

	sf := reflect.StructField{Name: goName}
	var rest []int
	tag := ""
	if jsonTag != "" {
		tag = `json:"` + jsonTag + `"`
	}
	switch kind {
	case kindPlainString:
		sf.Type = reflect.TypeOf("")
	case kindPHIString:
		sf.Type = reflect.TypeOf("")
		tag = strings.TrimSpace(tag + ` hmslog:"phi"`)
	case kindPTStruct:
		sf.Type = reflect.TypeOf(tagOnly{})
		rest = []int{0}
	case kindPTPointer:
		sf.Type = reflect.TypeOf(&tagOnly{})
		rest = []int{-1, 0}
	case kindPTSlice:
		sf.Type = reflect.TypeOf([]tagOnly{})
		rest = []int{-2, 0}
	}
	sf.Tag = reflect.StructTag(tag)
	return sf, rest
}

func TestFuzzDuplicateEmittedNamesNeverLeak(t *testing.T) {
	rng := rand.New(rand.NewSource(20260813))
	const iterations = 3000

	leaks, exercised := 0, 0
	var firstLeak string
	for i := range iterations {
		typ, secretPath, ok := buildFuzzStruct(rng)
		if !ok {
			continue
		}
		val := reflect.New(typ).Elem()
		if !plantSecret(val, secretPath) {
			continue
		}
		v := val.Interface()

		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		if !bytes.Contains(raw, []byte(secretName)) {
			// encoding/json dropped or shadowed the secret; there is nothing
			// for the mask to do and nothing that could leak.
			continue
		}
		exercised++

		var buf bytes.Buffer
		wrapped(&buf).Info("fuzz", "v", v)
		line := buf.String()
		if strings.Contains(line, secretName) {
			leaks++
			if firstLeak == "" {
				firstLeak = fmt.Sprintf("iteration %d\n  type   %s\n  json   %s\n  logged %s",
					i, typ, raw, strings.TrimSpace(line))
			}
		}
	}
	require.Positive(t, exercised, "the generator must actually emit the secret somewhere")
	require.Zero(t, leaks, "%d of %d exercised shapes leaked; first:\n%s", leaks, exercised, firstLeak)
	t.Logf("%d generated shapes emitted the secret and were all masked", exercised)
}

// buildFuzzStruct generates a struct type whose fields compete for emitted
// names, returning the field index path at which the secret should be planted.
func buildFuzzStruct(rng *rand.Rand) (reflect.Type, []int, bool) {
	n := 2 + rng.Intn(3)
	secretField := rng.Intn(n)

	fields := make([]reflect.StructField, 0, n)
	used := map[string]bool{}
	var secretPath []int

	for i := range n {
		kind := allKinds[rng.Intn(len(allKinds))]
		if i == secretField {
			kind = secretKinds[rng.Intn(len(secretKinds))]
		}
		sf, rest := fuzzField(kind,
			fuzzGoNames[rng.Intn(len(fuzzGoNames))],
			fuzzJSONNames[rng.Intn(len(fuzzJSONNames))])
		if used[sf.Name] {
			continue
		}
		used[sf.Name] = true
		if i == secretField {
			if rest == nil {
				return nil, nil, false
			}
			secretPath = append([]int{len(fields)}, rest...)
		}
		fields = append(fields, sf)
	}
	if len(secretPath) == 0 || len(fields) < 2 {
		return nil, nil, false
	}
	return reflect.StructOf(fields), secretPath, true
}

// plantSecret walks the generated index path, allocating pointers and slices as
// it goes, and writes the secret at the end. -1 means "through a pointer", -2
// means "into a one-element slice".
func plantSecret(v reflect.Value, path []int) bool {
	cur := v
	for _, step := range path {
		switch step {
		case -1:
			cur.Set(reflect.New(cur.Type().Elem()))
			cur = cur.Elem()
		case -2:
			cur.Set(reflect.MakeSlice(cur.Type(), 1, 1))
			cur = cur.Index(0)
		default:
			if cur.Kind() != reflect.Struct || step >= cur.NumField() {
				return false
			}
			cur = cur.Field(step)
		}
	}
	if cur.Kind() != reflect.String || !cur.CanSet() {
		return false
	}
	cur.SetString(secretName)
	return true
}

var _ = logging.RedactionCount
