package iam

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func manualIdentityVersion(v int64) *int64 { return &v }

func TestManualIdentityCaseRequiresConsentAndExactProfile(t *testing.T) {
	valid := ManualIdentityCaseInput{SourceProfileID: uuid.Must(uuid.NewV7()), ExpectedProfileVersion: manualIdentityVersion(1), ConsentAcknowledged: true, Reason: "Identity assurance for this account"}
	for _, tc := range []struct {
		name   string
		change func(*ManualIdentityCaseInput)
	}{
		{"missing consent", func(in *ManualIdentityCaseInput) { in.ConsentAcknowledged = false }},
		{"missing version", func(in *ManualIdentityCaseInput) { in.ExpectedProfileVersion = nil }},
		{"zero version", func(in *ManualIdentityCaseInput) { in.ExpectedProfileVersion = manualIdentityVersion(0) }},
		{"overflow version", func(in *ManualIdentityCaseInput) {
			in.ExpectedProfileVersion = manualIdentityVersion(manualIdentityMaxVersion + 1)
		}},
		{"missing source", func(in *ManualIdentityCaseInput) { in.SourceProfileID = uuid.Nil }},
		{"blank reason", func(in *ManualIdentityCaseInput) { in.Reason = "\u00a0" }},
		{"control character", func(in *ManualIdentityCaseInput) { in.Reason = "Identity\nassurance" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			require.ErrorIs(t, validateManualIdentityCase(in), errManualIdentityInput)
		})
	}
	require.NoError(t, validateManualIdentityCase(valid))
}

func manualExaminationFixture(now time.Time) ManualIdentityExaminationInput {
	return ManualIdentityExaminationInput{ExpectedCaseVersion: manualIdentityVersion(1), PersonID: uuid.Must(uuid.NewV7()), ExpectedPersonVersion: manualIdentityVersion(0), SourcePersonReference: uuid.Must(uuid.NewV7()), SourceReference: uuid.Must(uuid.NewV7()), FindingReference: uuid.Must(uuid.NewV7()), ObservedAt: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), OriginalEvidenceExamined: true, KnownAssociationsChecked: true, Reason: "Original identity evidence examined and associations checked"}
}

func TestManualIdentityExaminationRequiresCurrentOriginalEvidence(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	valid := manualExaminationFixture(now)
	for _, tc := range []struct {
		name   string
		change func(*ManualIdentityExaminationInput)
	}{
		{"missing case version", func(in *ManualIdentityExaminationInput) { in.ExpectedCaseVersion = nil }},
		{"missing person version", func(in *ManualIdentityExaminationInput) { in.ExpectedPersonVersion = nil }},
		{"negative person version", func(in *ManualIdentityExaminationInput) { in.ExpectedPersonVersion = manualIdentityVersion(-1) }},
		{"missing person", func(in *ManualIdentityExaminationInput) { in.PersonID = uuid.Nil }},
		{"missing source person", func(in *ManualIdentityExaminationInput) { in.SourcePersonReference = uuid.Nil }},
		{"missing evidence", func(in *ManualIdentityExaminationInput) { in.SourceReference = uuid.Nil }},
		{"missing finding", func(in *ManualIdentityExaminationInput) { in.FindingReference = uuid.Nil }},
		{"unexamined original", func(in *ManualIdentityExaminationInput) { in.OriginalEvidenceExamined = false }},
		{"unchecked associations", func(in *ManualIdentityExaminationInput) { in.KnownAssociationsChecked = false }},
		{"future observation", func(in *ManualIdentityExaminationInput) { in.ObservedAt = now.Add(time.Second) }},
		{"expired evidence", func(in *ManualIdentityExaminationInput) { in.ValidUntil = now }},
		{"missing observation", func(in *ManualIdentityExaminationInput) { in.ObservedAt = time.Time{} }},
		{"blank reason", func(in *ManualIdentityExaminationInput) { in.Reason = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			require.ErrorIs(t, validateManualIdentityExamination(in, now), errManualIdentityInput)
		})
	}
	require.NoError(t, validateManualIdentityExamination(valid, now))
	valid.ExpectedPersonVersion = manualIdentityVersion(2)
	require.NoError(t, validateManualIdentityExamination(valid, now))
}

func TestManualIdentityDecisionRequiresEvidenceAndExplicitOutcome(t *testing.T) {
	valid := ManualIdentityDecisionInput{ExpectedCaseVersion: manualIdentityVersion(2), ExaminationID: uuid.Must(uuid.NewV7()), Decision: "approve", EvidenceExamined: true, IndependentPersonConfirmed: true, Reason: "Independent original-evidence finding"}
	for _, tc := range []struct {
		name   string
		change func(*ManualIdentityDecisionInput)
	}{
		{"missing version", func(in *ManualIdentityDecisionInput) { in.ExpectedCaseVersion = nil }},
		{"zero version", func(in *ManualIdentityDecisionInput) { in.ExpectedCaseVersion = manualIdentityVersion(0) }},
		{"missing evidence revision", func(in *ManualIdentityDecisionInput) { in.ExaminationID = uuid.Nil }},
		{"implicit outcome", func(in *ManualIdentityDecisionInput) { in.Decision = "" }},
		{"unknown outcome", func(in *ManualIdentityDecisionInput) { in.Decision = "accept" }},
		{"unexamined evidence", func(in *ManualIdentityDecisionInput) { in.EvidenceExamined = false }},
		{"unconfirmed independence", func(in *ManualIdentityDecisionInput) { in.IndependentPersonConfirmed = false }},
		{"blank reason", func(in *ManualIdentityDecisionInput) { in.Reason = "\ufeff" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.change(&in)
			require.ErrorIs(t, validateManualIdentityDecision(in), errManualIdentityInput)
		})
	}
	require.NoError(t, validateManualIdentityDecision(valid))
	valid.Decision = "reject"
	require.NoError(t, validateManualIdentityDecision(valid))
}
