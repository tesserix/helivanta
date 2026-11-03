package authz

import (
	"context"

	"github.com/google/uuid"
)

// StaffActorUse is Workforce's permission to consume current Staff actor eligibility.
const StaffActorUse Permission = "workforce.staff_actor.use"

type StaffActorScope struct {
	TenantID, Subject string
	HospitalID        uuid.UUID
}

// StaffActorReference is attributable source data, never reusable authorization.
type StaffActorReference struct {
	StaffID, LinkID, GenerationID uuid.UUID
	LinkVersion, EmploymentEpoch  int64
}

type CurrentStaffActorGuard interface {
	WithCurrentStaffActor(context.Context, StaffActorScope, func(context.Context, StaffActorReference) error) error
}
