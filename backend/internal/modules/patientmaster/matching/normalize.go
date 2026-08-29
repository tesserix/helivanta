// Package matching decides whether two people are the same person.
//
// It is deliberately pure — no database, no clock, no HTTP — because
// the only way to trust it is to run it over a corpus of real name
// variants, and a package that needs a transaction to answer a question
// cannot be run that way.
package matching

import (
	"strings"
	"unicode"

	"github.com/xrash/smetrics"
)

// honorifics are titles that end up typed into the name field. Stored
// normalised (lowercase, no punctuation) because they are matched after
// the punctuation and case passes have run.
var honorifics = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "miss": true,
	"dr": true, "prof": true,
	"smt": true, "shri": true, "sri": true, "kum": true,
	"master": true, "baby": true,
}

// nameExpansions unify the abbreviations and spelling variants that
// cause duplicate records. Each entry here is one test case in
// normalize_test.go — the map and the corpus are meant to stay in step.
//
// This is a ruleset, not an algorithm: it is enumerable, reviewable, and
// every entry is justified by a failure mode named in #70. Resist adding
// speculative entries; an expansion that fires on a name it should not
// have touched merges two people.
var nameExpansions = map[string]string{
	"md":       "mohammed",
	"mohd":     "mohammed",
	"mohammad": "mohammed",
	"muhammad": "mohammed",
	"mohamed":  "mohammed",
}

// NormalizeName reduces a written name to a comparable form: lowercase,
// punctuation removed, whitespace collapsed, honorifics dropped, known
// abbreviations expanded.
//
// It is idempotent by construction — every rule maps into the normalised
// alphabet, so a second pass finds nothing left to change. TestNormalizeNameIsIdempotent
// is what keeps that true as rules are added.
func NormalizeName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			b.WriteRune(' ')
		}
	}

	out := make([]string, 0, 4)
	for _, tok := range strings.Fields(b.String()) {
		if honorifics[tok] {
			continue
		}
		if exp, ok := nameExpansions[tok]; ok {
			tok = exp
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}

// PhoneticKey encodes a name for sounds-alike comparison.
//
// Soundex is English-centric and does poorly on raw Indian
// transliterations — which is exactly why NormalizeName runs first. The
// normalisation collapses the Indian-specific variance, so Soundex here
// encodes a regularised form rather than the raw input. The ruleset does
// the domain work; the encoder only has to be stable.
//
// Per-token rather than whole-string so that a differing middle name or
// a reordered given/family name does not change every code.
func PhoneticKey(raw string) string {
	toks := strings.Fields(NormalizeName(raw))
	codes := make([]string, 0, len(toks))
	for _, t := range toks {
		codes = append(codes, smetrics.Soundex(t))
	}
	return strings.Join(codes, " ")
}
