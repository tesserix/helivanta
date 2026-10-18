package workforce

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func (h *handlers) insights(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		respond.Error(c, 400, "invalid_request", "staff insights are a current hospital snapshot and accept no query parameters")
		return
	}
	var rows = make([]struct {
		Category string `json:"category"`
		Status   string `json:"status"`
		Count    int64  `json:"count"`
	}, 0)
	asAt := time.Now().UTC()
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		// Hospital grants are mandatory even for tenant administrators; deny on lookup failure.
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL statement_timeout = '2s'").Error; err != nil {
			return err
		}
		return tx.Table("workforce_staff").
			Select("job_category AS category, employment_status AS status, count(*) AS count").
			Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital).
			Group("job_category, employment_status").Order("job_category, employment_status").Scan(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "as_at": asAt, "counts": rows})
}
