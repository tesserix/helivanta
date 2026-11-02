package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrDepartmentReferenceChanged = errors.New("department reference changed")
	ErrDepartmentReferenceHidden  = errors.New("department reference unavailable in this scope")
)

// DepartmentReference pins an owner-validated structural source, not a Staff assignment.
type DepartmentReference struct {
	TenantID                                     string
	HospitalID, DepartmentID, DepartmentRevision uuid.UUID
}

// DepartmentReferenceGuard holds the current owner source through a guarded command.
type DepartmentReferenceGuard interface {
	WithCurrentDepartment(context.Context, DepartmentReference, func(context.Context) error) error
}
