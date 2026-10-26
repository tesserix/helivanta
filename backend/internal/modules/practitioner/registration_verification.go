package practitioner

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type registrationVerification struct {
	ID                          uuid.UUID  `json:"id"`
	Action                      string     `json:"action"`
	ExpectedVersion             int64      `json:"expected_version"`
	Version                     int64      `json:"version"`
	RevisionID                  *uuid.UUID `json:"revision_id,omitempty"`
	PolicyID                    *uuid.UUID `json:"policy_id,omitempty"`
	PolicyVersion               *int64     `json:"policy_version,omitempty"`
	ReviewID                    *uuid.UUID `json:"review_id,omitempty"`
	IdentityReviewReference     *uuid.UUID `json:"identity_review_reference,omitempty"`
	UndatedAttestationReference *uuid.UUID `json:"undated_attestation_reference,omitempty"`
	TargetVerificationID        *uuid.UUID `json:"target_verification_id,omitempty"`
	ReasonReference             *uuid.UUID `json:"reason_reference,omitempty"`
	CanonicalValue              *string    `json:"canonical_value,omitempty"`
	SourceCheckedAt             *time.Time `json:"source_checked_at,omitempty"`
	ValidUntil                  *time.Time `json:"valid_until,omitempty"`
	AuthorizedBy                string     `json:"authorized_by"`
	CreatedAt                   time.Time  `json:"created_at"`
	IntentSHA256                string     `json:"-"`
	Method                      string     `json:"method" gorm:"-"`
	ClinicalAuthority           string     `json:"clinical_authority" gorm:"-"`
}

func (r *registrationVerification) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	if r.SourceCheckedAt != nil {
		at := r.SourceCheckedAt.UTC()
		r.SourceCheckedAt = &at
	}
	if r.ValidUntil != nil {
		at := r.ValidUntil.UTC()
		r.ValidUntil = &at
	}
	r.Method = "independent_source_review"
	if r.Action == "revoke" {
		r.Method = "attestation_withdrawal"
	}
	r.ClinicalAuthority = "not_evaluated"
	return nil
}
func (r registrationVerification) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const registrationVerificationColumns = "id,action,expected_version,version,revision_id,policy_id,policy_version,review_id,identity_review_reference,undated_attestation_reference,target_verification_id,reason_reference,canonical_value,source_checked_at,valid_until,created_by AS authorized_by,created_at,intent_sha256"

func registrationVerificationQuery(tx *gorm.DB, p authn.Principal, owner, reg uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_registration_verifications").Select(registrationVerificationColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=?", p.TenantID, owner, reg)
}
func verificationScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	reg, ok := resourceID(c, "registration")
	return p, owner, reg, ok
}
func (h *handlers) auditVerificationAttempt(c *gin.Context, p authn.Principal, owner, reg uuid.UUID) error {
	return h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var row struct{ ID uuid.UUID }
		if err := tx.Table("practitioner_registrations").Select("id").Where("tenant_id=? AND practitioner_id=? AND id=?", p.TenantID, owner, reg).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_registrations", []uuid.UUID{reg})
	})
}
func (h *handlers) verifyRegistration(c *gin.Context) { h.registrationAttestationCommand(c, false) }
func (h *handlers) revokeRegistrationVerification(c *gin.Context) {
	h.registrationAttestationCommand(c, true)
}
func (h *handlers) registrationAttestationCommand(c *gin.Context, revoke bool) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	permission, param, action := PermRegistrationVerify, "verification", "verify"
	if revoke {
		permission, param, action = PermRegistrationVerificationRevoke, "revocation", "revoke"
	}
	id, ok := resourceID(c, param)
	if !ok {
		return
	}
	var revision, policy, review, identity uuid.UUID
	var undated *uuid.UUID
	var expected, policyVersion int64
	var target, reason uuid.UUID
	if revoke {
		var in *struct {
			ExpectedVersion      int64  `json:"expected_version"`
			TargetVerificationID string `json:"target_verification_id"`
			ReasonReference      string `json:"reason_reference"`
		}
		if !decode(c, &in) {
			return
		}
		if in == nil {
			respond.Error(c, 400, "invalid_request", "Expected an explicit attestation withdrawal")
			return
		}
		target, ok = canonicalStewardReference(in.TargetVerificationID)
		reason, ok = parseReferencePair(in.ReasonReference, ok)
		expected = in.ExpectedVersion
	} else {
		revision, ok = resourceID(c, "revision")
		if !ok {
			return
		}
		var in *struct {
			ExpectedVersion             int64   `json:"expected_version"`
			PolicyID                    string  `json:"policy_id"`
			PolicyVersion               int64   `json:"policy_version"`
			ReviewID                    string  `json:"review_id"`
			IdentityReviewReference     string  `json:"identity_review_reference"`
			UndatedAttestationReference *string `json:"undated_attestation_reference"`
		}
		if !decode(c, &in) {
			return
		}
		if in == nil {
			respond.Error(c, 400, "invalid_request", "Expected a policy-bound source attestation")
			return
		}
		expected, policyVersion = in.ExpectedVersion, in.PolicyVersion
		policy, ok = canonicalStewardReference(in.PolicyID)
		review, ok = parseReferencePair(in.ReviewID, ok)
		identity, ok = parseReferencePair(in.IdentityReviewReference, ok)
		if in.UndatedAttestationReference != nil {
			value, valid := canonicalStewardReference(*in.UndatedAttestationReference)
			ok = ok && valid
			undated = &value
		}
		ok = ok && policyVersion >= 1 && policyVersion <= 9007199254740991
	}
	if !ok || expected < 0 || expected > 9007199254740990 {
		respond.Error(c, 400, "invalid_request", "Expected bounded versions and canonical references")
		return
	}
	intent, err := json.Marshal(struct {
		Action                                                                  string
		Owner, Registration, Revision, Policy, Review, Identity, Target, Reason uuid.UUID
		Undated                                                                 *uuid.UUID
		Expected, PolicyVersion                                                 int64
	}{action, owner, reg, revision, policy, review, identity, target, reason, undated, expected, policyVersion})
	if err != nil {
		failure(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(intent))
	if !h.reauthorize(c, permission) {
		return
	}
	if err := h.auditVerificationAttempt(c, p, owner, reg); err != nil {
		failure(c, err)
		return
	}
	if !h.reauthorize(c, permission) {
		return
	}
	var row registrationVerification
	created := false
	err = h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		marker := "app.practitioner_verify"
		if revoke {
			marker = "app.practitioner_verification_revoke"
		}
		if err := tx.Exec("SELECT set_config(?, 'true',true)", marker).Error; err != nil {
			return err
		}
		var head struct{ ID uuid.UUID }
		if err := tx.Raw("SELECT id FROM practitioner_registrations WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, owner, reg).Scan(&head).Error; err != nil {
			return err
		}
		if head.ID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		err := registrationVerificationQuery(tx, p, owner, reg).Where("id=?", id).Take(&row).Error
		if err == nil {
			if row.IntentSHA256 != digest || row.AuthorizedBy != p.Subject {
				return errIntentConflict
			}
			return auditIDs(tx, p, "practitioner_registration_verifications", []uuid.UUID{id})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if revoke {
			err = tx.Exec("INSERT INTO practitioner_registration_verifications(tenant_id,practitioner_id,registration_id,id,action,expected_version,version,target_verification_id,reason_reference,intent_sha256,created_by) VALUES(?,?,?,?,'revoke',?,?,?,?,?,?)", p.TenantID, owner, reg, id, expected, expected+1, target, reason, digest, p.Subject).Error
		} else {
			err = tx.Exec("INSERT INTO practitioner_registration_verifications(tenant_id,practitioner_id,registration_id,id,action,expected_version,version,revision_id,policy_id,policy_version,review_id,identity_review_reference,undated_attestation_reference,intent_sha256,created_by) VALUES(?,?,?,?,'verify',?,?,?,?,?,?,?,?,?,?)", p.TenantID, owner, reg, id, expected, expected+1, revision, policy, policyVersion, review, identity, undated, digest, p.Subject).Error
		}
		if err != nil {
			return err
		}
		if err := registrationVerificationQuery(tx, p, owner, reg).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if err := registrationVerificationEvent(tx, p, owner, reg, id, row.Version); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func parseReferencePair(value string, previous bool) (uuid.UUID, bool) {
	parsed, ok := canonicalStewardReference(value)
	return parsed, previous && ok
}
func (h *handlers) getRegistrationVerification(c *gin.Context) {
	h.getRegistrationVerificationReceipt(c, false)
}
func (h *handlers) getRegistrationVerificationRevocation(c *gin.Context) {
	h.getRegistrationVerificationReceipt(c, true)
}
func (h *handlers) getRegistrationVerificationReceipt(c *gin.Context, revoke bool) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	param := "verification"
	if revoke {
		param = "revocation"
	}
	id, ok := resourceID(c, param)
	if !ok {
		return
	}
	var revision uuid.UUID
	if !revoke {
		revision, ok = resourceID(c, "revision")
		if !ok {
			return
		}
	}
	var row registrationVerification
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		q := registrationVerificationQuery(tx, p, owner, reg).Where("id=?", id)
		if revoke {
			q = q.Where("action='revoke'")
		} else {
			q = q.Where("revision_id=? AND action='verify'", revision)
		}
		if err := q.Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_registration_verifications", []uuid.UUID{id})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listRegistrationVerifications(c *gin.Context, params pagination.Params) ([]registrationVerification, error) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return nil, nil
	}
	var rows []registrationVerification
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var head struct{ ID uuid.UUID }
		if err := tx.Table("practitioner_registrations").Select("id").Where("tenant_id=? AND practitioner_id=? AND id=?", p.TenantID, owner, reg).Take(&head).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_registrations", []uuid.UUID{reg}); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(registrationVerificationQuery(tx, p, owner, reg), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registration_verifications", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) currentRegistrationVerification(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, owner, reg, ok := verificationScope(c)
	if !ok {
		return
	}
	var result struct {
		State             string                    `json:"state"`
		Reason            string                    `json:"reason"`
		Version           int64                     `json:"version"`
		Receipt           *registrationVerification `json:"receipt,omitempty"`
		ClinicalAuthority string                    `json:"clinical_authority"`
	}
	result.State, result.Reason, result.ClinicalAuthority = "unverified", "no_attestation", "not_evaluated"
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, false); err != nil {
			return err
		}
		var head struct {
			ID      uuid.UUID
			Version int64
		}
		if err := tx.Table("practitioner_registrations").Select("id,version").Where("tenant_id=? AND practitioner_id=? AND id=?", p.TenantID, owner, reg).Take(&head).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_registrations", []uuid.UUID{reg}); err != nil {
			return err
		}
		var receipt registrationVerification
		err := registrationVerificationQuery(tx, p, owner, reg).Order("version DESC").Take(&receipt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result.Version = receipt.Version
		result.Receipt = &receipt
		if err := auditIDs(tx, p, "practitioner_registration_verifications", []uuid.UUID{receipt.ID}); err != nil {
			return err
		}
		if receipt.Action == "revoke" {
			result.State, result.Reason = "revoked", "attestation_withdrawn"
			return nil
		}
		result.State, result.Reason = "stale", "source_dependencies_changed"
		var valid bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM practitioner_registration_verifications v JOIN practitioner_registrations g ON g.tenant_id=v.tenant_id AND g.id=v.registration_id JOIN practitioner_registration_revisions r ON r.tenant_id=g.tenant_id AND r.registration_id=g.id AND r.id=v.revision_id AND r.version=g.version JOIN practitioner_registration_policies p ON p.tenant_id=v.tenant_id AND p.id=v.policy_id AND p.version=v.policy_version WHERE v.tenant_id=? AND v.id=? AND v.valid_until>practitioner_registry_now() AND NOT EXISTS(SELECT 1 FROM practitioner_credential_reviews c WHERE c.tenant_id=v.tenant_id AND c.credential_kind='registration' AND c.credential_id=v.registration_id AND c.finding<>'active') AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(v.evidence_snapshot) seal WHERE NOT EXISTS(SELECT 1 FROM practitioner_credential_evidence d WHERE d.tenant_id=v.tenant_id AND d.practitioner_id=v.practitioner_id AND d.credential_kind='registration' AND d.credential_id=v.registration_id AND d.revision_id=v.revision_id AND d.id=(seal->>'id')::uuid AND d.status='available')))`, p.TenantID, receipt.ID).Scan(&valid).Error; err != nil {
			return err
		}
		if valid {
			result.State, result.Reason = "verified", "independent_source_review"
		}
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, result)
}

func registrationVerificationEvent(tx *gorm.DB, p authn.Principal, owner, reg, id uuid.UUID, version int64) error {
	data, err := json.Marshal(practitionercontract.RegistrationVerificationChangedData{PractitionerID: owner.String(), RegistrationID: reg.String(), VerificationID: id.String(), Version: version})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, practitionercontract.RegistrationVerificationChanged, events.Event{Type: practitionercontract.RegistrationVerificationChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
