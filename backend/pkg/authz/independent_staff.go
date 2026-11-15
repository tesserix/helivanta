package authz

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

// IndependentStaffScope names a current nonclinical identity source, never operating authority.
type IndependentStaffScope struct {
	TenantID, Subject, EmployerKind string
	EmployerID                      uuid.UUID
}

// IndependentStaffReference binds a reviewed account to one exact Staff revision.
type IndependentStaffReference struct {
	StaffID, LinkID           uuid.UUID
	StaffVersion, LinkVersion int64
}

type IndependentEmployerScope struct {
	TenantID, EmployerKind string
	EmployerID             uuid.UUID
}

type CurrentIndependentStaffGuard interface {
	WithIndependentEmployerAccess(context.Context, IndependentEmployerScope, string, func(context.Context) error) error
	WithCurrentIndependentStaff(context.Context, IndependentStaffScope, func(context.Context, IndependentStaffReference) error) error
	WithCurrentIndependentStaffForReview(context.Context, IndependentStaffScope, string, func(context.Context, IndependentStaffReference) error) error
}

// GovernedIndependentEmployerRequest binds duty administration to one current source.
// It provides no target Staff, location or operating authority.
type GovernedIndependentEmployerRequest struct {
	Principal  authn.Principal
	Scope      IndependentEmployerScope
	Capability GovernanceCapability
	Source     GovernanceSourceEvidence
}

type GovernedIndependentEmployerGuard interface {
	WithGovernedIndependentEmployerAccess(context.Context, GovernedIndependentEmployerRequest, func(context.Context) error) error
}
