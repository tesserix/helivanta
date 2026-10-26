package practitioner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type handlers struct {
	db          *tenantdb.DB
	files       *attachments.GCS
	reauthorize func(*gin.Context, authz.Permission) bool
}
type record struct {
	ID                uuid.UUID `json:"id"`
	GivenName         string    `json:"given_name"`
	FamilyName        *string   `json:"family_name"`
	ProfessionalType  string    `json:"professional_type"`
	VerificationState string    `json:"verification_state"`
	CreatedAt         time.Time `json:"created_at"`
}

func (record) TableName() string           { return "practitioner_records" }
func (r *record) AfterFind(*gorm.DB) error { r.CreatedAt = r.CreatedAt.UTC(); return nil }

const recordColumns = "id,given_name,family_name,professional_type,verification_state,created_at"

var errIntentConflict = errors.New("practitioner intent conflicts")

type recordInput struct {
	GivenName        string  `json:"given_name"`
	FamilyName       *string `json:"family_name"`
	ProfessionalType string  `json:"professional_type"`
}

func validName(s string) bool {
	if !utf8.ValidString(s) || len(s) == 0 || len(s) > 150 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (in recordInput) valid() bool {
	if !validName(in.GivenName) || (in.FamilyName != nil && !validName(*in.FamilyName)) {
		return false
	}
	switch in.ProfessionalType {
	case "physician", "surgeon", "dentist", "nurse", "pharmacist", "pathologist", "radiologist", "lab_technician", "physiotherapist", "paramedic", "biomedical_technician":
		return true
	default:
		return false
	}
}
func decode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		respond.Error(c, 400, "invalid_request", "Expected a bounded practitioner request")
		return false
	}
	return true
}
func input(c *gin.Context) (*recordInput, bool) {
	var in *recordInput
	if !decode(c, &in) {
		return nil, false
	}
	if in == nil || !in.valid() {
		respond.Error(c, 400, "invalid_request", "Expected an unverified practitioner identity")
		return nil, false
	}
	return in, true
}
func resourceID(c *gin.Context, key string) (uuid.UUID, bool) {
	raw := c.Param(key)
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != raw {
		respond.NotFound(c, "practitioner")
		return uuid.Nil, false
	}
	return id, true
}
func failure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, ErrEvidenceNotReady):
		respond.Error(c, 409, "evidence_not_available", "Evidence content has not been released or is revoked")
	case errors.Is(err, errEvidenceUnavailable):
		respond.Error(c, 503, "evidence_unavailable", "Private evidence storage is unavailable; retry the same operation")
	case errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "practitioner_qualification_year":
		respond.Error(c, 400, "invalid_request", "Completion year cannot be in the future")
	case errors.Is(err, errStewardInvalid) || errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "practitioner_steward_nomination":
		respond.Error(c, 400, "invalid_request", "Invalid steward appointment")
	case errors.Is(err, errStewardIndependence):
		respond.Error(c, 403, "independent_nomination_required", "A different tenant administrator must nominate the reviewer")
	case errors.Is(err, gorm.ErrRecordNotFound) || errors.As(err, &pg) && pg.Code == "P0002":
		respond.NotFound(c, "practitioner")
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "57014"):
		respond.Error(c, 503, "practitioner_unavailable", "Practitioner operation timed out; reconcile the retained command before retrying")
	case errors.As(err, &pg) && pg.Code == "23514" && verificationConstraintCode(pg.ConstraintName) != "":
		code := verificationConstraintCode(pg.ConstraintName)
		respond.Error(c, 409, code, "Registration attestation requirements are not satisfied")
	case errors.Is(err, errPolicyIntent):
		respond.Error(c, 409, "policy_intent_conflict", "Retained policy intent differs")
	case errors.Is(err, errPolicyVersion) || errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "registration_policy_version":
		respond.Error(c, 409, "policy_version_conflict", "Policy version changed")
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "registration_policy_source_unique":
		respond.Error(c, 409, "policy_source_conflict", "Issuer namespace is already configured")
	case errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "registration_policy_authority":
		respond.Error(c, 403, "policy_authority_required", "A current accepted policy appointment is required")
	case errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "registration_policy_input":
		respond.Error(c, 400, "invalid_verification_policy", "Issuer calendar or validity is invalid")
	case errors.Is(err, errIntentConflict) || errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514" && (pg.ConstraintName == "practitioner_steward_acceptance" || pg.ConstraintName == "practitioner_evidence_limit" || pg.ConstraintName == "practitioner_evidence_upload" || pg.ConstraintName == "practitioner_evidence_revocation" || pg.ConstraintName == "practitioner_evidence_retry" || pg.ConstraintName == "practitioner_review_state" || pg.ConstraintName == "practitioner_specialty_retirement")):
		respond.Error(c, 409, "practitioner_conflict", "This request conflicts with the retained practitioner state")
	default:
		respond.InternalErr(c, errors.New("practitioner storage operation unavailable"), "Practitioner operation unavailable")
	}
}
func setScope(tx *gorm.DB, p authn.Principal, manage bool) error {
	manageValue := "false"
	if manage {
		manageValue = "true"
	}
	return tx.Exec("SELECT set_config('app.actor_subject',?,true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_manage',?,true),set_config('lock_timeout','2s',true),set_config('statement_timeout','5s',true)", p.Subject, manageValue).Error
}
func read(tx *gorm.DB, p authn.Principal, id uuid.UUID, row *record) error {
	return tx.Select(recordColumns).Where("tenant_id=? AND id=?", p.TenantID, id).Take(row).Error
}
func auditRead(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_records',?)", p.TenantID, p.Subject, id).Error
}
func sameOptionalString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func (h *handlers) put(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	in, ok := input(c)
	if !ok {
		return
	}
	created := false
	var row record
	err := h.transaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		result := tx.Exec("INSERT INTO practitioner_records(tenant_id,id,given_name,family_name,professional_type,created_by) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, id, in.GivenName, in.FamilyName, in.ProfessionalType, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if e := read(tx, p, id, &row); e != nil {
			return e
		}
		if row.GivenName != in.GivenName || !sameOptionalString(row.FamilyName, in.FamilyName) || row.ProfessionalType != in.ProfessionalType {
			return errIntentConflict
		}
		if !created {
			return auditRead(tx, p, id)
		}
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
func (h *handlers) get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	var row record
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := read(tx, p, id, &row); e != nil {
			return e
		}
		return auditRead(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) transaction(ctx context.Context, p authn.Principal, manage bool, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := setScope(tx, p, manage); err != nil {
			return err
		}
		return fn(tx)
	})
}

func verificationConstraintCode(name string) string {
	switch name {
	case "registration_reconciliation_source":
		return "replacement_source_not_later"
	case "registration_verification_version":
		return "verification_version_conflict"
	case "registration_verification_target":
		return "verification_target_changed"
	case "registration_verification_revision":
		return "registration_revision_changed"
	case "registration_verification_independence":
		return "independent_verifier_required"
	case "registration_verification_policy":
		return "verification_policy_not_current"
	case "registration_verification_review":
		return "matching_active_review_required"
	case "registration_verification_conflict":
		return "source_reviews_require_reconciliation"
	case "registration_verification_evidence":
		return "reviewed_evidence_not_current"
	case "registration_verification_freshness":
		return "source_verification_expired"
	case "registration_verification_dates":
		return "registration_issue_date_in_future"
	case "registration_verification_expiry":
		return "registration_expiry_not_supported"
	case "registration_verification_normalization":
		return "identifier_normalization_not_supported"
	case "registration_identifier_conflict":
		return "registration_identifier_conflict"
	}
	return ""
}
