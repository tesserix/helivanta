package authz

import (
	"context"
	"github.com/google/uuid"
	"time"
)

const PartnerMemberSourceUse Permission = "workforce.partner_member_source.use"

type PartnerMemberScope struct {
	Posting       StaffPostingSourceScope
	MemberSubject string
}

// PartnerMemberReference records owner observations, never reusable authority.
type PartnerMemberReference struct {
	UseID                  uuid.UUID
	Subject, DisplayName   string
	Member                 StaffActorReference
	Source                 StaffPostingSourceReference
	CheckedAt, SourceUntil time.Time
}

type PartnerMemberCandidatePage struct {
	Limit          int
	AfterCreatedAt time.Time
	AfterID        uuid.UUID
}

// PartnerMemberCandidate is an owner-selected hint; preparation must recheck live eligibility.
type PartnerMemberCandidate struct {
	ID, HospitalID, StaffID, PostingRevisionID, DepartmentID, DepartmentRevisionID uuid.UUID
	Subject, DisplayName                                                           string
	PostingVersion                                                                 int64
	StartsAt, NominalUntil, CreatedAt                                              time.Time
}

type CurrentPartnerMemberGuard interface {
	WithPartnerMemberCandidates(context.Context, StaffActorScope, PartnerMemberCandidatePage, func(context.Context, []PartnerMemberCandidate) error) error
	WithCurrentPartnerActor(context.Context, StaffActorScope, func(context.Context, StaffActorReference) error) error
	WithCurrentPartnerMember(context.Context, PartnerMemberScope, func(context.Context, PartnerMemberReference) error) error
}
