package authz

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

const (
	// DepartmentSubtreeLimit bounds one discovery read (ADR-0055); a larger subtree fails closed.
	DepartmentSubtreeLimit = 1000
	// DepartmentScopeRootLimit matches the per-subject department grant bound.
	DepartmentScopeRootLimit = 50
)

// ErrDepartmentSubtreeLimit is unavailability, never a truncated answer.
var ErrDepartmentSubtreeLimit = fmt.Errorf("department subtree limit exceeded: %w", ErrScopedAuthorizationUnavailable)

// DepartmentScopeGuard is Organization's current department ancestry for department-scoped assignments.
type DepartmentScopeGuard interface {
	// CurrentPath returns the active node-to-root path. It selects candidates and is never authority.
	CurrentPath(ctx context.Context, tenantID string, hospitalID, departmentID uuid.UUID) ([]uuid.UUID, error)
	// CurrentSubtree returns the active nodes under active roots for discovery and is never authority.
	CurrentSubtree(ctx context.Context, tenantID string, hospitalID uuid.UUID, roots []uuid.UUID) ([]uuid.UUID, error)
	// WithinCurrentDepartment holds the target's active path, which must contain scope, through the guarded command's commit.
	WithinCurrentDepartment(ctx context.Context, tenantID string, hospitalID, target, scope uuid.UUID, action func(context.Context) error) error
}
