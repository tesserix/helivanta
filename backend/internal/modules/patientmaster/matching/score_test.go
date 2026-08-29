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
	require.NotEqual(t, matching.BandConfident, got.Band)
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
