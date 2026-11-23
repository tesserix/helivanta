package authz

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// StocktakeReadScopeChecks returns exact held department read/source proofs, not reusable authority.
func StocktakeReadScopeChecks(ctx context.Context, scope StaffActorScope) ([]ScopeCheck, error) {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || scope.HospitalID == uuid.Nil || scope.Subject == "" || len(scope.DepartmentPath) == 0 || len(scope.DepartmentPath) > MaxDepartmentPath {
		return nil, ErrScopedAuthorizationUnavailable
	}
	for i, value := range scope.DepartmentPath {
		id, err := uuid.Parse(value)
		if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != value || value == scope.HospitalID.String() || slices.Contains(scope.DepartmentPath[:i], value) {
			return nil, ErrScopedAuthorizationUnavailable
		}
	}
	checks := make([]ScopeCheck, 0, 2)
	for _, permission := range []Permission{"pharmacy.stocktake.read", StocktakeActorUse} {
		request := ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), DepartmentPath: slices.Clone(scope.DepartmentPath), Permission: permission}
		grant, ok := ScopedCommandGrant(ctx, request)
		id, err := uuid.Parse(grant.AssignmentID)
		if !ok || err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != grant.AssignmentID || grant.Version < 1 || grant.ScopeEpoch < 1 || grant.MembershipEpoch < 1 || grant.ScopeKind != "department" || !slices.Contains(scope.DepartmentPath, grant.DepartmentID) {
			return nil, ErrScopedAuthorizationUnavailable
		}
		checks = append(checks, ScopeCheck{Request: request, Grant: grant})
	}
	return checks, nil
}

// StocktakeHospitalNameReader discloses Workforce's hospital name under current stocktake Staff authority.
type StocktakeHospitalNameReader interface {
	StocktakeHospitalName(context.Context, StaffActorScope) (string, error)
}

// StocktakeDepartmentNameReader discloses Organization's department name under exact held stocktake proofs.
type StocktakeDepartmentNameReader interface {
	StocktakeDepartmentName(context.Context, StaffActorScope) (string, error)
}
