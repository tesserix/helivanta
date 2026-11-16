package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrGovernanceScopeHidden = errors.New("governance scope not found")

// GovernanceEstablishmentScope names a tenant or one canonical employing root.
type GovernanceEstablishmentScope struct {
	TenantID string
	Kind     string
	ID       *uuid.UUID
}

// GovernanceTopologyReference is current only during its guarded callback.
type GovernanceTopologyReference struct {
	HospitalCount int64
}

// CurrentGovernanceTopologyGuard retains the owner topology gate during admission or use.
type CurrentGovernanceTopologyGuard interface {
	WithGovernanceTopology(context.Context, GovernanceEstablishmentScope, func(context.Context, GovernanceTopologyReference) error) error
}
