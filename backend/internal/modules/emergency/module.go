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
	PermReceivingManage authz.Permission = "emergency.receiving.manage"
	PermReceivingRead   authz.Permission = "emergency.receiving.read"
	PermReceivingAct    authz.Permission = "emergency.receiving.act"
	PermBaseRead        authz.Permission = "emergency.base.read"
	PermBaseManage      authz.Permission = "emergency.base.manage"
	PermCaseRead        authz.Permission = "emergency.case.read"
	PermCaseIntake      authz.Permission = "emergency.case.intake"
	PermCaseClose       authz.Permission = "emergency.case.close"
	PermFleetRead       authz.Permission = "emergency.fleet.read"
	PermFleetManage     authz.Permission = "emergency.fleet.manage"
	PermFleetCheck      authz.Permission = "emergency.fleet.check"
	PermClinicalManage  authz.Permission = "emergency.clinical.manage"
	PermCaseAssess      authz.Permission = "emergency.case.assess"
	PermDispatchManage  authz.Permission = "emergency.dispatch.manage"
	PermCrewRead        authz.Permission = "emergency.crew.read"
	PermCrewAct         authz.Permission = "emergency.crew.act"
)

//go:embed schema.sql
var schema string

//go:embed fleet.sql
var fleetSchema string

//go:embed transport_review.sql
var transportReviewSchema string

//go:embed dispatch.sql
var dispatchSchema string

//go:embed dispatch_worker.sql
var dispatchWorkerSchema string

//go:embed receiving.sql
var receivingSchema string

//go:embed contract/intake.json
var intakeCatalogue string

type Module struct{}

func New() *Module             { return &Module{} }
func (m *Module) Name() string { return "emergency" }
func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_emergency", SQL: schema}, {ID: "0002_emergency", SQL: fleetSchema}, {ID: "0003_emergency", SQL: transportReviewSchema}, {ID: "0004_emergency", SQL: dispatchContractSchema + "\n" + dispatchSchema}, {ID: "0005_emergency", SQL: dispatchWorkerSchema}, {ID: "0006_emergency", SQL: receivingSchema}}
}
func (m *Module) Permissions() []authz.Grant {
	operators := []authz.Role{authz.RoleEmergencyDispatcher, authz.RoleEmergencySupervisor}
	readers := []authz.Role{authz.RoleEmergencyDispatcher, authz.RoleEmergencySupervisor, authz.RoleDoctor}
	return []authz.Grant{{Permission: PermReceivingManage}, {Permission: PermReceivingRead, Roles: []authz.Role{authz.RoleEmergencyReceiver}}, {Permission: PermReceivingAct, Roles: []authz.Role{authz.RoleEmergencyReceiver}}, {Permission: PermCrewRead, Roles: []authz.Role{authz.RoleEmergencyCrew}}, {Permission: PermCrewAct, Roles: []authz.Role{authz.RoleEmergencyCrew}}, {Permission: PermBaseRead, Roles: readers}, {Permission: PermBaseManage}, {Permission: PermCaseRead, Roles: readers}, {Permission: PermCaseIntake, Roles: operators}, {Permission: PermDispatchManage, Roles: operators}, {Permission: PermCaseClose, Roles: []authz.Role{authz.RoleEmergencySupervisor}}, {Permission: PermFleetRead, Roles: operators}, {Permission: PermFleetManage, Roles: []authz.Role{authz.RoleEmergencySupervisor}}, {Permission: PermFleetCheck, Roles: operators}, {Permission: PermClinicalManage}, {Permission: PermCaseAssess, Roles: []authz.Role{authz.RoleDoctor}}}
}
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, transit: deps.Transit, dispatchEnabled: deps.EmergencyDispatchEnabled}
	receiving := r.Group("/emergency/receiving/facilities")
	platform.ListRoute(receiving, "", PermReceivingRead, h.listReceivingFacilities)
	platform.ListRoute(receiving, "/:facility/links", PermReceivingRead, h.listReceivingLinks)
	platform.ListRoute(receiving, "/:facility/requests", PermReceivingRead, h.listReceivingRequests)
	receiving.PUT("/:facility", PermReceivingManage, h.putReceivingFacility)
	receiving.GET("/:facility", PermReceivingRead, h.getReceivingFacility)
	receiving.GET("/:facility/requests/:provider/:request", PermReceivingRead, h.getReceivingRequest)
	receiving.PUT("/:facility/requests/:provider/:request/commands/:command", PermReceivingAct, h.putReceivingCommand)
	receiving.GET("/:facility/links/:provider/:link", PermReceivingRead, h.getReceivingLink)
	receiving.PUT("/:facility/links/:provider/:link/commands/:command", PermReceivingAct, h.putReceivingLinkCommand)
	receiving.PUT("/:facility/access/:subject", PermReceivingManage, h.putReceivingAccess)
	receiving.GET("/:facility/access/:subject", PermReceivingRead, h.getReceivingAccess)
	platform.ListRoute(r.Group("/emergency/crew"), "/assignments", PermCrewRead, h.listCrewAssignments)
	crew := r.Group("/emergency/crew/bases")
	crew.GET("/:base/dispatches/:dispatch", PermCrewRead, h.getCrewDispatch)
	platform.ListRoute(crew, "/:base/dispatches/:dispatch/receiving", PermCrewRead, h.listReceivingRequests)
	crew.GET("/:base/dispatches/:dispatch/receiving/:request", PermCrewRead, h.getReceivingRequest)
	crew.GET("/:base/dispatches/:dispatch/incident", PermCrewRead, h.getCrewIncident)
	crew.PUT("/:base/dispatches/:dispatch/commands/:command", PermCrewAct, h.putDispatchCommand)
	g := r.Group("/emergency/bases")
	platform.ListRoute(g, "", PermBaseRead, h.listBases)
	g.GET("/:base/access", PermBaseRead, h.getAccess)
	g.PUT("/:base", PermBaseManage, h.putBase)
	g.PUT("/:base/access/:subject", PermBaseManage, h.putAccess)
	g.DELETE("/:base/access/:subject", PermBaseManage, h.deleteAccess)
	g.GET("/:base/cases/:case/dispatch-eligibility", PermDispatchManage, h.getDispatchEligibility)
	g.GET("/:base/dispatches/:dispatch", PermCaseRead, h.getDispatch)
	platform.ListRoute(g, "/:base/dispatches", PermCaseRead, h.listDispatches)
	platform.ListRoute(g, "/:base/cases/:case/dispatches", PermCaseRead, h.listDispatches)
	g.PUT("/:base/dispatches/:dispatch", PermDispatchManage, h.putDispatch)
	g.PUT("/:base/dispatches/:dispatch/receiving/:request", PermDispatchManage, h.putReceivingRequest)
	g.GET("/:base/dispatches/:dispatch/receiving/:request", PermDispatchManage, h.getReceivingRequest)
	platform.ListRoute(g, "/:base/receiving-links", PermDispatchManage, h.listReceivingLinks)
	platform.ListRoute(g, "/:base/dispatches/:dispatch/receiving", PermDispatchManage, h.listReceivingRequests)
	g.PUT("/:base/receiving-links/:link", PermDispatchManage, h.putReceivingLink)
	g.GET("/:base/receiving-links/:link", PermDispatchManage, h.getReceivingLink)
	g.PUT("/:base/receiving-links/:link/commands/:command", PermDispatchManage, h.putReceivingLinkCommand)
	g.PUT("/:base/dispatches/:dispatch/commands/:command", PermDispatchManage, h.putDispatchCommand)
	g.PUT("/:base/vehicles/:vehicle", PermFleetManage, h.putVehicle)
	platform.ListRoute(g, "/:base/vehicles", PermFleetRead, h.listVehicles)
	g.PUT("/:base/crew/:crew", PermFleetManage, h.putCrew)
	platform.ListRoute(g, "/:base/crew", PermFleetRead, h.listCrew)
	g.PUT("/:base/duties/:duty", PermFleetManage, h.putDuty)
	g.POST("/:base/duties/:duty/end", PermFleetManage, h.endDuty)
	platform.ListRoute(g, "/:base/duties", PermFleetRead, h.listDuties)
	g.PUT("/:base/readiness/:check", PermFleetCheck, h.putReadiness)
	g.POST("/:base/constraints/:check/:code/clear", PermFleetManage, h.clearConstraint)
	g.PUT("/:base/clinical-reviewers/:subject", PermClinicalManage, h.putClinicalReviewer)
	g.GET("/:base/clinical-reviewers/:subject", PermClinicalManage, h.getClinicalReviewer)
	g.GET("/:base/clinical-reviewer", PermCaseAssess, h.getClinicalReviewer)
	g.PUT("/:base/cases/:case/assessments/:assessment", PermCaseAssess, h.putTransportAssessment)
	platform.ListRoute(g, "/:base/cases/:case/assessments", PermCaseRead, h.listTransportAssessments)
	g.PUT("/:base/cases/:case", PermCaseIntake, h.putCase)
	platform.ListRoute(g, "/:base/cases", PermCaseRead, h.listCases)
	g.GET("/:base/cases/:case", PermCaseRead, h.getCase)
	g.POST("/:base/cases/:case/close", PermCaseClose, h.closeCase)
	platform.ListRoute(g, "/:base/cases/:case/timeline", PermCaseRead, h.timeline)
}
func (m *Module) Publishes() []string {
	return []string{emergencycontract.SubjectCaseReceived, emergencycontract.SubjectCaseClosed, emergencycontract.SubjectFleetChanged, emergencycontract.SubjectClinicalAccessChanged, emergencycontract.SubjectCaseAssessed, emergencycontract.SubjectDispatchChanged, emergencycontract.SubjectReceivingChanged}
}
func (m *Module) DirectedSubjects() []string {
	return []string{emergencycontract.SubjectReceivingChanged}
}
func (m *Module) DirectedWriteTables() []string { return nil }
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	if deps.Workflows == nil || (!deps.EmergencyWorkflowsEnabled && !deps.EmergencyDispatchEnabled) {
		return nil
	}
	return []events.Consumer{DispatchConsumer(deps.Workflows), ReceivingConsumer(deps.Workflows)}
}
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
