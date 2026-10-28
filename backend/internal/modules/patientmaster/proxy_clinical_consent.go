package patientmaster

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/countryprofile"
)

var errProxyClinicalCaptureDisabled = errors.New("proxy clinical capture disabled")

type clinicalAuthoritySource struct {
	ID                    uuid.UUID
	PatientID             uuid.UUID
	ReviewRequestID       uuid.UUID
	DecisionID            uuid.UUID
	Subject               string
	Status                string
	Kind                  string
	Version               int64
	ValidUntil            time.Time
	RegistrationUpdatedAt time.Time
}

func clinicalClaimRelationship(kind string) string {
	switch kind {
	case "parental":
		return "child"
	case "appointed_guardian":
		return "ward"
	default:
		// Omit the field for self to preserve the legacy private request.
		return ""
	}
}

func clinicalSourceAuthority(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, proxy bool) (clinicalAuthoritySource, error) {
	var out clinicalAuthoritySource
	if !proxy {
		err := tx.Table("patient_consent_authorities").Select("id,patient_id,review_request_id,decision_id,subject,status,version,valid_until,registration_updated_at").Where("tenant_id=? AND hospital_id=? AND id=? AND subject=?", p.TenantID, hospital, id, p.Subject).Take(&out).Error
		return out, err
	}
	if err := tx.Exec(`SELECT set_config('app.patient_proxy_authority',?,true)`, id).Error; err != nil {
		return out, err
	}
	var source proxyAuthority
	if err := readProxyAuthority(tx.Clauses(clause.Locking{Strength: "SHARE"}), p.TenantID, hospital, id, &source, false); err != nil {
		return out, err
	}
	if source.Subject != p.Subject {
		return out, gorm.ErrRecordNotFound
	}
	var now time.Time
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
		return out, err
	}
	if source.Status != "accepted" || source.Version != 2 || !source.ValidUntil.After(now) {
		return out, errClinicalConflict
	}
	out = clinicalAuthoritySource{ID: source.ID, PatientID: source.PatientID, ReviewRequestID: source.ReviewRequestID, DecisionID: source.DecisionID, Subject: source.Subject, Status: source.Status, Kind: source.Kind, Version: source.Version, ValidUntil: source.ValidUntil, RegistrationUpdatedAt: source.RegistrationUpdatedAt}
	var registration struct {
		UpdatedAt    time.Time
		DOB          time.Time
		DOBEstimated bool
	}
	if err := tx.Table("patients").Clauses(clause.Locking{Strength: "SHARE"}).Select("updated_at,dob,dob_estimated").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, source.PatientID).Take(&registration).Error; err != nil {
		return out, err
	}
	if !registration.UpdatedAt.Equal(source.RegistrationUpdatedAt) {
		return out, errClinicalConflict
	}
	if source.Kind == "parental" {
		profile, ok := countryprofile.Lookup(source.CountryCode)
		if !ok || registration.DOBEstimated || source.ParentalEndAt == nil {
			return out, errClinicalConflict
		}
		end, err := proxyParentalEnd(registration.DOB, profile.Timezone)
		if err != nil {
			return out, err
		}
		if !end.Equal(*source.ParentalEndAt) {
			return out, errClinicalConflict
		}
	}
	if err := patientAudit(tx, p, hospital, "read", []patient{{ID: source.PatientID}}); err != nil {
		return out, err
	}
	return out, proxyAuthorityReadAudit(tx, p, source)
}

func clinicalProxyRecipientState(tx *gorm.DB, p authn.Principal, row clinicalConsent, body clinicalConsentBody) (bool, error) {
	var count int64
	err := tx.Raw(`SELECT count(*) FROM patient_proxy_authorities a
 JOIN patient_link_reviews r ON r.tenant_id=a.tenant_id AND r.request_id=a.review_request_id
 JOIN patients p ON p.tenant_id=a.tenant_id AND p.hospital_id=a.hospital_id AND p.id=a.patient_id
 JOIN patient_sharing_notices n ON n.tenant_id=a.tenant_id AND n.hospital_id=a.hospital_id AND n.id=?
 JOIN patient_sharing_recipients g ON g.tenant_id=a.tenant_id AND g.hospital_id=a.hospital_id AND g.id=?
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.id=? AND a.subject=? AND a.patient_id=? AND a.kind=?
 AND a.status='accepted' AND a.version=? AND a.valid_until>clock_timestamp()
 AND a.decision_id=? AND a.registration_updated_at=? AND p.updated_at=a.registration_updated_at
 AND r.status='approved' AND r.decision_id=a.decision_id AND r.requester_subject=a.subject AND r.hospital_id=a.hospital_id AND r.patient_id=a.patient_id AND r.registration_updated_at=a.registration_updated_at
 AND ((a.kind='parental' AND r.relationship='child' AND NOT p.dob_estimated AND a.parental_end_at=((p.dob+interval '18 years')::date::timestamp AT TIME ZONE a.time_zone) AND a.parental_end_at>clock_timestamp()) OR (a.kind='appointed_guardian' AND r.relationship='ward'))
 AND n.status='approved' AND n.version=? AND n.content_hash=? AND n.authority_kind=a.kind
 AND g.status='approved' AND g.version=? AND g.grantee_subject=? AND g.valid_until>clock_timestamp()`, row.NoticeID, row.RecipientID, p.TenantID, row.HospitalID, row.AuthorityID, row.Subject, row.PatientID, body.AuthorityKind, body.AuthorityVersion, body.DecisionID, body.RegistrationUpdatedAt, body.NoticeVersion, body.NoticeHash, body.RecipientVersion, p.Subject).Scan(&count).Error
	return count == 1, err
}
