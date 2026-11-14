package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type assignmentTargetProvider interface {
	AssignmentTargets(*tenantdb.DB) authz.GovernedAssignmentTargetReader
}

// AssignmentTargets resolves the canonical Workforce owner, never a tenant directory.
func AssignmentTargets(modules []Module, db *tenantdb.DB) authz.GovernedAssignmentTargetReader {
	for _, module := range modules {
		if module.Name() == "workforce" {
			if provider, ok := module.(assignmentTargetProvider); ok {
				return provider.AssignmentTargets(db)
			}
		}
	}
	return nil
}
