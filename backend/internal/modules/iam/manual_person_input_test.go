package iam

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func manualSourceInputFixture() ManualIdentitySourceImport {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	return ManualIdentitySourceImport{TenantID: uuid.MustParse("11111111-1111-7111-8111-111111111111"), SourceProfileID: uuid.Must(uuid.NewV7()), PolicyReference: uuid.Must(uuid.NewV7()), ReviewReference: uuid.Must(uuid.NewV7()), WitnessReference: uuid.Must(uuid.NewV7()), DisplayName: "Synthetic manual identity source", ObservedAt: now.Add(-time.Hour), ValidFrom: now, ValidUntil: now.AddDate(1, 0, 0), IndependentRootReviewConfirmed: true, Roots: []ManualIdentityRootPerson{{PersonID: uuid.Must(uuid.NewV7()), SourcePersonReference: uuid.Must(uuid.NewV7()), FindingReference: uuid.Must(uuid.NewV7()), Accounts: []ManualIdentityRootAccount{{Subject: "synthetic-examiner", ExpectedMembershipEpoch: 1}}}, {PersonID: uuid.Must(uuid.NewV7()), SourcePersonReference: uuid.Must(uuid.NewV7()), FindingReference: uuid.Must(uuid.NewV7()), Accounts: []ManualIdentityRootAccount{{Subject: "synthetic-reviewer", ExpectedMembershipEpoch: 1}}}}, Reason: "Synthetic independently completed original-evidence review"}
}
func TestManualIdentitySourceRequiresIndependentExplicitRootEvidence(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*ManualIdentitySourceImport)
		valid  bool
	}{
		{"valid manual source", func(*ManualIdentitySourceImport) {}, true},
		{"one root person", func(in *ManualIdentitySourceImport) { in.Roots = in.Roots[:1] }, false},
		{"same person two accounts", func(in *ManualIdentitySourceImport) { in.Roots[1].PersonID = in.Roots[0].PersonID }, false},
		{"same source person two IDs", func(in *ManualIdentitySourceImport) {
			in.Roots[1].SourcePersonReference = in.Roots[0].SourcePersonReference
		}, false},
		{"same account two people", func(in *ManualIdentitySourceImport) {
			in.Roots[1].Accounts[0].Subject = in.Roots[0].Accounts[0].Subject
		}, false},
		{"missing source review", func(in *ManualIdentitySourceImport) { in.ReviewReference = uuid.Nil }, false},
		{"missing witness", func(in *ManualIdentitySourceImport) { in.WitnessReference = uuid.Nil }, false},
		{"unconfirmed roots", func(in *ManualIdentitySourceImport) { in.IndependentRootReviewConfirmed = false }, false},
		{"future observation", func(in *ManualIdentitySourceImport) { in.ObservedAt = now.Add(time.Second) }, false},
		{"expired source", func(in *ManualIdentitySourceImport) { in.ValidUntil = now }, false},
		{"reversed validity", func(in *ManualIdentitySourceImport) { in.ValidUntil = in.ValidFrom.Add(-time.Hour) }, false},
		{"unknown incarnation", func(in *ManualIdentitySourceImport) { in.Roots[0].Accounts[0].ExpectedMembershipEpoch = 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := manualSourceInputFixture()
			tc.change(&in)
			err := validateManualIdentitySource(in, now)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestManualIdentityReviewRequiresDifferentCurrentPeople(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	profile := uuid.Must(uuid.NewV7())
	examiner := uuid.Must(uuid.NewV7())
	reviewer := uuid.Must(uuid.NewV7())
	target := uuid.Must(uuid.NewV7())
	valid := manualPersonActor{ProfileID: profile, ProfileVersion: 1, PersonID: reviewer, PersonVersion: 1, Subject: "reviewer-one", MembershipEpoch: 1, ValidUntil: now.Add(time.Hour)}
	require.NoError(t, manualIdentityIndependentReview(valid, profile, examiner, target, now))
	alias := valid
	alias.Subject = "reviewer-second-account"
	alias.PersonID = examiner
	require.ErrorIs(t, manualIdentityIndependentReview(alias, profile, examiner, target, now), errManualIdentityIndependence)
	self := valid
	self.PersonID = target
	require.ErrorIs(t, manualIdentityIndependentReview(self, profile, examiner, target, now), errManualIdentityIndependence)
	stale := valid
	stale.ValidUntil = now
	require.Error(t, manualIdentityIndependentReview(stale, profile, examiner, target, now))
	wrong := valid
	wrong.ProfileID = uuid.Must(uuid.NewV7())
	require.Error(t, manualIdentityIndependentReview(wrong, profile, examiner, target, now))
}
