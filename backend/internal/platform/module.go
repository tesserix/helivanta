package platform

import (
	"github.com/gin-gonic/gin"
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
	Routes(r *gin.RouterGroup, deps Deps)
	Consumers() []events.Consumer
}
