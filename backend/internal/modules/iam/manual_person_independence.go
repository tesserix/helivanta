package iam

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var errManualIdentityIndependence = errors.New("manual identity review requires independent current people")

type manualPersonActor struct {
	ProfileID       uuid.UUID
	ProfileVersion  int64
	PersonID        uuid.UUID
	PersonVersion   int64
	Subject         string
	MembershipEpoch int64
	ValidUntil      time.Time
}

func manualIdentityIndependentReview(actor manualPersonActor, profile, examiner, target uuid.UUID, now time.Time) error {
	if !manualIdentityUUID(profile) || !manualIdentityUUID(examiner) || !manualIdentityUUID(target) ||
		!actor.current(profile, now) || actor.PersonID == examiner || actor.PersonID == target {
		return errManualIdentityIndependence
	}
	return nil
}

func (actor manualPersonActor) current(profile uuid.UUID, now time.Time) bool {
	return actor.ProfileID == profile && manualIdentityUUID(actor.PersonID) &&
		actor.ProfileVersion >= 1 && actor.ProfileVersion <= manualIdentityMaxVersion &&
		actor.PersonVersion >= 1 && actor.PersonVersion <= manualIdentityMaxVersion &&
		actor.MembershipEpoch >= 1 && actor.MembershipEpoch <= manualIdentityMaxVersion &&
		manualIdentitySubject(actor.Subject) && governanceInstant(actor.ValidUntil) && actor.ValidUntil.After(now)
}
