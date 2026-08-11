package platform

import (
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// Deps is everything a module may depend on. Modules must not reach
// around it — cross-module data access goes through events (spec D6).
type Deps struct {
	DB  *tenantdb.DB
	Bus *events.Bus
}

// Module is the registration contract from issue #2.
type Module interface {
	Name() string
	Migrations() []tenantdb.Migration
	// Permissions declares every permission this module's routes use and
	// which system roles hold it. authz.RoleTenantAdmin is implicit — the
	// reconciler grants it everything, so never list it here.
	Permissions() []authz.Grant
	Routes(r *Router, deps Deps)
	Consumers(deps Deps) []events.Consumer
}
