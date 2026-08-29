package matching

import (
	"sort"

	"github.com/xrash/smetrics"
)

// Band is the outcome of comparing two people, and the input to the
// registration policy: Confident blocks, Possible offers candidates,
// NoMatch proceeds.
type Band int

const (
	BandNoMatch Band = iota
	BandPossible
	BandConfident
)

// Candidate is the comparable projection of a person. Deliberately flat
// strings: this package must be runnable over a corpus fixture with no
// database types in scope.
type Candidate struct {
	PatientID  string
	Name       string
	DOB        string // ISO date; compared for equality only
	Mobile     string
	ABHANumber string
}

// Thresholds are configuration, not constants. They will be tuned
// against real data, and a threshold that needs a code change to tune
// does not get tuned.
type Thresholds struct {
	Possible  float64
	Confident float64
}

// DefaultThresholds are a starting point, not a tuned answer. The spec
// records that no real corpus exists yet; these values are chosen so the
// documented cases land in the right bands and nothing more is claimed.
func DefaultThresholds() Thresholds {
	return Thresholds{Possible: 0.55, Confident: 0.90}
}

// Result is one comparison.
type Result struct {
	Candidate     Candidate
	Score         float64
	Band          Band
	Deterministic bool
}

// jaroWinklerBoost/prefix are smetrics' standard parameters: boost
// strings sharing a prefix of up to 4 once similarity passes 0.7.
const (
	jaroWinklerBoost  = 0.7
	jaroWinklerPrefix = 4
)

// Component weights. Name dominates because it is the only field that is
// both always present and genuinely discriminating: dates of birth are
// approximated to 1 January, and one mobile serves a household — so
// neither can carry the decision alone. See the shared-household test.
const (
	weightName     = 0.60
	weightDOB      = 0.25
	weightMobile   = 0.15
	phoneticCredit = 0.85 // similarity attributed when phonetic keys agree
)

// Score compares two people.
//
// A matching ABHA number short-circuits to Confident regardless of
// demographics: it is a verified national identifier, and a corrected
// date of birth against the same ABHA is the same human. Everything else
// is scored.
func Score(subject, candidate Candidate, t Thresholds) Result {
	if subject.ABHANumber != "" && subject.ABHANumber == candidate.ABHANumber {
		return Result{Candidate: candidate, Score: 1, Band: BandConfident, Deterministic: true}
	}

	nameSim := smetrics.JaroWinkler(
		NormalizeName(subject.Name), NormalizeName(candidate.Name),
		jaroWinklerBoost, jaroWinklerPrefix)
	// Sounds-alike names that survive normalisation still count, but at a
	// discount — a phonetic collision is weaker evidence than a spelling
	// match, and treating them equally is how unrelated people merge.
	if nameSim < phoneticCredit && PhoneticKey(subject.Name) == PhoneticKey(candidate.Name) {
		nameSim = phoneticCredit
	}

	var dobSim float64
	if subject.DOB != "" && subject.DOB == candidate.DOB {
		dobSim = 1
	}
	var mobileSim float64
	if subject.Mobile != "" && subject.Mobile == candidate.Mobile {
		mobileSim = 1
	}

	score := nameSim*weightName + dobSim*weightDOB + mobileSim*weightMobile

	band := BandNoMatch
	switch {
	case score >= t.Confident:
		band = BandConfident
	case score >= t.Possible:
		band = BandPossible
	}
	return Result{Candidate: candidate, Score: score, Band: band}
}

// Rank scores a corpus and returns it best-first, so the clerk sees the
// most likely existing record at the top.
func Rank(subject Candidate, corpus []Candidate, t Thresholds) []Result {
	out := make([]Result, 0, len(corpus))
	for _, c := range corpus {
		out = append(out, Score(subject, c, t))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
