package authz

import (
	"context"
	"errors"
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
}

// ScopeGrant is evidence for one authorization check, not a token or cache entry.
// A zero AssignmentID means denied. Products still verify their current object/link state.
type ScopeGrant struct {
	AssignmentID    string
	Version         int64
	ScopeEpoch      int64
	MembershipEpoch int64
}

type ScopedAuthorizer interface {
	AuthorizeScope(context.Context, ScopeRequest) (ScopeGrant, error)
}

// ScopedCommandGuard keeps current IAM authority locked through the product commit.
// The receipt and authentication time must come from the verified request context.
type ScopedCommandGuard interface {
	WithScopeGrant(context.Context, ScopeRequest, ScopeGrant, time.Time, func(context.Context) error) error
}

type ScopedAssignmentChecker interface {
	CheckScopedAssignment(context.Context, ScopedAssignment, string, Permission) (bool, error)
}
