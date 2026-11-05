package authz

import (
	"context"
	"errors"
	"slices"
	"time"
)

// ErrScopedCommandDenied means the checked authority is no longer current.
var ErrScopedCommandDenied = errors.New("scoped command authority is no longer current")

// ErrScopedCredentialRevoked must never fall back to another business grant.
var ErrScopedCredentialRevoked = errors.New("scoped command credential has been revoked")

// ScopeRequest comes from a verified principal and a server-declared product scope/action.
// Assignment IDs and revisions are selected by the authority owner, never by the caller.
type ScopeRequest struct {
	TenantID   string
	Subject    string
	HospitalID string
	Permission Permission
	// DepartmentPath is the target's active node-to-root path from Organization; empty selects hospital grants only.
	DepartmentPath []string
}

// MaxDepartmentPath matches Organization's persisted department depth.
const MaxDepartmentPath = 8

// MaxDepartmentScopes bounds one subject's current department grants per hospital and permission (ADR-0055).
const MaxDepartmentScopes = 50

// Same compares every field, including the exact department path order.
func (r ScopeRequest) Same(other ScopeRequest) bool {
	return r.TenantID == other.TenantID && r.Subject == other.Subject && r.HospitalID == other.HospitalID &&
		r.Permission == other.Permission && slices.Equal(r.DepartmentPath, other.DepartmentPath)
}

// ScopeGrant is evidence for one authorization check, not a token or cache entry.
// A zero AssignmentID means denied. Products still verify their current object/link state.
type ScopeGrant struct {
	AssignmentID    string
	Version         int64
	ScopeEpoch      int64
	MembershipEpoch int64
	// ScopeKind is "hospital" or "department"; a department grant reaches only targets inside DepartmentID.
	ScopeKind    string
	DepartmentID string
}

type ScopedAuthorizer interface {
	AuthorizeScope(context.Context, ScopeRequest) (ScopeGrant, error)
}

// ScopedCommandGuard keeps current IAM authority locked through the product commit.
// The receipt and authentication time must come from the verified request context.
type ScopedCommandGuard interface {
	WithScopeGrant(context.Context, ScopeRequest, ScopeGrant, time.Time, func(context.Context) error) error
}

// MaxScopedCommandChecks bounds independently reserved owner permissions.
const MaxScopedCommandChecks = 3

// ScopeCheck binds one server-required permission to its current IAM evidence.
type ScopeCheck struct {
	Request ScopeRequest
	Grant   ScopeGrant
}

// CompoundScopedCommandGuard holds all required grants through one owner commit.
type CompoundScopedCommandGuard interface {
	WithScopeGrants(context.Context, []ScopeCheck, time.Time, func(context.Context) error) error
}

// DepartmentScopeLister discovers current department grants for listing, never as a command receipt.
type DepartmentScopeLister interface {
	DepartmentScopes(context.Context, ScopeRequest) ([]ScopeGrant, error)
}

type ScopedAssignmentChecker interface {
	CheckScopedAssignment(context.Context, ScopedAssignment, string, Permission) (bool, error)
}
