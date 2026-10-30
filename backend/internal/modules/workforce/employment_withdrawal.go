package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func employmentLifecycleProjection(c *gin.Context) (bool, bool) {
	values, present := c.Request.URL.Query()["lifecycle"]
	if !present {
		return false, true
	}
	if len(values) != 1 || values[0] != "withdrawal-v1" {
		respond.Error(c, 400, "invalid_request", "This employment lifecycle view is not supported. Refresh and retry.")
		return false, false
	}
	return true, true
}

func (h *handlers) withdrawEmploymentPeriod(c *gin.Context) {
	p, hospital, staff, id, ok := employmentPeriodScope(c)
	if !ok {
		return
	}
	var in EmploymentPeriodWithdrawInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion >= 9007199254740990 || !evidenceReason(&in.Reason) {
		respond.Error(c, 400, "invalid_request", "Enter a withdrawal reason and the current draft version.")
		return
	}
	raw, err := json.Marshal(in)
	if err != nil {
		respondError(c, err)
		return
	}
	digest := sha256.Sum256(append([]byte("withdraw-employment-period:"), raw...))
	intent := hex.EncodeToString(digest[:])
	var result EmploymentPeriodDraft
	err = h.employmentPeriodTransaction(c, p, hospital, staff, func(tx *gorm.DB, _ EmploymentPeriodSource) error {
		var receipt employmentPeriodRecord
		err := tx.Table("workforce_employment_period_history").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND revision_id=?", p.TenantID, hospital, in.CommandID).Take(&receipt).Error
		if err == nil {
			if receipt.ID != id || receipt.StaffID != staff || receipt.ActorSubject != p.Subject || receipt.Status != "withdrawn" || receipt.InputHash != intent {
				return errEmploymentPeriodConflict
			}
			result = receipt.EmploymentPeriodDraft
			return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.receipt.read", result.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var current employmentPeriodRecord
		if err := tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&current).Error; err != nil {
			return err
		}
		if current.Status != "draft" || current.Version != *in.ExpectedVersion {
			return errEmploymentPeriodConflict
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_withdrawal','authorized',true)`).Error; err != nil {
			return err
		}
		changed := tx.Table("workforce_employment_period_drafts").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND status='draft'", p.TenantID, hospital, staff, id, *in.ExpectedVersion).Updates(map[string]any{"status": "withdrawn", "reason": in.Reason, "revision_id": in.CommandID, "version": *in.ExpectedVersion + 1, "actor_subject": p.Subject, "input_hash": intent})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return errEmploymentPeriodConflict
		}
		if err := tx.Table("workforce_employment_period_drafts").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&result).Error; err != nil {
			return err
		}
		return employmentPeriodAudit(tx, p, hospital, id, "workforce.employment_period.withdrawn", result.Version)
	})
	if err != nil {
		respondEmploymentPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}
