package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type departmentReferenceProvider interface {
	DepartmentReferences(*tenantdb.DB) authz.DepartmentReferenceGuard
}

type departmentScopeProvider interface {
	DepartmentScopes(*tenantdb.DB) authz.DepartmentScopeGuard
}

// DepartmentReferences binds only the registered structural owner's authorization guard.
func DepartmentReferences(modules []Module, db *tenantdb.DB) authz.DepartmentReferenceGuard {
	provider, ok := soleOwner(modules, db, "organization").(departmentReferenceProvider)
	if !ok {
		return nil
	}
	return provider.DepartmentReferences(db)
}

// DepartmentScopes binds only the registered structural owner's department-subtree guard.
func DepartmentScopes(modules []Module, db *tenantdb.DB) authz.DepartmentScopeGuard {
	provider, ok := soleOwner(modules, db, "organization").(departmentScopeProvider)
	if !ok {
		return nil
	}
	return provider.DepartmentScopes(db)
}

// soleOwner fails closed without a database or when the named owner is missing or duplicated.
func soleOwner(modules []Module, db *tenantdb.DB, name string) Module {
	if db == nil {
		return nil
	}
	var owner Module
	for _, module := range modules {
		if module.Name() != name {
			continue
		}
		if owner != nil {
			return nil
		}
		owner = module
	}
	return owner
}
