package authz

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// HospitalWorkPurpose names one department-scoped hospital work purpose. Only the
// constants below are known; any other value refuses, so the set stays closed.
type HospitalWorkPurpose string

const (
	StocktakeWork  HospitalWorkPurpose = "stocktake"
	WardIndentWork HospitalWorkPurpose = "ward_indent"
)

func (w HospitalWorkPurpose) permissions() (read, actor Permission, ok bool) {
	switch w {
	case StocktakeWork:
		return "pharmacy.stocktake.read", StocktakeActorUse, true
	case WardIndentWork:
		return "pharmacy.indent.read", WardIndentActorUse, true
	}
	return "", "", false
}

// Known reports whether w is one of the declared purposes.
func (w HospitalWorkPurpose) Known() bool {
	_, _, ok := w.permissions()
	return ok
}

// ReadPermission is the operation permission every read under w must hold.
func (w HospitalWorkPurpose) ReadPermission() Permission {
	read, _, _ := w.permissions()
	return read
}

// ActorPermission is the Workforce actor purpose w consumes.
func (w HospitalWorkPurpose) ActorPermission() Permission {
	_, actor, _ := w.permissions()
	return actor
}

// ReadScopeChecks returns exact held department read/source proofs, not reusable authority.
func (w HospitalWorkPurpose) ReadScopeChecks(ctx context.Context, scope StaffActorScope) ([]ScopeCheck, error) {
	read, actor, known := w.permissions()
	tenant, err := uuid.Parse(scope.TenantID)
	if !known || err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || scope.HospitalID == uuid.Nil || scope.Subject == "" || len(scope.DepartmentPath) == 0 || len(scope.DepartmentPath) > MaxDepartmentPath {
		return nil, ErrScopedAuthorizationUnavailable
	}
	for i, value := range scope.DepartmentPath {
		id, err := uuid.Parse(value)
		if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != value || value == scope.HospitalID.String() || slices.Contains(scope.DepartmentPath[:i], value) {
			return nil, ErrScopedAuthorizationUnavailable
		}
	}
	checks := make([]ScopeCheck, 0, 2)
	for _, permission := range []Permission{read, actor} {
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

// HospitalWorkHospitalNameReader discloses Workforce's hospital name under current Staff authority for one purpose.
type HospitalWorkHospitalNameReader interface {
	HospitalWorkHospitalName(context.Context, HospitalWorkPurpose, StaffActorScope) (string, error)
}

// HospitalWorkDepartmentNameReader discloses Organization's department name under exact held proofs for one purpose.
type HospitalWorkDepartmentNameReader interface {
	HospitalWorkDepartmentName(context.Context, HospitalWorkPurpose, StaffActorScope) (string, error)
}
