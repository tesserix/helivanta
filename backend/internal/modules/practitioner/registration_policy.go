package practitioner

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	_ "time/tzdata"

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

var errPolicyIntent = errors.New("retained registration policy intent differs")
var errPolicyVersion = errors.New("registration policy version differs")

// RegistrationPolicyIdentity fixes issuer interpretation independently of lease editions.
type RegistrationPolicyIdentity struct {
	SourceSystem   string `json:"source_system"`
	Normalization  string `json:"normalization"`
	ExpiryTimeZone string `json:"expiry_time_zone"`
}

func (i RegistrationPolicyIdentity) valid() bool {
	if len(i.SourceSystem) > 63 || !councilSystem.MatchString(i.SourceSystem) || i.Normalization != "exact" && i.Normalization != "ascii_upper" || len(i.ExpiryTimeZone) > 128 || i.ExpiryTimeZone == "Local" {
		return false
	}
	_, err := time.LoadLocation(i.ExpiryTimeZone)
	return err == nil && i.ExpiryTimeZone != ""
}

// RegistrationPolicyParameters contains explicit tenant rules, not statutory defaults.
type RegistrationPolicyParameters struct {
	MaxSourceAgeSeconds int64     `json:"max_source_age_seconds"`
	DateRule            string    `json:"date_rule"`
	ValidUntil          time.Time `json:"valid_until"`
	ReviewReference     uuid.UUID `json:"review_reference"`
	AppointmentID       uuid.UUID `json:"appointment_id"`
	AppointmentVersion  int64     `json:"appointment_version"`
}
type RegistrationPolicyInput struct {
	MaxSourceAgeSeconds int64  `json:"max_source_age_seconds"`
	DateRule            string `json:"date_rule"`
	ValidUntil          string `json:"valid_until"`
	ReviewReference     string `json:"review_reference"`
	AppointmentID       string `json:"appointment_id"`
	AppointmentVersion  int64  `json:"appointment_version"`
}

func (in RegistrationPolicyInput) parse() (RegistrationPolicyParameters, bool) {
	until, tOK := stewardUntil(in.ValidUntil)
	reference, rOK := canonicalStewardReference(in.ReviewReference)
	appointment, aOK := canonicalStewardReference(in.AppointmentID)
	out := RegistrationPolicyParameters{in.MaxSourceAgeSeconds, in.DateRule, until, reference, appointment, in.AppointmentVersion}
	return out, tOK && rOK && aOK && in.AppointmentVersion == 2 && in.MaxSourceAgeSeconds >= 1 && in.MaxSourceAgeSeconds <= 31536000 && (in.DateRule == "date_required" || in.DateRule == "allow_undated_with_attestation")
}
func (p RegistrationPolicyParameters) equals(other RegistrationPolicyParameters) bool {
	return p.MaxSourceAgeSeconds == other.MaxSourceAgeSeconds && p.DateRule == other.DateRule && p.ValidUntil.Equal(other.ValidUntil) && p.ReviewReference == other.ReviewReference && p.AppointmentID == other.AppointmentID && p.AppointmentVersion == other.AppointmentVersion
}

type registrationPolicyRevision struct {
	ID                           uuid.UUID `json:"id"`
	PolicyID                     uuid.UUID `json:"policy_id"`
	Version                      int64     `json:"version"`
	ExpectedVersion              int64     `json:"expected_version"`
	RegistrationPolicyIdentity   `gorm:"embedded"`
	RegistrationPolicyParameters `gorm:"embedded"`
	AuthorizedBy                 string    `json:"authorized_by"`
	CreatedAt                    time.Time `json:"created_at"`
}

func (p *registrationPolicyRevision) AfterFind(*gorm.DB) error {
	p.CreatedAt = p.CreatedAt.UTC()
	p.ValidUntil = p.ValidUntil.UTC()
	return nil
}
func (p registrationPolicyRevision) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }
func policyRevisionQuery(tx *gorm.DB, p authn.Principal, id uuid.UUID) *gorm.DB {
	return tx.Table("(SELECT r.id,r.policy_id,r.version,r.expected_version,h.source_system,h.normalization,h.expiry_time_zone,r.max_source_age_seconds,r.date_rule,r.valid_until,r.review_reference,r.appointment_id,r.appointment_version,r.created_by AS authorized_by,r.created_at FROM practitioner_registration_policy_revisions r JOIN practitioner_registration_policies h ON h.tenant_id=r.tenant_id AND h.id=r.policy_id WHERE r.tenant_id=? AND r.policy_id=?) AS policy_editions", p.TenantID, id)
}

type registrationPolicyHead struct {
	ID                           uuid.UUID `json:"id"`
	RevisionID                   uuid.UUID `json:"revision_id"`
	Version                      int64     `json:"version"`
	RegistrationPolicyIdentity   `gorm:"embedded"`
	RegistrationPolicyParameters `gorm:"embedded"`
	AuthorizedBy                 string    `json:"authorized_by"`
	CreatedAt                    time.Time `json:"created_at"`
}

func (p *registrationPolicyHead) AfterFind(*gorm.DB) error {
	p.CreatedAt = p.CreatedAt.UTC()
	p.ValidUntil = p.ValidUntil.UTC()
	return nil
}
func (p registrationPolicyHead) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }
func policyHeadQuery(tx *gorm.DB, p authn.Principal) *gorm.DB {
	return tx.Table("(SELECT h.id,r.id AS revision_id,h.version,h.source_system,h.normalization,h.expiry_time_zone,r.max_source_age_seconds,r.date_rule,r.valid_until,r.review_reference,r.appointment_id,r.appointment_version,r.created_by AS authorized_by,h.created_at FROM practitioner_registration_policies h JOIN practitioner_registration_policy_revisions r ON r.tenant_id=h.tenant_id AND r.policy_id=h.id AND r.version=h.version WHERE h.tenant_id=?) AS policy_heads", p.TenantID)
}
func (h *handlers) policyTransaction(ctx context.Context, p authn.Principal, manage bool, fn func(*gorm.DB) error) error {
	return h.transaction(ctx, p, false, func(tx *gorm.DB) error {
		if manage {
			if err := tx.Exec("SELECT set_config('app.practitioner_policy_manage','true',true)").Error; err != nil {
				return err
			}
		}
		return fn(tx)
	})
}
func (h *handlers) putRegistrationPolicy(c *gin.Context)    { h.registrationPolicyCommand(c, false) }
func (h *handlers) reviseRegistrationPolicy(c *gin.Context) { h.registrationPolicyCommand(c, true) }
func (h *handlers) registrationPolicyCommand(c *gin.Context, revision bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "policy")
	if !ok {
		return
	}
	command := id
	if revision {
		command, ok = resourceID(c, "revision")
		if !ok {
			return
		}
	}
	var identity RegistrationPolicyIdentity
	var parameters RegistrationPolicyParameters
	var expected int64
	if revision {
		var in *struct {
			ExpectedVersion int64 `json:"expected_version"`
			RegistrationPolicyInput
		}
		if !decode(c, &in) {
			return
		}
		if in == nil {
			respond.Error(c, 400, "invalid_request", "Expected a complete issuer policy edition")
			return
		}
		parameters, ok = in.parse()
		expected = in.ExpectedVersion
		if !ok || expected < 1 || expected > 9007199254740990 {
			respond.Error(c, 400, "invalid_request", "Expected explicit bounded issuer rules and current version")
			return
		}
	} else {
		var in *struct {
			RegistrationPolicyIdentity
			RegistrationPolicyInput
		}
		if !decode(c, &in) {
			return
		}
		if in == nil {
			respond.Error(c, 400, "invalid_request", "Expected explicit issuer interpretation and rules")
			return
		}
		parameters, ok = in.parse()
		identity = in.RegistrationPolicyIdentity
		if !ok || !identity.valid() {
			respond.Error(c, 400, "invalid_request", "Expected explicit bounded issuer interpretation and rules")
			return
		}
	}
	if !h.reauthorize(c, PermRegistrationPolicyManage) {
		return
	}
	var row registrationPolicyRevision
	created := false
	err := h.policyTransaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		if revision {
			var head struct{ Version int64 }
			if err := tx.Raw("SELECT version FROM practitioner_registration_policies WHERE tenant_id=? AND id=? FOR UPDATE", p.TenantID, id).Scan(&head).Error; err != nil {
				return err
			}
			if head.Version == 0 {
				return gorm.ErrRecordNotFound
			}
			err := policyRevisionQuery(tx, p, id).Where("id=?", command).Take(&row).Error
			if err == nil {
				if row.ExpectedVersion != expected || !row.equals(parameters) {
					return errPolicyIntent
				}
				return auditIDs(tx, p, "practitioner_registration_policy_revisions", []uuid.UUID{command})
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if head.Version != expected {
				return errPolicyVersion
			}
		} else {
			result := tx.Exec("INSERT INTO practitioner_registration_policies(tenant_id,id,source_system,normalization,expiry_time_zone,created_by) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, id, identity.SourceSystem, identity.Normalization, identity.ExpiryTimeZone, p.Subject)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				if err := policyRevisionQuery(tx, p, id).Where("id=?", id).Take(&row).Error; err != nil {
					return err
				}
				if row.RegistrationPolicyIdentity != identity || !row.equals(parameters) {
					return errPolicyIntent
				}
				return auditIDs(tx, p, "practitioner_registration_policy_revisions", []uuid.UUID{id})
			}
		}
		if err := tx.Exec("INSERT INTO practitioner_registration_policy_revisions(tenant_id,policy_id,id,expected_version,version,max_source_age_seconds,date_rule,valid_until,review_reference,appointment_id,appointment_version,created_by) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", p.TenantID, id, command, expected, expected+1, parameters.MaxSourceAgeSeconds, parameters.DateRule, parameters.ValidUntil, parameters.ReviewReference, parameters.AppointmentID, parameters.AppointmentVersion, p.Subject).Error; err != nil {
			return err
		}
		if revision {
			updated := tx.Exec("UPDATE practitioner_registration_policies SET version=version+1 WHERE tenant_id=? AND id=? AND version=?", p.TenantID, id, expected)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return errPolicyVersion
			}
		}
		data, err := json.Marshal(practitionercontract.RegistrationPolicyChangedData{PolicyID: id.String(), Version: expected + 1})
		if err != nil {
			return err
		}
		if err := events.Enqueue(tx, practitionercontract.RegistrationPolicyChanged, events.Event{Type: practitionercontract.RegistrationPolicyChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		created = true
		return policyRevisionQuery(tx, p, id).Where("id=?", command).Take(&row).Error
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
func (h *handlers) getRegistrationPolicy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "policy")
	if !ok {
		return
	}
	var row registrationPolicyHead
	err := h.policyTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := policyHeadQuery(tx, p).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_registration_policy_revisions", []uuid.UUID{row.RevisionID})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) getRegistrationPolicyRevision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "policy")
	if !ok {
		return
	}
	command, ok := resourceID(c, "revision")
	if !ok {
		return
	}
	var row registrationPolicyRevision
	err := h.policyTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := policyRevisionQuery(tx, p, id).Where("id=?", command).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_registration_policy_revisions", []uuid.UUID{command})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listRegistrationPolicies(c *gin.Context, params pagination.Params) ([]registrationPolicyHead, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []registrationPolicyHead
	err := h.policyTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_registration_policies',?)", p.TenantID, p.Subject, p.TenantID).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(policyHeadQuery(tx, p), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.RevisionID
		}
		return auditIDs(tx, p, "practitioner_registration_policy_revisions", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) listRegistrationPolicyRevisions(c *gin.Context, params pagination.Params) ([]registrationPolicyRevision, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "policy")
	if !ok {
		return nil, nil
	}
	var rows []registrationPolicyRevision
	err := h.policyTransaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var head registrationPolicyHead
		if err := policyHeadQuery(tx, p).Where("id=?", id).Take(&head).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_registration_policies", []uuid.UUID{id}); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(policyRevisionQuery(tx, p, id), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registration_policy_revisions", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
