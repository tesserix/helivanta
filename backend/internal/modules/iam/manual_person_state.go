package iam

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var errManualIdentityState = errors.New("manual identity source or case changed")

type manualIdentityProfile struct {
	ID         uuid.UUID
	Version    int64
	ValidFrom  time.Time
	ValidUntil time.Time
	Revoked    bool
}

type manualIdentityExamination struct {
	ID           uuid.UUID
	CaseID       uuid.UUID
	CaseVersion  int64
	PersonID     uuid.UUID
	ValidUntil   time.Time
	Contributors []uuid.UUID
}

func manualIdentityOpenState(source manualIdentityProfile, id uuid.UUID, subject string, epoch int64, in ManualIdentityCaseInput, now time.Time) (ManualIdentityCase, error) {
	if err := validateManualIdentityCase(in); err != nil {
		return ManualIdentityCase{}, err
	}
	if !source.current(in.SourceProfileID, *in.ExpectedProfileVersion, now) ||
		!manualIdentityUUID(id) || !manualIdentitySubject(subject) || epoch < 1 || epoch > manualIdentityMaxVersion || !governanceInstant(now) {
		return ManualIdentityCase{}, errManualIdentityState
	}
	return ManualIdentityCase{ID: id, SourceProfileID: source.ID, ProfileVersion: source.Version, Subject: subject, MembershipEpoch: epoch, Version: 1, State: "awaiting_evidence", CreatedAt: now, ClinicalAuthority: "not_evaluated"}, nil
}

func manualIdentityExaminationState(source manualIdentityProfile, actor manualPersonActor, current ManualIdentityCase, id uuid.UUID, in ManualIdentityExaminationInput, now time.Time) (ManualIdentityCase, error) {
	if err := validateManualIdentityExamination(in, now); err != nil {
		return ManualIdentityCase{}, err
	}
	if !source.current(current.SourceProfileID, current.ProfileVersion, now) || !actor.current(source.ID, now) ||
		actor.ProfileVersion != source.Version || actor.Subject == current.Subject || actor.PersonID == in.PersonID ||
		!manualIdentityUUID(current.ID) || !manualIdentityUUID(id) || current.Version != *in.ExpectedCaseVersion || current.Version >= manualIdentityMaxVersion ||
		(current.State != "awaiting_evidence" && current.State != "awaiting_review") ||
		!manualIdentitySubject(current.Subject) || current.MembershipEpoch < 1 || current.MembershipEpoch > manualIdentityMaxVersion ||
		current.ClinicalAuthority != "not_evaluated" {
		return ManualIdentityCase{}, errManualIdentityState
	}
	next := current
	next.Version++
	next.State = "awaiting_review"
	next.LatestExaminationID = &id
	return next, nil
}

func (source manualIdentityProfile) current(id uuid.UUID, version int64, now time.Time) bool {
	return manualIdentityUUID(source.ID) && source.ID == id && source.Version == version &&
		version >= 1 && version <= manualIdentityMaxVersion && !source.Revoked &&
		governanceInstant(source.ValidFrom) && governanceInstant(source.ValidUntil) &&
		!source.ValidFrom.After(now) && source.ValidUntil.After(now) && source.ValidUntil.After(source.ValidFrom)
}

func manualIdentityDecisionState(source manualIdentityProfile, actor manualPersonActor, current ManualIdentityCase, evidence manualIdentityExamination, in ManualIdentityDecisionInput, now time.Time) (ManualIdentityCase, error) {
	if err := validateManualIdentityDecision(in); err != nil {
		return ManualIdentityCase{}, err
	}
	if !source.current(current.SourceProfileID, current.ProfileVersion, now) ||
		actor.ProfileVersion != source.Version || actor.Subject == current.Subject ||
		!manualIdentityUUID(current.ID) || current.Version != *in.ExpectedCaseVersion || current.Version >= manualIdentityMaxVersion ||
		current.State != "awaiting_review" || current.LatestExaminationID == nil ||
		*current.LatestExaminationID != in.ExaminationID || evidence.ID != in.ExaminationID ||
		evidence.CaseID != current.ID || evidence.CaseVersion != current.Version ||
		!manualIdentitySubject(current.Subject) || current.MembershipEpoch < 1 || current.MembershipEpoch > manualIdentityMaxVersion ||
		current.ClinicalAuthority != "not_evaluated" ||
		!governanceInstant(evidence.ValidUntil) || !evidence.ValidUntil.After(now) || len(evidence.Contributors) == 0 {
		return ManualIdentityCase{}, errManualIdentityState
	}
	for _, contributor := range evidence.Contributors {
		if err := manualIdentityIndependentReview(actor, source.ID, contributor, evidence.PersonID, now); err != nil {
			return ManualIdentityCase{}, err
		}
	}
	next := current
	next.Version++
	if in.Decision == "approve" {
		next.State = "approved"
	} else {
		next.State = "rejected"
	}
	return next, nil
}
