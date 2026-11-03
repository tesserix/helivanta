package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type staffActorProvider interface {
	StaffActors(*tenantdb.DB) authz.CurrentStaffActorGuard
}

func StaffActors(modules []Module, db *tenantdb.DB) authz.CurrentStaffActorGuard {
	for _, module := range modules {
		if module.Name() != "workforce" {
			continue
		}
		if provider, ok := module.(staffActorProvider); ok {
			return provider.StaffActors(db)
		}
	}
	return nil
}
