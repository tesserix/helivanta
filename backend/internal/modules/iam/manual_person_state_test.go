package iam

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManualIdentityDecisionBindsCurrentSourceCaseAndAllContributors(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	profile, target, examiner, earlierExaminer, reviewer, examination := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	source := manualIdentityProfile{ID: profile, Version: 2, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	actor := manualPersonActor{ProfileID: profile, ProfileVersion: 2, PersonID: reviewer, PersonVersion: 1, Subject: "reviewer", MembershipEpoch: 1, ValidUntil: now.Add(time.Hour)}
	caseRecord := ManualIdentityCase{ID: uuid.Must(uuid.NewV7()), SourceProfileID: profile, ProfileVersion: 2, Subject: "target", MembershipEpoch: 1, Version: 3, State: "awaiting_review", LatestExaminationID: &examination, ClinicalAuthority: "not_evaluated"}
	evidence := manualIdentityExamination{ID: examination, CaseID: caseRecord.ID, CaseVersion: caseRecord.Version, PersonID: target, ValidUntil: now.Add(time.Hour), Contributors: []uuid.UUID{earlierExaminer, examiner}}
	in := ManualIdentityDecisionInput{ExpectedCaseVersion: manualIdentityVersion(3), ExaminationID: examination, Decision: "approve", EvidenceExamined: true, IndependentPersonConfirmed: true, Reason: "Independent original evidence reviewed"}
	for _, tc := range []struct {
		name   string
		change func(*manualIdentityProfile, *manualPersonActor, *ManualIdentityCase, *manualIdentityExamination, *ManualIdentityDecisionInput)
	}{
		{"target account conflicting identity", func(_ *manualIdentityProfile, a *manualPersonActor, c *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			a.Subject = c.Subject
		}},
		{"other evidence case", func(_ *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, e *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			e.CaseID = uuid.Must(uuid.NewV7())
		}},
		{"old evidence case version", func(_ *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, e *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			e.CaseVersion--
		}},
		{"earlier contributor alternate account", func(_ *manualIdentityProfile, a *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			a.PersonID = earlierExaminer
		}},
		{"current contributor alternate account", func(_ *manualIdentityProfile, a *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			a.PersonID = examiner
		}},
		{"target alternate account", func(_ *manualIdentityProfile, a *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			a.PersonID = target
		}},
		{"revoked source", func(s *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			s.Revoked = true
		}},
		{"future source", func(s *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			s.ValidFrom = now.Add(time.Second)
		}},
		{"expired source", func(s *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			s.ValidUntil = now
		}},
		{"replaced profile", func(s *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			s.ID = uuid.Must(uuid.NewV7())
		}},
		{"stale profile version", func(s *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			s.Version++
		}},
		{"stale actor profile", func(_ *manualIdentityProfile, a *manualPersonActor, _ *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			a.ProfileVersion--
		}},
		{"stale case", func(_ *manualIdentityProfile, _ *manualPersonActor, c *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			c.Version++
		}},
		{"old evidence revision", func(_ *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, e *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			e.ID = uuid.Must(uuid.NewV7())
		}},
		{"expired evidence", func(_ *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, e *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			e.ValidUntil = now
		}},
		{"missing contributors", func(_ *manualIdentityProfile, _ *manualPersonActor, _ *ManualIdentityCase, e *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			e.Contributors = nil
		}},
		{"terminal approved", func(_ *manualIdentityProfile, _ *manualPersonActor, c *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			c.State = "approved"
		}},
		{"terminal rejected", func(_ *manualIdentityProfile, _ *manualPersonActor, c *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			c.State = "rejected"
		}},
		{"evidence not yet recorded", func(_ *manualIdentityProfile, _ *manualPersonActor, c *ManualIdentityCase, _ *manualIdentityExamination, _ *ManualIdentityDecisionInput) {
			c.State = "awaiting_evidence"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, a, c, e, input := source, actor, caseRecord, evidence, in
			tc.change(&s, &a, &c, &e, &input)
			_, err := manualIdentityDecisionState(s, a, c, e, input, now)
			require.Error(t, err)
		})
	}
	next, err := manualIdentityDecisionState(source, actor, caseRecord, evidence, in, now)
	require.NoError(t, err)
	require.Equal(t, int64(4), next.Version)
	require.Equal(t, "approved", next.State)
	require.Equal(t, "not_evaluated", next.ClinicalAuthority)
	require.Equal(t, "awaiting_review", caseRecord.State)
	in.Decision = "reject"
	next, err = manualIdentityDecisionState(source, actor, caseRecord, evidence, in, now)
	require.NoError(t, err)
	require.Equal(t, "rejected", next.State)
}

func TestManualIdentitySelfCaseBindsCurrentAccountIncarnation(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	profile, id := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	source := manualIdentityProfile{ID: profile, Version: 1, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	in := ManualIdentityCaseInput{SourceProfileID: profile, ExpectedProfileVersion: manualIdentityVersion(1), ConsentAcknowledged: true, Reason: "Assurance for my own account"}
	result, err := manualIdentityOpenState(source, id, "current-account", 3, in, now)
	require.NoError(t, err)
	require.Equal(t, "current-account", result.Subject)
	require.Equal(t, int64(3), result.MembershipEpoch)
	require.Equal(t, "awaiting_evidence", result.State)
	require.Equal(t, int64(1), result.Version)
	require.Nil(t, result.LatestExaminationID)
	require.Equal(t, now, result.CreatedAt)
	require.Equal(t, "not_evaluated", result.ClinicalAuthority)
	for _, tc := range []struct {
		name    string
		subject string
		epoch   int64
	}{
		{"missing actor", "", 3}, {"unbound epoch", "current-account", 0}, {"invalid account", "other:account", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manualIdentityOpenState(source, id, tc.subject, tc.epoch, in, now)
			require.Error(t, err)
		})
	}
	source.Version++
	_, err = manualIdentityOpenState(source, id, "current-account", 3, in, now)
	require.ErrorIs(t, err, errManualIdentityState)
}

func TestManualIdentityExaminationBindsExactCaseAndAssuredExaminer(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	profile := uuid.Must(uuid.NewV7())
	source := manualIdentityProfile{ID: profile, Version: 1, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	actor := manualPersonActor{ProfileID: profile, ProfileVersion: 1, PersonID: uuid.Must(uuid.NewV7()), PersonVersion: 1, Subject: "examiner", MembershipEpoch: 1, ValidUntil: now.Add(time.Hour)}
	current := ManualIdentityCase{ID: uuid.Must(uuid.NewV7()), SourceProfileID: profile, ProfileVersion: 1, Subject: "target", MembershipEpoch: 1, Version: 1, State: "awaiting_evidence", ClinicalAuthority: "not_evaluated"}
	in := manualExaminationFixture(now)
	id := uuid.Must(uuid.NewV7())
	for _, tc := range []struct {
		name   string
		change func(*manualPersonActor, *ManualIdentityCase, *ManualIdentityExaminationInput)
	}{
		{"same account", func(a *manualPersonActor, c *ManualIdentityCase, _ *ManualIdentityExaminationInput) {
			a.Subject = c.Subject
		}},
		{"same person other account", func(a *manualPersonActor, _ *ManualIdentityCase, in *ManualIdentityExaminationInput) {
			in.PersonID = a.PersonID
		}},
		{"expired examiner", func(a *manualPersonActor, _ *ManualIdentityCase, _ *ManualIdentityExaminationInput) {
			a.ValidUntil = now
		}},
		{"other source", func(a *manualPersonActor, _ *ManualIdentityCase, _ *ManualIdentityExaminationInput) {
			a.ProfileID = uuid.Must(uuid.NewV7())
		}},
		{"old source version", func(a *manualPersonActor, _ *ManualIdentityCase, _ *ManualIdentityExaminationInput) {
			a.ProfileVersion++
		}},
		{"stale case", func(_ *manualPersonActor, c *ManualIdentityCase, _ *ManualIdentityExaminationInput) { c.Version++ }},
		{"terminal case", func(_ *manualPersonActor, c *ManualIdentityCase, _ *ManualIdentityExaminationInput) {
			c.State = "approved"
		}},
		{"version exhausted", func(_ *manualPersonActor, c *ManualIdentityCase, in *ManualIdentityExaminationInput) {
			c.Version = manualIdentityMaxVersion
			in.ExpectedCaseVersion = manualIdentityVersion(c.Version)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c, input := actor, current, in
			tc.change(&a, &c, &input)
			_, err := manualIdentityExaminationState(source, a, c, id, input, now)
			require.Error(t, err)
		})
	}
	next, err := manualIdentityExaminationState(source, actor, current, id, in, now)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", next.State)
	require.Equal(t, int64(2), next.Version)
	require.Equal(t, &id, next.LatestExaminationID)
	require.Equal(t, "awaiting_evidence", current.State)
	in.ExpectedCaseVersion = &next.Version
	newID := uuid.Must(uuid.NewV7())
	refreshed, err := manualIdentityExaminationState(source, actor, next, newID, in, now)
	require.NoError(t, err)
	require.Equal(t, &newID, refreshed.LatestExaminationID)
	require.Equal(t, int64(3), refreshed.Version)
}
