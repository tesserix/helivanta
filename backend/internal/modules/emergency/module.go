// Package emergency owns operating-base access and canonical emergency intake.
package emergency

import (
	_ "embed"

	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	PermBaseRead    authz.Permission = "emergency.base.read"
	PermBaseManage  authz.Permission = "emergency.base.manage"
	PermCaseRead    authz.Permission = "emergency.case.read"
	PermCaseIntake  authz.Permission = "emergency.case.intake"
	PermCaseClose   authz.Permission = "emergency.case.close"
	PermFleetRead   authz.Permission = "emergency.fleet.read"
	PermFleetManage authz.Permission = "emergency.fleet.manage"
	PermFleetCheck  authz.Permission = "emergency.fleet.check"
)

//go:embed schema.sql
var schema string

//go:embed fleet.sql
var fleetSchema string

//go:embed contract/intake.json
var intakeCatalogue string

type Module struct{}

func New() *Module             { return &Module{} }
func (m *Module) Name() string { return "emergency" }
func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_emergency", SQL: schema}, {ID: "0002_emergency", SQL: fleetSchema}}
}
func (m *Module) Permissions() []authz.Grant {
	operators := []authz.Role{authz.RoleEmergencyDispatcher, authz.RoleEmergencySupervisor}
	return []authz.Grant{{Permission: PermBaseRead, Roles: operators}, {Permission: PermBaseManage}, {Permission: PermCaseRead, Roles: operators}, {Permission: PermCaseIntake, Roles: operators}, {Permission: PermCaseClose, Roles: []authz.Role{authz.RoleEmergencySupervisor}}, {Permission: PermFleetRead, Roles: operators}, {Permission: PermFleetManage, Roles: []authz.Role{authz.RoleEmergencySupervisor}}, {Permission: PermFleetCheck, Roles: operators}}
}
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, transit: deps.Transit}
	g := r.Group("/emergency/bases")
	platform.ListRoute(g, "", PermBaseRead, h.listBases)
	g.GET("/:base/access", PermBaseRead, h.getAccess)
	g.PUT("/:base", PermBaseManage, h.putBase)
	g.PUT("/:base/access/:subject", PermBaseManage, h.putAccess)
	g.DELETE("/:base/access/:subject", PermBaseManage, h.deleteAccess)
	g.PUT("/:base/vehicles/:vehicle", PermFleetManage, h.putVehicle)
	platform.ListRoute(g, "/:base/vehicles", PermFleetRead, h.listVehicles)
	g.PUT("/:base/crew/:crew", PermFleetManage, h.putCrew)
	platform.ListRoute(g, "/:base/crew", PermFleetRead, h.listCrew)
	g.PUT("/:base/duties/:duty", PermFleetManage, h.putDuty)
	g.POST("/:base/duties/:duty/end", PermFleetManage, h.endDuty)
	platform.ListRoute(g, "/:base/duties", PermFleetRead, h.listDuties)
	g.PUT("/:base/readiness/:check", PermFleetCheck, h.putReadiness)
	g.POST("/:base/constraints/:check/:code/clear", PermFleetManage, h.clearConstraint)
	g.PUT("/:base/cases/:case", PermCaseIntake, h.putCase)
	platform.ListRoute(g, "/:base/cases", PermCaseRead, h.listCases)
	g.GET("/:base/cases/:case", PermCaseRead, h.getCase)
	g.POST("/:base/cases/:case/close", PermCaseClose, h.closeCase)
	platform.ListRoute(g, "/:base/cases/:case/timeline", PermCaseRead, h.timeline)
}
func (m *Module) Publishes() []string {
	return []string{emergencycontract.SubjectCaseReceived, emergencycontract.SubjectCaseClosed, emergencycontract.SubjectFleetChanged}
}
func (m *Module) DirectedSubjects() []string                  { return nil }
func (m *Module) DirectedWriteTables() []string               { return nil }
func (m *Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
