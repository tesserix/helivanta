package authz

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

type OperatingScopeRequest struct {
	Principal              authn.Principal
	EmployerKind           string
	EmployerID, LocationID uuid.UUID
	Permission             Permission
}
type OperatingScopeGrant struct {
	Scope                           OperatingLocationScope
	Permission                      Permission
	DutyID                          uuid.UUID
	SnapshotVersion, AuthorityEpoch int64
	Identity                        IndependentStaffReference
}
type OperatingScopeAuthorizer interface {
	WithOperatingScope(context.Context, OperatingScopeRequest, func(context.Context, OperatingScopeGrant) error) error
}
