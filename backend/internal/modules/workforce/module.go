// Package workforce owns hospital-local staff directories and explicit access.
package workforce

import (
	_ "embed"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermHospitalManage authz.Permission = "workforce.hospital.manage"
const PermEnrollmentManage authz.Permission = "workforce.enrollment.manage"
const PermDocumentManage authz.Permission = "workforce.documents.manage"
const PermRosterManage authz.Permission = "workforce.roster.manage"
const RosterConfigChanged = workforcecontract.SubjectRosterConfigChanged
const DocumentScanRequested = workforcecontract.SubjectDocumentScanRequested
const DocumentChanged = workforcecontract.SubjectDocumentChanged
const ShiftChanged = workforcecontract.SubjectShiftChanged

const PermMasterManage authz.Permission = "workforce.master.manage"
const EnrollmentChanged = workforcecontract.SubjectStaffEnrollmentChanged
const MasterChanged = workforcecontract.SubjectMasterChanged

//go:embed schema.sql
var schema string

//go:embed enrollment.sql
var enrollmentSchema string

//go:embed staff_code.sql
var staffCodeSchema string

//go:embed masters.sql
var mastersSchema string

//go:embed shifts.sql
var shiftsSchema string

//go:embed roster_config.sql
var rosterConfigSchema string

//go:embed documents.sql
var documentsSchema string

//go:embed document_worker.sql
var documentWorkerSchema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "workforce" }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_workforce", SQL: schema}, {ID: "0002_workforce_enrollment", SQL: enrollmentSchema}, {ID: "0003_workforce_staff_code_casefold", SQL: staffCodeSchema}, {ID: "0004_workforce_hr_masters", SQL: mastersSchema}, {ID: "0005_workforce_shifts", SQL: shiftsSchema}, {ID: "0006_workforce_roster_config", SQL: rosterConfigSchema}, {ID: "0007_workforce_documents", SQL: documentsSchema}, {ID: "0008_workforce_document_worker", SQL: documentWorkerSchema}}
}
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermHospitalManage}, {Permission: PermEnrollmentManage}, {Permission: PermDocumentManage}, {Permission: PermMasterManage}, {Permission: PermRosterManage}}
}
func (*Module) Publishes() []string {
	return []string{EnrollmentChanged, MasterChanged, ShiftChanged, RosterConfigChanged, DocumentScanRequested, DocumentChanged}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(deps platform.Deps) []events.Consumer {
	if deps.Workflows == nil {
		return nil
	}
	return []events.Consumer{DocumentScanConsumer(deps.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, bus: deps.Bus, documents: deps.Documents}
	g := r.Group("/workforce/hospitals")
	platform.ListRoute(g, "", authz.Public, h.listHospitals)
	g.PUT("/:hospital", PermHospitalManage, h.putHospital)
	g.PUT("/:hospital/access/:subject", PermHospitalManage, h.putAccess)
	g.DELETE("/:hospital/access/:subject", PermHospitalManage, h.deleteAccess)
	// Public still requires authentication and current tenant membership.
	// These handlers additionally check hospital grants inside the transaction.
	g.PUT("/:hospital/staff/:id", authz.Public, h.putStaff)
	g.GET("/:hospital/staff/:id", authz.Public, h.getStaff)
	g.GET("/:hospital/staff/:id/enrollment", PermEnrollmentManage, h.getEnrollment)
	g.GET("/:hospital/staff/:id/document-records", PermDocumentManage, h.documentRecords)
	g.PUT("/:hospital/staff/:id/enrollment", PermEnrollmentManage, h.putEnrollment)
	g.PUT("/:hospital/staff/:id/documents/:file", PermDocumentManage, h.putDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/content", PermDocumentManage, h.uploadDocument)
	g.GET("/:hospital/staff/:id/documents/:file/content", PermDocumentManage, h.downloadDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/revocation", PermDocumentManage, h.revokeDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/retries/:attempt", PermDocumentManage, h.retryDocument)
	platform.ListRoute(g, "/:hospital/staff/:id/documents", PermDocumentManage, h.listDocuments)
	g.PUT("/:hospital/hr/roster-configs/:id", PermRosterManage, h.putRosterConfig)
	platform.ListRoute(g, "/:hospital/hr/roster-configs", authz.Public, h.listRosterConfigs)
	platform.ListRoute(g, "/:hospital/hr/roster-configs/:id/history", authz.Public, h.rosterConfigHistory)
	g.PUT("/:hospital/hr/shifts/:id", PermRosterManage, h.putShift)
	platform.ListRoute(g, "/:hospital/hr/shifts", authz.Public, h.listShifts)
	platform.ListRoute(g, "/:hospital/hr/shifts/:id/history", authz.Public, h.shiftHistory)
	g.PUT("/:hospital/hr/masters/:id", PermMasterManage, h.putMaster)
	platform.ListRoute(g, "/:hospital/hr/masters", authz.Public, h.listMasters)
	platform.ListRoute(g, "/:hospital/hr/masters/:id/history", authz.Public, h.masterHistory)
	platform.ListRoute(g, "/:hospital/staff", authz.Public, h.listStaff)
}
