package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const StaffPostingSourceUse Permission = "workforce.posting_source.use"

type StaffPostingSourceScope struct {
	TenantID, Subject                                                              string
	HospitalID, StaffID, PostingID, RevisionID, DepartmentID, DepartmentRevisionID uuid.UUID
	Version                                                                        int64
}

// StaffPostingSourceReference records an observation; it is never reusable authorization.
type StaffPostingSourceReference struct {
	UseID, StaffID, PostingID, RevisionID, GenerationID, DepartmentID, DepartmentRevisionID uuid.UUID
	Version, StaffVersion, EmploymentEpoch                                                  int64
	Actor                                                                                   StaffActorReference
	SourceCount                                                                             int
	CheckedAt, SourceUntil                                                                  time.Time
	SourceHash                                                                              string
}

type CurrentStaffPostingSourceGuard interface {
	WithCurrentStaffPosting(context.Context, StaffPostingSourceScope, func(context.Context, StaffPostingSourceReference) error) error
}
