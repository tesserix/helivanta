package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type departmentReferenceProvider interface {
	DepartmentReferences(*tenantdb.DB) authz.DepartmentReferenceGuard
}

// DepartmentReferences binds only the registered structural owner's authorization guard.
func DepartmentReferences(modules []Module, db *tenantdb.DB) authz.DepartmentReferenceGuard {
	if db == nil {
		return nil
	}
	var owner Module
	for _, module := range modules {
		if module.Name() != "organization" {
			continue
		}
		if owner != nil {
			return nil
		}
		owner = module
	}
	provider, ok := owner.(departmentReferenceProvider)
	if !ok {
		return nil
	}
	return provider.DepartmentReferences(db)
}
