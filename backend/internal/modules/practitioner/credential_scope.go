package practitioner

import (
	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func scanCredential(in practitionercontract.EvidenceScanInput) (string, string, bool) {
	if in.RegistrationID != "" && in.QualificationID == "" {
		return "registration", in.RegistrationID, true
	}
	if in.QualificationID != "" && in.RegistrationID == "" {
		return "qualification", in.QualificationID, true
	}
	return "", "", false
}
func evidenceScanInput(tenant string, parent evidenceParent, file uuid.UUID, attempt int64) practitionercontract.EvidenceScanInput {
	in := practitionercontract.EvidenceScanInput{TenantID: tenant, PractitionerID: parent.practitioner.String(), RevisionID: parent.revision.String(), FileID: file.String(), ScanAttempt: attempt}
	switch parent.kind {
	case "registration":
		in.RegistrationID = parent.credential.String()
	case "qualification":
		in.QualificationID = parent.credential.String()
	}
	return in
}
func evidenceScanData(in practitionercontract.EvidenceScanInput) practitionercontract.EvidenceScanRequestedData {
	return practitionercontract.EvidenceScanRequestedData{PractitionerID: in.PractitionerID, RegistrationID: in.RegistrationID, QualificationID: in.QualificationID, RevisionID: in.RevisionID, FileID: in.FileID, ScanAttempt: in.ScanAttempt}
}
func credentialTables(kind string) (head, revision, column string) {
	if kind == "registration" {
		return "practitioner_registrations", "practitioner_registration_revisions", "registration_id"
	}
	if kind == "qualification" {
		return "practitioner_qualifications", "practitioner_qualification_revisions", "qualification_id"
	}
	return "", "", ""
}
func credentialRevisionVersion(tx *gorm.DB, p authn.Principal, parent evidenceParent) (int64, error) {
	_, table, column := credentialTables(parent.kind)
	if table == "" {
		return 0, gorm.ErrRecordNotFound
	}
	var row struct{ Version int64 }
	err := tx.Table(table).Select("version").Where("tenant_id=? AND practitioner_id=? AND "+column+"=? AND id=?", p.TenantID, parent.practitioner, parent.credential, parent.revision).Take(&row).Error
	return row.Version, err
}
func credentialRevisionAudit(tx *gorm.DB, p authn.Principal, parent evidenceParent) error {
	_, table, _ := credentialTables(parent.kind)
	if table == "" {
		return gorm.ErrRecordNotFound
	}
	return auditIDs(tx, p, table, []uuid.UUID{parent.revision})
}

func credentialReviewPermission(kind string) authz.Permission {
	if kind == "qualification" {
		return PermQualificationReview
	}
	return PermRegistrationReview
}
