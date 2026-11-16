package platform

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type governanceSourceProvider interface {
	GovernanceSources(*tenantdb.DB, authz.CurrentGovernanceTopologyGuard) authz.CurrentGovernanceSourceGuard
}

// GovernanceSources requires the sole IAM owner and sole canonical establishment owner.
func GovernanceSources(modules []Module, db *tenantdb.DB) authz.CurrentGovernanceSourceGuard {
	owner, ok := soleOwner(modules, db, "iam").(governanceSourceProvider)
	_, topologyOK := soleOwner(modules, db, "workforce").(governanceTopologyProvider)
	if ok && topologyOK {
		return owner.GovernanceSources(db, GovernanceTopology(modules, db))
	}
	return unavailableGovernanceSources{}
}

type unavailableGovernanceSources struct{}

func (unavailableGovernanceSources) WithGovernanceSource(context.Context, authz.GovernanceSourceRequest, func(context.Context, authz.GovernanceSourceEvidence) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}
