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

	"github.com/tesserix/helivanta/pkg/logging"
)

// --- duplicate emitted JSON names -------------------------------------------
//
// encoding/json resolves two fields competing for one JSON name by depth, and
// at equal depth by which one takes its name from a json tag. A path layer that
// binds the emitted key to the losing field computes a path that never matches,
// and the winner goes out in plaintext. It is declaration-order dependent,
// which is the same order-dependence class that sank the withdrawn design.

type tagOnly struct {
	Name string `json:"name" helivantalog:"phi"`
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

// --- tag names encoding/json rejects, competing with a real key -------------
//
// Go 1.26's encoding/json rejects some tag names and falls back to the Go field
// name — `नाम` and `पता` are rejected while `aé`, `имя` and `名前` are accepted.
// A Hindi-named field on a patient DTO is an entirely realistic thing to write
// in an Indian hospital system, so this is not an exotic trigger. When such a
// field then competes with another for the Go field name, the emitted key must
// still be bound to whichever field encoding/json binds it to.

type shallowInner struct{ Data string }

type rejectedTagOuter struct {
	shallowInner
	// encoding/json rejects "नाम" and emits this field as "Data" at depth 0,
	// which hides shallowInner.Data at depth 1.
	Data string `json:"नाम" helivantalog:"phi"`
}

type rejectedTagOverMask struct {
	// Rejected tag: this would be emitted as "Alias" at depth 0 — except that
	// Other claims "Alias" from a tag, so encoding/json drops this field.
	Alias string `json:"नाम" helivantalog:"phi"`
	Other string `json:"Alias"`
}

func TestRejectedTagNameCompetingWithADeeperRealKeyStillMasks(t *testing.T) {
	v := rejectedTagOuter{shallowInner: shallowInner{Data: "public"}, Data: secretName}
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	require.Contains(t, string(raw), secretName, "fixture must actually emit the tagged field")

	line := logged(t, "v", v)
	require.NotContains(t, line, secretName,
		"encoding/json renders %s at depth 0; the mask must follow it there", raw)
	require.Contains(t, line, `"v":{"Data":"`+phiMarker+`"}`, line)
}

func TestRejectedTagNameLosingToATaggedSiblingDoesNotOverMask(t *testing.T) {
	v := rejectedTagOverMask{Alias: secretName, Other: "PUBLIC"}
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	require.JSONEq(t, `{"Alias":"PUBLIC"}`, string(raw),
		"encoding/json drops the rejected-tag field: the tagged sibling owns this key")

	line := logged(t, "v", v)
	require.NotContains(t, line, secretName)
	require.Contains(t, line, `"Alias":"PUBLIC"`,
		"the key belongs to the untagged sibling and must not be masked: %s", line)
}

// --- M1: over-masking must not destroy an untagged sibling ------------------

type overMaskSibling struct {
	Alias string `json:"whatever" helivantalog:"phi"`
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
	Note string `json:"Data" helivantalog:"phi"`
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
// fuzzJSONNames deliberately includes names encoding/json REJECTS ("नाम",
// "पता", an emoji) alongside ones it accepts. Their absence is why the first
// version of this fuzz could not see the class it was written to guard: a
// rejected tag name is the entire reason name resolution has anything to
// decide, so a pool without one tests only the easy half.
var (
	fuzzGoNames   = []string{"Data", "Alias", "Rec", "Extra", "Name"}
	fuzzJSONNames = []string{"", "", "Data", "Alias", "name", "alpha", "-", "Extra", "नाम", "पता", "🙂", "aé"}
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
		tag = strings.TrimSpace(tag + ` helivantalog:"phi"`)
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

	leaks, overMasks, exercised := 0, 0, 0
	var firstLeak, firstOverMask string
	for i := range iterations {
		typ, secretPath, ok := buildFuzzStruct(rng)
		if !ok {
			continue
		}
		val := reflect.New(typ).Elem()
		if !plantSecret(val, secretPath) {
			continue
		}
		// Every other string position gets a public sentinel, so the fuzz
		// measures both directions: a leak is the secret surviving, an
		// over-mask is a sentinel disappearing. Only checking for leaks would
		// pass a redactor that masked the entire record.
		fillPublic(val)
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
			continue
		}
		if want, got := bytes.Count(raw, []byte(publicSentinel)), strings.Count(line, publicSentinel); want != got {
			overMasks++
			if firstOverMask == "" {
				firstOverMask = fmt.Sprintf("iteration %d (%d public values in, %d out)\n  type   %s\n  json   %s\n  logged %s",
					i, want, got, typ, raw, strings.TrimSpace(line))
			}
		}
	}
	require.Positive(t, exercised, "the generator must actually emit the secret somewhere")
	require.Zero(t, leaks, "%d of %d exercised shapes leaked; first:\n%s", leaks, exercised, firstLeak)
	require.Zero(t, overMasks, "%d of %d exercised shapes lost an untagged public value; first:\n%s",
		overMasks, exercised, firstOverMask)
	t.Logf("%d generated shapes emitted the secret; all masked, no untagged value lost", exercised)
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

const publicSentinel = "PUBLIC-VALUE-KEEP"

// fillPublic gives every still-empty string position a sentinel, so the fuzz
// can tell "masked correctly" from "masked everything".
func fillPublic(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() && v.String() == "" {
			v.SetString(publicSentinel)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			// Skip tagged positions: those are supposed to disappear, and
			// filling them would make correct masking look like data loss.
			if v.Type().Field(i).Tag.Get("helivantalog") == "phi" {
				continue
			}
			if v.Field(i).CanSet() {
				fillPublic(v.Field(i))
			}
		}
	case reflect.Pointer:
		if !v.IsNil() {
			fillPublic(v.Elem())
		}
	case reflect.Slice:
		for i := range v.Len() {
			fillPublic(v.Index(i))
		}
	}
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
