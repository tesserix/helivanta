// Package practitioner owns tenant-scoped professional identity, separate from HR.
package practitioner

import (
	_ "embed"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermEvidenceManage authz.Permission = "practitioner.evidence.manage"
const PermEvidenceRead authz.Permission = "practitioner.evidence.read"

const PermStewardManage authz.Permission = "practitioner.steward.manage"

const PermRegistrationReview authz.Permission = "practitioner.registration.review"

const PermRecordManage authz.Permission = "practitioner.record.manage"
const PermRecordRead authz.Permission = "practitioner.record.read"

//go:embed records.sql
var recordsSchema string

//go:embed registrations.sql
var registrationsSchema string

//go:embed stewards.sql
var stewardsSchema string

//go:embed evidence.sql
var evidenceSchema string

//go:embed evidence_upload.sql
var evidenceUploadSchema string

//go:embed evidence_processing.sql
var evidenceProcessingSchema string

//go:embed evidence_release.sql
var evidenceReleaseSchema string

//go:embed evidence_retry.sql
var evidenceRetrySchema string

//go:embed reviews.sql
var reviewsSchema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "practitioner" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermRegistrationReview}, {Permission: PermRecordManage}, {Permission: PermRecordRead}, {Permission: PermStewardManage}, {Permission: PermEvidenceManage}, {Permission: PermEvidenceRead}}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_practitioner", SQL: recordsSchema}, {ID: "0002_practitioner", SQL: registrationsSchema}, {ID: "0003_practitioner", SQL: stewardsSchema}, {ID: "0004_practitioner", SQL: evidenceSchema}, {ID: "0005_practitioner", SQL: evidenceUploadSchema}, {ID: "0006_practitioner", SQL: evidenceProcessingSchema}, {ID: "0007_practitioner", SQL: evidenceReleaseSchema}, {ID: "0008_practitioner", SQL: evidenceRetrySchema}, {ID: "0009_practitioner", SQL: reviewsSchema}}
}
func (*Module) Routes(r *platform.Router, d platform.Deps) {
	h := &handlers{db: d.DB, files: d.Documents, reauthorize: r.Reauthorize}
	g := r.Group("/practitioners")
	g.PUT("/steward-appointments/:appointment", PermStewardManage, h.nominateSteward)
	g.GET("/steward-appointments/:appointment", PermStewardManage, h.getManagedSteward)
	g.POST("/steward-appointments/:appointment/revoke", PermStewardManage, h.revokeSteward)
	platform.ListRoute(g, "/steward-appointments", PermStewardManage, h.listManagedStewards)
	platform.ListRoute(g, "/steward-appointments/:appointment/history", PermStewardManage, h.managedStewardHistory)
	g.GET("/my/steward-appointments/:appointment", authz.Public, h.getOwnSteward)
	g.POST("/my/steward-appointments/:appointment/accept", authz.Public, h.acceptSteward)
	g.POST("/my/steward-appointments/:appointment/withdraw", authz.Public, h.withdrawSteward)
	platform.ListRoute(g, "/my/steward-appointments", authz.Public, h.listOwnStewards)
	platform.ListRoute(g, "/my/steward-appointments/:appointment/history", authz.Public, h.ownStewardHistory)
	g.GET("/my/steward-appointment", authz.Public, h.currentSteward)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/reviews/:review", PermRegistrationReview, h.putRegistrationReview)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision/reviews/:review", PermRecordRead, h.getRegistrationReview)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/revisions/:revision/reviews", PermRecordRead, h.listRegistrationReviews)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file", PermEvidenceManage, h.putEvidence)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file/scan-retries/:retry", PermEvidenceManage, h.retryEvidence)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file/content", PermEvidenceRead, h.downloadEvidence)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file/revocations/:revocation", PermEvidenceManage, h.revokeEvidence)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file/content", PermEvidenceManage, h.uploadEvidence)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision/evidence/:file", PermEvidenceRead, h.getEvidence)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/revisions/:revision/evidence", PermEvidenceRead, h.listEvidence)
	g.PUT("/:practitioner", PermRecordManage, h.put)
	g.GET("/:practitioner", PermRecordRead, h.get)
	g.PUT("/:practitioner/registrations/:registration", PermRecordManage, h.putRegistration)
	g.GET("/:practitioner/registrations/:registration", PermRecordRead, h.getRegistration)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision", PermRecordManage, h.putRegistrationRevision)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision", PermRecordRead, h.getRegistrationRevision)
	platform.ListRoute(g, "/:practitioner/registrations", PermRecordRead, h.listRegistrations)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/revisions", PermRecordRead, h.listRegistrationRevisions)
}
func (*Module) Publishes() []string {
	return []string{EvidenceScanRequested, practitionercontract.EvidenceChanged, practitionercontract.ReviewRecorded}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(d platform.Deps) []events.Consumer {
	return []events.Consumer{EvidenceScanConsumer(d.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
