package emergency

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/transit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type closureInput struct {
	Version     int64  `json:"version" binding:"required,min=1"`
	Reason      string `json:"reason" binding:"required,max=50"`
	Explanation string `json:"explanation" binding:"required,max=1000"`
}

func (h *handlers) closeCase(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := caseID(c)
	if !ok {
		return
	}
	var in closureInput
	if !decode(c, &in) {
		return
	}
	in.Explanation = strings.TrimSpace(in.Explanation)
	if !validChoice("closure_reasons", in.Reason) || in.Explanation == "" || !validNarrative(in.Explanation) {
		respond.Error(c, 400, "invalid_request", "a coded reason and explanation are required")
		return
	}
	if err := h.checkScope(c.Request.Context(), p, b, "close"); err != nil {
		respondError(c, err)
		return
	}
	if h.transit == nil {
		respondError(c, transit.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	ciphertext, err := h.transit.Encrypt(ctx, caseKey(p), caseContext(p, b, id, "closure"), []byte(in.Explanation))
	if err != nil {
		respondError(c, err)
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		respondError(c, err)
		return
	}
	fingerprint, err := h.transit.HMAC(ctx, caseKey(p), append(caseContext(p, b, id, "closure-request"), encoded...))
	if err != nil {
		respondError(c, err)
		return
	}
	var out caseRow
	err = h.transaction(ctx, p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(caseProtectedColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if out.Status == "closed" {
			if out.ClosureFingerprint != nil && subtle.ConstantTimeCompare([]byte(*out.ClosureFingerprint), []byte(fingerprint)) == 1 {
				return nil
			}
			return errCaseConflict
		}
		if out.Version != in.Version {
			return errCaseConflict
		}
		result := tx.Exec(`UPDATE emergency_cases SET status='closed',version=version+1,closure_reason=?,closure_ciphertext=?,closure_fingerprint=?,closed_at=now(),updated_at=now() WHERE tenant_id=? AND base_id=? AND id=? AND status='received' AND version=?`, in.Reason, ciphertext, fingerprint, p.TenantID, b, id, in.Version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errCaseConflict
		}
		if err := tx.Select(caseProtectedColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		return changed(tx, p, b, out, emergencycontract.SubjectCaseClosed)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
