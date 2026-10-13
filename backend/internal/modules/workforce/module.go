// Package workforce owns hospital-local staff directories and explicit access.
package workforce

import (
	_ "embed"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermHospitalManage authz.Permission = "workforce.hospital.manage"

//go:embed schema.sql
var schema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "workforce" }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_workforce", SQL: schema}}
}
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermHospitalManage}}
}
func (*Module) Publishes() []string                         { return nil }
func (*Module) DirectedSubjects() []string                  { return nil }
func (*Module) DirectedWriteTables() []string               { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB}
	g := r.Group("/workforce/hospitals")
	g.PUT("/:hospital", PermHospitalManage, h.putHospital)
	g.PUT("/:hospital/access/:subject", PermHospitalManage, h.putAccess)
	g.DELETE("/:hospital/access/:subject", PermHospitalManage, h.deleteAccess)
	// Public still requires authentication and current tenant membership.
	// These handlers additionally check hospital grants inside the transaction.
	g.PUT("/:hospital/staff/:id", authz.Public, h.putStaff)
	g.GET("/:hospital/staff/:id", authz.Public, h.getStaff)
	platform.ListRoute(g, "/:hospital/staff", authz.Public, h.listStaff)
}
