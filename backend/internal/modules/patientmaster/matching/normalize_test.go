package matching_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
)

// TestNormalizeName is the ruleset, one case per rule. Each line is a
// real failure mode from #70: honorifics typed into the name field,
// Md/Mohd abbreviations, case and spacing noise, and punctuation.
func TestNormalizeName(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"lowercases", "SURESH", "suresh"},
		{"collapses whitespace", "  Suresh   Kumar ", "suresh kumar"},
		{"strips punctuation", "S.  Kumar-Rao", "s kumar rao"},
		{"drops the Mr honorific", "Mr Suresh Kumar", "suresh kumar"},
		{"drops the Smt honorific", "Smt. Lakshmi Devi", "lakshmi devi"},
		{"drops the Dr honorific", "Dr. Anil Sharma", "anil sharma"},
		{"expands Md", "Md Ali", "mohammed ali"},
		{"expands Mohd", "Mohd. Ali", "mohammed ali"},
		{"expands Mohammad spelling", "Mohammad Ali", "mohammed ali"},
		{"expands Muhammad spelling", "Muhammad Ali", "mohammed ali"},
		{"expands Mohamed spelling", "Mohamed Ali", "mohammed ali"},
		{"leaves Mohammed alone", "Mohammed Ali", "mohammed ali"},
		{"keeps an unknown name intact", "Priya Nair", "priya nair"},
		// Near-miss guard against over-eager expansion: "Mohan" shares a
		// prefix with the Mohammed variants above but is a different name
		// belonging to a different person. This is asserted at
		// NormalizeName rather than PhoneticKey because that is where an
		// over-eager rule would actually do the damage — an expansion
		// entry that fires on "mohan" rewrites the normalised string
		// itself, which is the direct cause of a false merge. PhoneticKey
		// only inherits whatever NormalizeName produces, so testing here
		// catches the defect at its source instead of through an extra
		// layer of Soundex behaviour.
		{"does not expand the Mohan near-miss", "Mohan Ali", "mohan ali"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, matching.NormalizeName(tc.in))
		})
	}
}

// TestNormalizeNameIsIdempotent: normalising an already-normalised name
// must change nothing. Without this a rule that rewrites its own output
// (an expansion that re-matches its result) loops or drifts silently.
func TestNormalizeNameIsIdempotent(t *testing.T) {
	for _, in := range []string{"Mr Md Ali", "Dr. Suresh  Kumar", "Priya Nair"} {
		once := matching.NormalizeName(in)
		require.Equal(t, once, matching.NormalizeName(once), "input %q", in)
	}
}

// TestPhoneticKeyMatchesAcrossSpellingVariants is why normalisation runs
// BEFORE the encoder: Soundex over the raw strings does not reliably
// unify these, over the normalised form it does.
func TestPhoneticKeyMatchesAcrossSpellingVariants(t *testing.T) {
	require.Equal(t, matching.PhoneticKey("Md Ali"), matching.PhoneticKey("Mohammed Ali"))
	require.Equal(t, matching.PhoneticKey("Mohammad Ali"), matching.PhoneticKey("Mohammed Ali"))
}

// TestPhoneticKeySeparatesDifferentPeople is the half that prevents a
// false merge. A phonetic key that collapses everything is worse than
// no key at all.
func TestPhoneticKeySeparatesDifferentPeople(t *testing.T) {
	require.NotEqual(t, matching.PhoneticKey("Suresh Kumar"), matching.PhoneticKey("Priya Nair"))
	require.NotEqual(t, matching.PhoneticKey("Mohammed Ali"), matching.PhoneticKey("Mohammed Akram"))
}
