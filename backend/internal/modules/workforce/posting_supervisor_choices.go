package workforce

import (
	"context"
	"net/url"
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

func (r StaffPostingSupervisorChoice) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type postingSupervisorChoiceRecord struct {
	ID                   uuid.UUID
	RevisionID           uuid.UUID
	HospitalID           uuid.UUID
	StaffID              uuid.UUID
	EmployeeCode         string
	DisplayName          string
	Kind                 string
	DepartmentID         uuid.UUID
	DepartmentRevisionID uuid.UUID
	StartsAt             time.Time
	EndsAt               *time.Time
	EligibilityUntil     time.Time
	CreatedAt            time.Time
}

func postingSupervisorChoiceQuery(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, page pagination.Params) *gorm.DB {
	branches := make([]*gorm.DB, 0, 2)
	for _, kind := range []string{"hospital_management", "department_supervisor"} {
		q := tx.Table("workforce_posting_supervision_intents i").
			Joins("JOIN workforce_posting_proposals p ON (p.tenant_id,p.hospital_id,p.staff_id,p.id)=(i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id)").
			Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)").
			Joins("JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(p.tenant_id,p.hospital_id,p.staff_id)").
			Select("i.proposal_id AS id,p.revision_id,p.hospital_id,p.staff_id,i.kind,r.department_id,r.department_revision_id,r.starts_at,r.ends_at,r.eligibility_until,i.created_at,staff.employee_code,staff.display_name").
			Where("i.tenant_id=? AND i.hospital_id=? AND i.kind=? AND p.status='approved'", p.TenantID, hospital, kind)
		branches = append(branches, tenantdb.ApplyKeyset(tx.Table("(?) AS candidate", q), page))
	}
	return tx.Table("(?) AS choices", tx.Raw("(?) UNION ALL (?)", branches[0], branches[1])).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
}

func (h *handlers) listPostingSupervisorChoices(c *gin.Context, page pagination.Params) ([]StaffPostingSupervisorChoice, error) {
	c.Header("Cache-Control", "no-store")
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	if _, available := h.scopeAuthority.(authz.CompoundScopedCommandGuard); !available || h.staffActors == nil || h.db == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "Staff posting hospital")
		return nil, nil
	}
	var query url.Values
	if len(c.Request.URL.RawQuery) <= 4096 {
		query, err = url.ParseQuery(c.Request.URL.RawQuery)
	}
	valid := len(c.Request.URL.RawQuery) <= 4096 && err == nil && page.Limit <= postingInboxLimit
	for key, values := range query {
		valid = valid && len(values) == 1 && contains(key, "purpose", "limit", "cursor")
	}
	purpose := query.Get("purpose")
	valid = valid && contains(purpose, "submission", "review") && len(query.Get("cursor")) <= 2048
	if !valid {
		respond.Error(c, 400, "invalid_request", "Choose one submission or review purpose and a page of at most eight records.")
		return nil, nil
	}
	permission := PermPostingSubmit
	if purpose == "review" {
		permission = PermPostingReview
	}
	actor, err := h.postingActorForHospital(c, p, hospital)
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	markScopedPosting(c)
	rows := []StaffPostingSupervisorChoice{}
	err = h.scopedPostingTransaction(c, p, postingDraftScope{hospital: hospital, staff: actor}, false, permission, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_posting_supervisor_choices','read',true),set_config('app.workforce_posting_directory','read',true),set_config('app.workforce_posting_review_mode','read',true)`).Error; err != nil {
			return err
		}
		var allowed bool
		if err := tx.Raw(`SELECT workforce_posting_supervisor_choices_context(?::uuid,?::uuid)`, p.TenantID, hospital).Scan(&allowed).Error; err != nil {
			return err
		}
		if !allowed {
			return authz.ErrScopedCommandDenied
		}
		var records []postingSupervisorChoiceRecord
		if err := postingSupervisorChoiceQuery(tx, p, hospital, page).Find(&records).Error; err != nil {
			return err
		}
		for _, r := range records {
			rows = append(rows, StaffPostingSupervisorChoice{ID: r.ID, RevisionID: r.RevisionID, HospitalID: r.HospitalID, Staff: StaffPostingTarget{ID: r.StaffID, EmployeeCode: r.EmployeeCode, DisplayName: r.DisplayName}, Kind: r.Kind, DepartmentID: r.DepartmentID, DepartmentRevisionID: r.DepartmentRevisionID, StartsAt: r.StartsAt, EndsAt: r.EndsAt, EligibilityUntil: r.EligibilityUntil, CreatedAt: r.CreatedAt})
		}
		return postingProposalAuditRecord(tx, p, hospital, hospital, "supervisor_choices_read", 0, map[string]any{"purpose": purpose, "rows": min(len(rows), page.Limit)})
	}, PermPostingRead)
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	return rows, nil
}
