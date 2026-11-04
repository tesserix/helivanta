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
	provider, ok := structuralOwner(modules, db).(departmentReferenceProvider)
	if !ok {
		return nil
	}
	return provider.DepartmentReferences(db)
}

// DepartmentScopes binds only the registered structural owner's department-subtree guard.
func DepartmentScopes(modules []Module, db *tenantdb.DB) authz.DepartmentScopeGuard {
	provider, ok := structuralOwner(modules, db).(departmentScopeProvider)
	if !ok {
		return nil
	}
	return provider.DepartmentScopes(db)
}

// structuralOwner fails closed without a database or when the organization owner is missing or duplicated.
func structuralOwner(modules []Module, db *tenantdb.DB) Module {
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
	return owner
}
