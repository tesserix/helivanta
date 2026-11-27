package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const verificationPolicyDraftColumns = "id,policy_id,outlet_id,expected_version,version,label,second_check_required,review_valid_for_seconds,query_response_target_seconds,reason,prepared_by,created_at,'draft' AS state,'not_evaluated' AS clinical_authority"

var errVerificationPolicyDraftConflict = errors.New("verification policy draft conflict")

func (r *VerificationPolicyDraftRevision) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	return nil
}

func (VerificationPolicyDraftRevision) TableName() string {
	return "pharmacy_verification_policy_drafts"
}
func (r VerificationPolicyDraftRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func verificationPolicyDraftRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/verification-policy-drafts/:policy/revisions/:revision", PermAccessManage, h.putVerificationPolicyDraft)
	s.GET("/verification-policy-drafts/:policy/revisions/:revision", PermAccessManage, h.getVerificationPolicyDraft)
	platform.ListRoute(s, "/verification-policy-drafts/:policy/revisions", PermAccessManage, h.listVerificationPolicyDraftHistory)
	platform.ListRoute(s, "/verification-policy-drafts", PermAccessManage, h.listVerificationPolicyDrafts)
}

func verificationPolicyDraftID(c *gin.Context, key string) (uuid.UUID, bool) {
	raw := c.Param(key)
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != raw {
		respond.NotFound(c, "verification policy draft")
		return uuid.Nil, false
	}
	return id, true
}

func verificationPolicyDraftError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "verification policy draft")
		return
	}
	var pg *pgconn.PgError
	if errors.Is(err, errVerificationPolicyDraftConflict) || errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "pharmacy_verification_policy_draft_version") {
		respond.Error(c, http.StatusConflict, "verification_policy_draft_conflict", "Refresh the draft or recover the original revision command")
		return
	}
	respond.Error(c, http.StatusServiceUnavailable, "verification_policy_draft_unavailable", "Policy draft storage is unavailable")
}

func policyDraftRead(tx *gorm.DB, tenant string, outlet, revision uuid.UUID, row *VerificationPolicyDraftRevision) error {
	return tx.Select(verificationPolicyDraftColumns).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, revision).Take(row).Error
}

func (h *catalogueHandlers) putVerificationPolicyDraft(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	policy, ok := verificationPolicyDraftID(c, "policy")
	if !ok {
		return
	}
	revision, ok := verificationPolicyDraftID(c, "revision")
	if !ok {
		return
	}
	var in VerificationPolicyDraftInput
	if !decodeCatalogueWithin(c, &in, 8192) {
		return
	}
	in.Label = strings.Trim(in.Label, verificationBlankNoteCharacters)
	in.Reason = strings.Trim(in.Reason, verificationBlankNoteCharacters)
	if !validVerificationPolicyDraftInput(in) {
		respond.Error(c, 400, "invalid_request", "Enter the policy draft settings, reason and expected revision")
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		verificationPolicyDraftError(c, err)
		return
	}
	digest := sha256.Sum256(append([]byte("helivanta.verification-policy-draft.v1\x00"), encoded...))
	fingerprint := hex.EncodeToString(digest[:])
	var result VerificationPolicyDraftRevision
	created := false
	err = h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('lock_timeout','2s',true),set_config('statement_timeout','5s',true)").Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pharmacy_verification_policy_draft_lock(?,?,?)", p.TenantID, outlet, policy).Error; err != nil {
			return err
		}
		var retained struct {
			PolicyID                 uuid.UUID
			PreparedBy, IntentSHA256 string
		}
		err := tx.Table("pharmacy_verification_policy_drafts").Select("policy_id,prepared_by,intent_sha256").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, revision).Take(&retained).Error
		if err == nil {
			if retained.PolicyID != policy || retained.PreparedBy != p.Subject || retained.IntentSHA256 != fingerprint {
				return errVerificationPolicyDraftConflict
			}
			if err := policyDraftRead(tx, p.TenantID, outlet, revision, &result); err != nil {
				return err
			}
			return auditRegisterRead(c, tx, "verification_policy_draft", p.TenantID, p.Subject, outlet, "command_recovered", "policy_id", policy.String(), "revision_id", revision.String())
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_verification_policy_drafts(tenant_id,outlet_id,policy_id,id,expected_version,version,label,second_check_required,review_valid_for_seconds,query_response_target_seconds,reason,intent_sha256,prepared_by) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, outlet, policy, revision, *in.ExpectedVersion, *in.ExpectedVersion+1, in.Label, *in.SecondCheckRequired, in.ReviewValidForSeconds, in.QueryResponseTargetSeconds, in.Reason, fingerprint, p.Subject).Error; err != nil {
			return err
		}
		created = true
		return policyDraftRead(tx, p.TenantID, outlet, revision, &result)
	})
	if err != nil {
		verificationPolicyDraftError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) getVerificationPolicyDraft(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	policy, ok := verificationPolicyDraftID(c, "policy")
	if !ok {
		return
	}
	revision, ok := verificationPolicyDraftID(c, "revision")
	if !ok {
		return
	}
	var row VerificationPolicyDraftRevision
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := policyDraftRead(tx, p.TenantID, outlet, revision, &row); err != nil {
			return err
		}
		if row.PolicyID != policy {
			return gorm.ErrRecordNotFound
		}
		return auditRegisterRead(c, tx, "verification_policy_draft", p.TenantID, p.Subject, outlet, "revision_read", "policy_id", policy.String(), "revision_id", revision.String())
	})
	if err != nil {
		verificationPolicyDraftError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *catalogueHandlers) listVerificationPolicyDraftHistory(c *gin.Context, params pagination.Params) ([]VerificationPolicyDraftRevision, error) {
	return h.listVerificationPolicyDraftRows(c, params, true)
}
func (h *catalogueHandlers) listVerificationPolicyDrafts(c *gin.Context, params pagination.Params) ([]VerificationPolicyDraftRevision, error) {
	return h.listVerificationPolicyDraftRows(c, params, false)
}
func (h *catalogueHandlers) listVerificationPolicyDraftRows(c *gin.Context, params pagination.Params, history bool) ([]VerificationPolicyDraftRevision, error) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return nil, nil
	}
	var policy uuid.UUID
	if history {
		policy, ok = verificationPolicyDraftID(c, "policy")
		if !ok {
			return nil, nil
		}
	}
	var rows []VerificationPolicyDraftRevision
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		query := tx.Model(&VerificationPolicyDraftRevision{}).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet)
		if history {
			query = query.Where("policy_id=?", policy)
			var exists VerificationPolicyDraftRevision
			if err := query.Session(&gorm.Session{}).Select("id").Take(&exists).Error; err != nil {
				return err
			}
		}
		if err := tenantdb.ApplyKeyset(query, params).Select(verificationPolicyDraftColumns).Find(&rows).Error; err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "verification_policy_draft", p.TenantID, p.Subject, outlet, "history_read", "policy_id", policy.String())
	})
	if err != nil {
		verificationPolicyDraftError(c, err)
		return nil, nil
	}
	return rows, nil
}
