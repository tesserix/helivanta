package authz

import "context"

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

type ScopedAssignmentChecker interface {
	CheckScopedAssignment(context.Context, ScopedAssignment, string, Permission) (bool, error)
}
