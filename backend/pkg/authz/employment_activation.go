package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EmploymentBasis identifies an owner-consumed source; it never grants access.
type EmploymentBasis struct {
	TenantID, Subject                 string
	HospitalID, StaffID, GenerationID uuid.UUID
	EmploymentEpoch                   int64
	Until                             time.Time
}

// EmploymentBasisWriter is usable only inside its guarded activation callback.
type EmploymentBasisWriter interface {
	ConsumeEmploymentBasis(context.Context, EmploymentBasis) error
}

// EmploymentActivationGuard serializes basis changes before IAM and product locks.
type EmploymentActivationGuard interface {
	WithEmploymentActivation(context.Context, []ScopeCheck, time.Time, func(context.Context, EmploymentBasisWriter) error) error
}
