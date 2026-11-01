package workforce

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
	"time"
)

func (r EmploymentReviewSummary) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *handlers) listEmploymentReviewInbox(c *gin.Context, page pagination.Params) ([]EmploymentReviewSummary, error) {
	p, hospital, ok := personBrowseParams(c)
	if !ok {
		return nil, nil
	}
	mode := "hr_intake"
	if _, present := c.Request.URL.Query()["employment_review"]; present {
		mode = "employment_review"
	}
	query := c.Request.URL.Query()
	for key, values := range query {
		if len(values) != 1 || !contains(key, mode, "status", "limit", "cursor") {
			employmentReviewError(c, errIntakeProjection)
			return nil, nil
		}
	}
	if query.Get(mode) != "v1" {
		employmentReviewError(c, errIntakeProjection)
		return nil, nil
	}
	status := "pending"
	if values, present := query["status"]; present {
		status = values[0]
	}
	if !contains(status, "pending", "approved", "rejected", "withdrawn", "revoked") {
		employmentReviewError(c, errIntakeProjection)
		return nil, nil
	}
	required := []authz.Permission{PermEnrollmentManage, PermDocumentManage}
	if mode == "employment_review" {
		required = []authz.Permission{PermEmploymentReview}
	}
	rows := []EmploymentReviewSummary{}
	err := h.intakeAuthorityTransaction(c, p, hospital, required, true, func(tx *gorm.DB, scoped bool) error {
		value, present := c.Get(intakeAuthorityKey)
		authority, valid := value.(*intakeAuthority)
		if !scoped || !present || !valid || authority == nil || !authority.Scoped {
			return authz.ErrScopedCommandDenied
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_browse','inbox',true),set_config('app.workforce_employment_browse_actor',?,true),set_config('app.workforce_employment_browse_link',?,true)`, authority.Actor.Staff.String(), authority.Actor.Link.String()).Error; err != nil {
			return err
		}
		records := tx.Table("workforce_employment_reviews r").Select("r.id,r.hospital_id,r.staff_id,s.employee_code,s.display_name,r.status,r.version,r.created_at").Joins("JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(r.tenant_id,r.hospital_id,r.staff_id)").Where("r.tenant_id=? AND r.hospital_id=? AND r.status=?", p.TenantID, hospital, status)
		if page.After != nil {
			records = records.Where("(r.created_at,r.id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := records.Order("r.created_at DESC,r.id DESC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		proof, err := json.Marshal(authority)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_review.inbox.read',?,jsonb_build_object('authority',?::jsonb,'rows',?::int,'status',?::text))`, p.TenantID, hospital, p.Subject, hospital, string(proof), min(len(rows), page.Limit), status).Error
	})
	if err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
