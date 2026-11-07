package authz

import (
	"context"
	"github.com/google/uuid"
)

// OperatingLocationScope names one independently owned physical stock source.
type OperatingLocationScope struct {
	TenantID, EmployerKind string
	EmployerID, LocationID uuid.UUID
}

// OperatingLocationReference is structural source evidence, never licence or trading eligibility.
type OperatingLocationReference struct {
	Scope                            OperatingLocationScope
	EmployerVersion, LocationVersion int64
	Country                          string
}

type CurrentOperatingLocationGuard interface {
	CurrentOperatingLocation(context.Context, OperatingLocationScope) (OperatingLocationReference, error)
}

func ValidOperatingLocationScope(scope OperatingLocationScope) bool {
	tenant, err := uuid.Parse(scope.TenantID)
	return err == nil && tenant != uuid.Nil && tenant.String() == scope.TenantID && (scope.EmployerKind == "supplier" || scope.EmployerKind == "pharmacy") && scope.EmployerID.Version() == 7 && scope.EmployerID.Variant() == uuid.RFC4122 && scope.LocationID.Version() == 7 && scope.LocationID.Variant() == uuid.RFC4122
}
