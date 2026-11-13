package platform

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type governanceTopologyProvider interface {
	GovernanceTopology(*tenantdb.DB) authz.CurrentGovernanceTopologyGuard
}

// GovernanceTopology binds the sole canonical Workforce owner or an unavailable guard.
func GovernanceTopology(modules []Module, db *tenantdb.DB) authz.CurrentGovernanceTopologyGuard {
	if owner, ok := soleOwner(modules, db, "workforce").(governanceTopologyProvider); ok {
		return owner.GovernanceTopology(db)
	}
	return unavailableGovernanceTopology{}
}

type unavailableGovernanceTopology struct{}

func (unavailableGovernanceTopology) WithGovernanceTopology(context.Context, authz.GovernanceEstablishmentScope, func(context.Context, authz.GovernanceTopologyReference) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}
