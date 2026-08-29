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
	PatientID string
	Name      string
	DOB       string // ISO date; compared for equality only
	// DOBEstimated marks a date of birth that was approximated (commonly
	// to 1 January) rather than recorded from a document. Thousands of
	// patients share an approximated 01-01 in Indian practice, so an
	// agreement between two estimated dates is much weaker evidence than
	// an agreement between two recorded ones — see dobEstimatedCredit.
	DOBEstimated bool
	Mobile       string
	ABHANumber   string
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
	weightName   = 0.60
	weightDOB    = 0.25
	weightMobile = 0.15
	// phoneticCredit is the similarity attributed when phonetic keys
	// agree but the normalised spellings do not. It is deliberately not
	// enough on its own: 0.85 * weightName = 0.51, short of the Possible
	// floor (0.55) with nothing else corroborating. Two people who merely
	// sound alike, sharing no date of birth or mobile number, are
	// probably different people; the discount only helps once another
	// field agrees too. See TestPhoneticOnlyMatchAloneIsNotPossible.
	phoneticCredit = 0.85
	// dobEstimatedCredit is a judgement call, not a measurement: half
	// credit for a date-of-birth agreement where either side is an
	// approximation. It exists because thousands of Indian patients
	// share an approximated 01-01, and crediting that agreement as full
	// evidence is exactly the false-merge pressure the dob_estimated
	// column was added to relieve. Tune against real data once it
	// exists; do not treat 0.5 as validated.
	dobEstimatedCredit = 0.5
)

// Score compares two people.
//
// Two deterministic rules short-circuit to Confident before anything is
// scored (spec D3). A matching ABHA number wins regardless of
// demographics: it is a verified national identifier, and a corrected
// date of birth against the same ABHA is the same human. Agreement on
// all three of normalised name, mobile and date of birth wins too — see
// the comment at that rule for why it ignores DOBEstimated. Everything
// else is scored.
func Score(subject, candidate Candidate, t Thresholds) Result {
	if subject.ABHANumber != "" && subject.ABHANumber == candidate.ABHANumber {
		return Result{Candidate: candidate, Score: 1, Band: BandConfident, Deterministic: true}
	}

	// Spec D3's second deterministic rule: name AND mobile AND date of
	// birth all agreeing is the same person, and it short-circuits
	// BEFORE the probabilistic path so that no component weight or
	// discount can veto a total agreement.
	//
	// It must ignore DOBEstimated, and that is the whole point of it
	// existing. Dates of birth are routinely approximated to 1 January
	// here, so without this rule a self-duplicate with an exact name, an
	// exact mobile and an exact-but-estimated date scores
	// 1.0*0.60 + 0.5*0.25 + 1.0*0.15 = 0.875 — Possible, not blocked —
	// and the block is unreachable for a large share of the population.
	// dobEstimatedCredit exists to stop an approximated date carrying a
	// PARTIAL match over the line (see TestSharedEstimatedDOBIsNotConfident,
	// where the names differ and this rule correctly does not fire); it
	// was never meant to veto a total one.
	//
	// All three are required, and all three must be non-empty: a rule
	// that fired on two blank mobiles would make every same-name
	// same-date pair a block.
	subjectName, candidateName := NormalizeName(subject.Name), NormalizeName(candidate.Name)
	if subjectName != "" && subjectName == candidateName &&
		subject.Mobile != "" && subject.Mobile == candidate.Mobile &&
		subject.DOB != "" && subject.DOB == candidate.DOB {
		return Result{Candidate: candidate, Score: 1, Band: BandConfident, Deterministic: true}
	}

	nameSim := smetrics.JaroWinkler(
		subjectName, candidateName,
		jaroWinklerBoost, jaroWinklerPrefix)
	// Sounds-alike names that survive normalisation still count, but at a
	// discount — a phonetic collision is weaker evidence than a spelling
	// match, and treating them equally is how unrelated people merge. The
	// discount is deliberately not sufficient by itself: see
	// phoneticCredit's comment and TestPhoneticOnlyMatchAloneIsNotPossible.
	if nameSim < phoneticCredit && PhoneticKey(subject.Name) == PhoneticKey(candidate.Name) {
		nameSim = phoneticCredit
	}

	var dobSim float64
	if subject.DOB != "" && subject.DOB == candidate.DOB {
		if subject.DOBEstimated || candidate.DOBEstimated {
			dobSim = dobEstimatedCredit
		} else {
			dobSim = 1
		}
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
