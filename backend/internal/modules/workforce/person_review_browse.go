package workforce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r PersonReviewTarget) PageKey() (time.Time, uuid.UUID)  { return r.CreatedAt, r.ID }
func (r PersonReviewSummary) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func personBrowseParams(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return p, hospital, false
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening person reviews.")
		return p, hospital, false
	}
	return p, hospital, authn.Refresh(c)
}

func (h *handlers) withPersonBrowse(c *gin.Context, p authn.Principal, hospital uuid.UUID, mode string, permission authz.Permission, read func(*gorm.DB, reviewedLeaveActor, authz.ScopeGrant) error) error {
	return h.withIdentityScopeGrant(c, p, hospital, permission, func(ctx context.Context, grant authz.ScopeGrant) error {
		return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
				return err
			}
			actor, err := lockReviewedLeaveActor(tx, p, hospital)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrScopedCommandDenied
			}
			if err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_person_browse',?,true),set_config('app.workforce_person_browse_actor',?,true),set_config('app.workforce_person_browse_link',?,true)`, mode, actor.StaffID.String(), actor.ID.String()).Error; err != nil {
				return err
			}
			return read(tx, actor, grant)
		})
	})
}

func (h *handlers) searchPersonReviewTargets(c *gin.Context, page pagination.Params) ([]PersonReviewTarget, error) {
	p, hospital, ok := personBrowseParams(c)
	if !ok {
		return nil, nil
	}
	var input PersonReviewTargetSearchInput
	if !decodeSized(c, &input, 1024) {
		return nil, nil
	}
	if input.EmployeeCode == nil || (*input.EmployeeCode != "" && !employeeCode.MatchString(*input.EmployeeCode)) {
		respond.Error(c, 400, "invalid_request", "Enter a Staff ID, or an empty value to browse staff.")
		return nil, nil
	}
	rows := []PersonReviewTarget{}
	err := h.withPersonBrowse(c, p, hospital, "targets", PermIdentityChecksManage, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		query := tenantdb.ApplyKeyset(tx, page).Table("workforce_staff").Select("id,hospital_id,employee_code,display_name,employment_status,version AS staff_version,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if *input.EmployeeCode != "" {
			query = query.Where("lower(employee_code)=?", strings.ToLower(*input.EmployeeCode))
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if err := personReviewAudit(tx, p, hospital, row.ID, "workforce.person_review.target.read", row.StaffVersion, actor, grant); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		personReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) listPersonReviewInbox(c *gin.Context, page pagination.Params) ([]PersonReviewSummary, error) {
	p, hospital, ok := personBrowseParams(c)
	if !ok {
		return nil, nil
	}
	status := "pending"
	if values, supplied := c.Request.URL.Query()["status"]; supplied {
		if len(values) != 1 {
			respond.Error(c, 400, "invalid_request", "Choose one review status.")
			return nil, nil
		}
		status = values[0]
	}
	switch status {
	case "pending", "approved", "rejected", "withdrawn", "revoked":
	default:
		respond.Error(c, 400, "invalid_request", "Choose a recorded review status.")
		return nil, nil
	}
	rows := []PersonReviewSummary{}
	read := func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		query := tx.Table("workforce_person_reviews r").Select("r.id,r.hospital_id,r.staff_id,s.employee_code,s.display_name,r.status,r.version,r.valid_until,r.created_at").Joins("JOIN workforce_staff s ON s.tenant_id=r.tenant_id AND s.hospital_id=r.hospital_id AND s.id=r.staff_id").Where("r.tenant_id=? AND r.hospital_id=? AND r.status=?", p.TenantID, hospital, status)
		if page.After != nil {
			query = query.Where("(r.created_at,r.id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Order("r.created_at DESC,r.id DESC").Limit(page.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if err := personReviewAudit(tx, p, hospital, row.ID, "workforce.person_review.inbox.read", row.Version, actor, grant); err != nil {
				return err
			}
		}
		return nil
	}
	err := h.withPersonBrowse(c, p, hospital, "inbox", PermIdentityChecksManage, read)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		err = h.withPersonBrowse(c, p, hospital, "inbox", PermIdentityChecksReview, read)
	}
	if err != nil {
		personReviewError(c, err)
		return nil, nil
	}
	return rows, nil
}
