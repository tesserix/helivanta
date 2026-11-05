package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type staffPostingSourceProvider interface {
	PostingSources(*tenantdb.DB, authz.DepartmentReferenceGuard, authz.CurrentStaffActorGuard) authz.CurrentStaffPostingSourceGuard
}

func StaffPostingSources(modules []Module, db *tenantdb.DB) authz.CurrentStaffPostingSourceGuard {
	for _, module := range modules {
		if module.Name() != "workforce" {
			continue
		}
		if provider, ok := module.(staffPostingSourceProvider); ok {
			return provider.PostingSources(db, DepartmentReferences(modules, db), StaffActors(modules, db))
		}
	}
	return nil
}
