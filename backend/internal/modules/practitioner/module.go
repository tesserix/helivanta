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

const PermRegistrationVerify authz.Permission = "practitioner.registration.verify"
const PermRegistrationVerificationRevoke authz.Permission = "practitioner.registration.verification_revoke"

const PermRegistrationPolicyManage authz.Permission = "practitioner.registration_policy.manage"
const PermRegistrationPolicyRead authz.Permission = "practitioner.registration_policy.read"

const PermQualificationReview authz.Permission = "practitioner.qualification.review"

const PermSpecialtyManage authz.Permission = "practitioner.specialty.manage"

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

//go:embed qualifications.sql
var qualificationsSchema string

//go:embed specialty_retirement.sql
var specialtyRetirementSchema string

//go:embed credential_evidence.sql
var credentialEvidenceSchema string

//go:embed registration_policy.sql
var registrationPolicySchema string

//go:embed registration_verification.sql
var registrationVerificationSchema string

//go:embed registration_reconciliation.sql
var registrationReconciliationSchema string

//go:embed registration_reconciliation_withdrawal.sql
var registrationReconciliationWithdrawalSchema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "practitioner" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermRegistrationVerify}, {Permission: PermRegistrationVerificationRevoke}, {Permission: PermRegistrationPolicyManage}, {Permission: PermRegistrationPolicyRead}, {Permission: PermQualificationReview}, {Permission: PermSpecialtyManage}, {Permission: PermRegistrationReview}, {Permission: PermRecordManage}, {Permission: PermRecordRead}, {Permission: PermStewardManage}, {Permission: PermEvidenceManage}, {Permission: PermEvidenceRead}}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_practitioner", SQL: recordsSchema}, {ID: "0002_practitioner", SQL: registrationsSchema}, {ID: "0003_practitioner", SQL: stewardsSchema}, {ID: "0004_practitioner", SQL: evidenceSchema}, {ID: "0005_practitioner", SQL: evidenceUploadSchema}, {ID: "0006_practitioner", SQL: evidenceProcessingSchema}, {ID: "0007_practitioner", SQL: evidenceReleaseSchema}, {ID: "0008_practitioner", SQL: evidenceRetrySchema}, {ID: "0009_practitioner", SQL: reviewsSchema}, {ID: "0010_practitioner", SQL: qualificationsSchema}, {ID: "0011_practitioner", SQL: specialtyRetirementSchema}, {ID: "0012_practitioner", SQL: credentialEvidenceSchema}, {ID: "0013_practitioner", SQL: registrationPolicySchema}, {ID: "0014_practitioner", SQL: registrationVerificationSchema}, {ID: "0015_practitioner", SQL: registrationReconciliationSchema}, {ID: "0016_practitioner", SQL: registrationReconciliationWithdrawalSchema}}
}
func (*Module) Routes(r *platform.Router, d platform.Deps) {
	h := &handlers{db: d.DB, files: d.Documents, reauthorize: r.Reauthorize}
	g := r.Group("/practitioners")
	g.PUT("/:practitioner/registrations/:registration/source-reconciliations/:reconciliation/withdrawals/:withdrawal", PermRegistrationVerificationRevoke, h.withdrawRegistrationReconciliation)
	g.GET("/:practitioner/registrations/:registration/source-reconciliations/:reconciliation/withdrawals/:withdrawal", PermRecordRead, h.getRegistrationReconciliationWithdrawal)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/source-reconciliations/:reconciliation/withdrawals", PermRecordRead, h.listRegistrationReconciliationWithdrawals)
	g.PUT("/:practitioner/registrations/:registration/source-reconciliations/:reconciliation", PermRegistrationVerify, h.reconcileRegistrationSource)
	g.GET("/:practitioner/registrations/:registration/source-reconciliations/:reconciliation", PermRecordRead, h.getRegistrationReconciliation)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/source-reconciliations", PermRecordRead, h.listRegistrationReconciliations)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/verifications/:verification", PermRegistrationVerify, h.verifyRegistration)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision/verifications/:verification", PermRecordRead, h.getRegistrationVerification)
	g.GET("/:practitioner/registrations/:registration/verification-revocations/:revocation", PermRecordRead, h.getRegistrationVerificationRevocation)
	g.PUT("/:practitioner/registrations/:registration/verification-revocations/:revocation", PermRegistrationVerificationRevoke, h.revokeRegistrationVerification)
	g.GET("/:practitioner/registrations/:registration/verification", PermRecordRead, h.currentRegistrationVerification)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/verifications", PermRecordRead, h.listRegistrationVerifications)

	g.PUT("/registration-policies/:policy", PermRegistrationPolicyManage, h.putRegistrationPolicy)
	g.GET("/registration-policies/:policy", PermRegistrationPolicyRead, h.getRegistrationPolicy)
	platform.ListRoute(g, "/registration-policies", PermRegistrationPolicyRead, h.listRegistrationPolicies)
	g.PUT("/registration-policies/:policy/revisions/:revision", PermRegistrationPolicyManage, h.reviseRegistrationPolicy)
	g.GET("/registration-policies/:policy/revisions/:revision", PermRegistrationPolicyRead, h.getRegistrationPolicyRevision)
	platform.ListRoute(g, "/registration-policies/:policy/revisions", PermRegistrationPolicyRead, h.listRegistrationPolicyRevisions)

	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file", PermEvidenceManage, h.putEvidence)
	g.GET("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file", PermEvidenceRead, h.getEvidence)
	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file/content", PermEvidenceManage, h.uploadEvidence)
	g.GET("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file/content", PermEvidenceRead, h.downloadEvidence)
	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file/revocations/:revocation", PermEvidenceManage, h.revokeEvidence)
	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision/evidence/:file/scan-retries/:retry", PermEvidenceManage, h.retryEvidence)
	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision/reviews/:review", PermQualificationReview, h.putCredentialReview)
	g.GET("/:practitioner/qualifications/:qualification/revisions/:revision/reviews/:review", PermRecordRead, h.getCredentialReview)
	platform.ListRoute(g, "/:practitioner/qualifications/:qualification/revisions/:revision/evidence", PermEvidenceRead, h.listEvidence)
	platform.ListRoute(g, "/:practitioner/qualifications/:qualification/revisions/:revision/reviews", PermRecordRead, h.listCredentialReviews)

	g.PUT("/specialties/:specialty/retirements/:retirement", PermSpecialtyManage, h.retireSpecialty)
	g.GET("/specialties/:specialty/retirements/:retirement", PermRecordRead, h.getSpecialtyRetirement)
	g.PUT("/specialties/:specialty", PermSpecialtyManage, h.putSpecialty)
	g.GET("/specialties/:specialty", PermRecordRead, h.getSpecialty)
	platform.ListRoute(g, "/specialties", PermRecordRead, h.listSpecialties)
	g.PUT("/:practitioner/qualifications/:qualification", PermRecordManage, h.putQualification)
	g.GET("/:practitioner/qualifications/:qualification", PermRecordRead, h.getQualification)
	platform.ListRoute(g, "/:practitioner/qualifications", PermRecordRead, h.listQualifications)
	g.PUT("/:practitioner/qualifications/:qualification/revisions/:revision", PermRecordManage, h.correctQualification)
	g.GET("/:practitioner/qualifications/:qualification/revisions/:revision", PermRecordRead, h.getQualificationRevision)
	platform.ListRoute(g, "/:practitioner/qualifications/:qualification/revisions", PermRecordRead, h.listQualificationRevisions)

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
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision/reviews/:review", PermRegistrationReview, h.putCredentialReview)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision/reviews/:review", PermRecordRead, h.getCredentialReview)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/revisions/:revision/reviews", PermRecordRead, h.listCredentialReviews)
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
	return []string{EvidenceScanRequested, practitionercontract.EvidenceChanged, practitionercontract.ReviewRecorded, practitionercontract.QualificationReviewRecorded, practitionercontract.RegistrationPolicyChanged, practitionercontract.RegistrationVerificationChanged, practitionercontract.RegistrationSourceReconciled, practitionercontract.RegistrationReconciliationWithdrawn}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(d platform.Deps) []events.Consumer {
	return []events.Consumer{EvidenceScanConsumer(d.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
