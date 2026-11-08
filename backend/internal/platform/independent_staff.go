package platform

import (
	"context"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type independentStaffProvider interface {
	IndependentStaffIdentities(*tenantdb.DB) authz.CurrentIndependentStaffGuard
}

func IndependentStaffIdentities(modules []Module, db *tenantdb.DB) authz.CurrentIndependentStaffGuard {
	if owner, ok := soleOwner(modules, db, "workforce").(independentStaffProvider); ok {
		return owner.IndependentStaffIdentities(db)
	}
	return unavailableIndependentStaff{}
}

type unavailableIndependentStaff struct{}

func (unavailableIndependentStaff) WithCurrentIndependentStaff(context.Context, authz.IndependentStaffScope, func(context.Context, authz.IndependentStaffReference) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}

func (unavailableIndependentStaff) WithCurrentIndependentStaffForReview(context.Context, authz.IndependentStaffScope, string, func(context.Context, authz.IndependentStaffReference) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}

func (unavailableIndependentStaff) WithIndependentEmployerAccess(context.Context, authz.IndependentEmployerScope, string, func(context.Context) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}
