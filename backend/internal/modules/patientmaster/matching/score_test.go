package matching_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/patientmaster/matching"
)

func subject() matching.Candidate {
	return matching.Candidate{Name: "Md Ali", DOB: "1979-04-02", Mobile: "9876543210"}
}

// TestABHAMatchIsConfidentAndDeterministic: a matching ABHA number is
// the strongest anchor there is. It must not depend on demographics
// agreeing — a corrected date of birth on the same ABHA is still the
// same person.
func TestABHAMatchIsConfidentAndDeterministic(t *testing.T) {
	s := subject()
	s.ABHANumber = "12345678901234"
	c := matching.Candidate{PatientID: "p1", Name: "Totally Different", DOB: "1955-01-01",
		ABHANumber: "12345678901234"}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
	require.True(t, got.Deterministic)
}

// TestSpellingVariantSameDOBAndMobileIsConfident is the case #70 was
// written about: the same human registering twice with the name spelled
// differently.
func TestSpellingVariantSameDOBAndMobileIsConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Mohammed Ali",
		DOB: "1979-04-02", Mobile: "9876543210"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
}

// TestSharedHouseholdMobileWithDifferentNameIsNotConfident: one mobile
// serves a whole family. Mobile alone must never reach the blocking
// band, or a wife is blocked from registering because her husband
// already exists.
func TestSharedHouseholdMobileWithDifferentNameIsNotConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Fatima Begum",
		DOB: "1985-11-20", Mobile: "9876543210"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.NotEqual(t, matching.BandConfident, got.Band)
}

// TestDifferentPersonIsNoMatch guards the false-merge direction, which
// is the one that causes clinical harm.
func TestDifferentPersonIsNoMatch(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Priya Nair",
		DOB: "1996-08-14", Mobile: "9000000000"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.Equal(t, matching.BandNoMatch, got.Band)
}

// TestSameNameDifferentDOBIsPossibleNotConfident: common names collide.
// Blocking on the name alone would stop unrelated people registering.
func TestSameNameDifferentDOBIsPossibleNotConfident(t *testing.T) {
	c := matching.Candidate{PatientID: "p1", Name: "Mohammed Ali",
		DOB: "1962-01-01", Mobile: "9111111111"}

	got := matching.Score(subject(), c, matching.DefaultThresholds())
	require.Equal(t, matching.BandPossible, got.Band,
		"the look-alike must be OFFERED, not hidden: NotEqual(Confident) would also "+
			"pass on a regression to NoMatch, which is the difference between the "+
			"clerk seeing the existing record and never hearing about it")
}

// TestEstimatedDOBSelfDuplicateStillBlocks is spec D3's second
// deterministic rule, and the reason it has to exist. The same human
// re-registering with the same name, the same mobile and the same date
// of birth — a date marked estimated, as it routinely is when a patient
// gives 1 January — scores 1.0*0.60 + 0.5*0.25 + 1.0*0.15 = 0.875 under
// the probabilistic path alone: Possible, not blocked. That makes the
// block unreachable for a large share of Indian registrations on an
// otherwise perfect match, so the deterministic rule must fire first and
// ignore the estimated flag.
func TestEstimatedDOBSelfDuplicateStillBlocks(t *testing.T) {
	s := matching.Candidate{Name: "Ravi Kumar", DOB: "1980-01-01", DOBEstimated: true, Mobile: "9876500011"}
	c := matching.Candidate{PatientID: "p1", Name: "Ravi Kumar", DOB: "1980-01-01", DOBEstimated: true, Mobile: "9876500011"}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
	require.True(t, got.Deterministic)
}

// TestDeterministicRuleRequiresAllThree pins the rule's narrowness from
// the other side: drop any one of the three agreements and it must not
// fire. Without this, "all three agree" could quietly decay into "two
// agree", which is the false-merge direction.
func TestDeterministicRuleRequiresAllThree(t *testing.T) {
	base := matching.Candidate{Name: "Ravi Kumar", DOB: "1980-01-01", DOBEstimated: true, Mobile: "9876500011"}

	noMobile := base
	noMobile.Mobile = ""
	got := matching.Score(noMobile, matching.Candidate{PatientID: "p1", Name: "Ravi Kumar",
		DOB: "1980-01-01", DOBEstimated: true}, matching.DefaultThresholds())
	require.False(t, got.Deterministic, "two blank mobiles must not count as agreement")
	require.NotEqual(t, matching.BandConfident, got.Band)

	otherDOB := matching.Candidate{PatientID: "p1", Name: "Ravi Kumar",
		DOB: "1975-06-02", Mobile: "9876500011"}
	got = matching.Score(base, otherDOB, matching.DefaultThresholds())
	require.False(t, got.Deterministic, "a differing date of birth must not fire the rule")
}

// TestRankOrdersByScoreDescending — the clerk sees the best candidate first.
func TestRankOrdersByScoreDescending(t *testing.T) {
	corpus := []matching.Candidate{
		{PatientID: "far", Name: "Priya Nair", DOB: "1996-08-14"},
		{PatientID: "near", Name: "Mohammed Ali", DOB: "1979-04-02", Mobile: "9876543210"},
	}
	got := matching.Rank(subject(), corpus, matching.DefaultThresholds())
	require.Len(t, got, 2)
	require.Equal(t, "near", got[0].Candidate.PatientID)
	require.GreaterOrEqual(t, got[0].Score, got[1].Score)
}

// TestPhoneticOnlyMatchWithDOBAndMobileReachesConfident actually exercises
// the phoneticCredit branch: "Farooq" and "Farukh" normalise to different
// strings (no honorific or expansion collapses them), so Jaro-Winkler on
// the normalised forms alone is 0.6667 — well below phoneticCredit. But
// both reduce to the same Soundex code per token (F620), so PhoneticKey
// agrees and the branch fires, crediting 0.85 instead.
//
// The corroborating DOB and mobile agreement is what makes the credit
// visible in the band: with the credit, the score is
// 0.85*0.60 + 1*0.25 + 1*0.15 = 0.91 (Confident). Without the branch —
// see the mutation proof in the task report — the raw 0.6667 similarity
// would only reach 0.6667*0.60 + 0.25 + 0.15 = 0.80 (Possible), so this
// test only passes because the branch actually ran.
func TestPhoneticOnlyMatchWithDOBAndMobileReachesConfident(t *testing.T) {
	require.Equal(t, matching.PhoneticKey("Farooq"), matching.PhoneticKey("Farukh"),
		"fixture assumption: the chosen pair must share a phonetic key")
	require.NotEqual(t, matching.NormalizeName("Farooq"), matching.NormalizeName("Farukh"),
		"fixture assumption: the pair must NOT be unified by NormalizeName, or this would not exercise the phonetic branch")

	s := matching.Candidate{Name: "Farooq", DOB: "1988-03-15", Mobile: "9876500000"}
	c := matching.Candidate{PatientID: "p1", Name: "Farukh", DOB: "1988-03-15", Mobile: "9876500000"}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.Equal(t, matching.BandConfident, got.Band)
}

// TestPhoneticOnlyMatchAloneIsNotPossible pins the deliberate half of the
// phonetic-credit design: a sounds-alike name with no corroborating date
// of birth or mobile number must not even clear the Possible floor.
// 0.85 (credit) * 0.60 (weightName) = 0.51, short of the 0.55 floor by
// design — two people who merely sound alike are probably different
// people until something else agrees.
func TestPhoneticOnlyMatchAloneIsNotPossible(t *testing.T) {
	s := matching.Candidate{Name: "Farooq", DOB: "1988-03-15", Mobile: "9876500000"}
	c := matching.Candidate{PatientID: "p1", Name: "Farukh", DOB: "1970-01-01", Mobile: "9111100000"}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.NotEqual(t, matching.BandPossible, got.Band)
	require.NotEqual(t, matching.BandConfident, got.Band)
}

// TestSharedEstimatedDOBIsNotConfident guards the false-merge direction
// the dob_estimated column exists to relieve: thousands of Indian
// patients carry an approximated 01-01 date of birth. Two people with
// different names who both happen to carry the same estimated DOB must
// not be pushed toward the blocking band by that agreement — an
// estimated match earns only half of DOB's weight, not full credit.
func TestSharedEstimatedDOBIsNotConfident(t *testing.T) {
	s := matching.Candidate{Name: "Ramesh Yadav", DOB: "1900-01-01", DOBEstimated: true}
	c := matching.Candidate{PatientID: "p1", Name: "Suresh Kumar", DOB: "1900-01-01", DOBEstimated: true}

	got := matching.Score(s, c, matching.DefaultThresholds())
	require.NotEqual(t, matching.BandConfident, got.Band)
	require.NotEqual(t, matching.BandPossible, got.Band)
	require.False(t, got.Deterministic,
		"the all-three-agree short-circuit must NOT fire here: the names differ, "+
			"so a shared approximated date of birth is still weak evidence")
}
