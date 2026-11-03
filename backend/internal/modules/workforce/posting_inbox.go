package workforce

import (
	"context"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const postingInboxLimit = 8

func (r StaffPostingInboxItem) PageKey() (time.Time, uuid.UUID) { return r.Proposal.PageKey() }

type postingInboxRecord struct {
	StaffPostingProposal `gorm:"embedded"`
	EmployeeCode         string
	DisplayName          string
	IsSubmitter          bool
}

func (h *handlers) listPostingProposalInbox(c *gin.Context, page pagination.Params) ([]StaffPostingInboxItem, error) {
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
		valid = valid && len(values) == 1 && contains(key, "status", "limit", "cursor")
	}
	status := "pending"
	if values, present := query["status"]; present {
		status = values[0]
	}
	valid = valid && contains(status, "pending", "approved", "rejected", "withdrawn") && len(query.Get("cursor")) <= 2048
	if !valid {
		respond.Error(c, 400, "invalid_request", "Choose one proposal status and a page of at most eight records.")
		return nil, nil
	}
	actor, err := h.postingActorForHospital(c, p, hospital)
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	markScopedPosting(c)
	scope := postingDraftScope{hospital: hospital, staff: actor}
	rows := []StaffPostingInboxItem{}
	err = h.scopedPostingTransaction(c, p, scope, false, PermPostingProposalRead, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_posting_inbox','read',true),set_config('app.workforce_posting_directory','read',true),set_config('app.workforce_posting_review_mode','read',true)`).Error; err != nil {
			return err
		}
		var allowed bool
		if err := tx.Raw(`SELECT workforce_posting_inbox_context(?::uuid,?::uuid)`, p.TenantID, hospital).Scan(&allowed).Error; err != nil {
			return err
		}
		if !allowed {
			return authz.ErrScopedCommandDenied
		}
		q := tx.Table("workforce_posting_proposals p").
			Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)").
			Joins("JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(p.tenant_id,p.hospital_id,p.staff_id)").
			Joins("JOIN workforce_posting_proposal_revisions submitted ON (submitted.tenant_id,submitted.hospital_id,submitted.staff_id,submitted.proposal_id)=(p.tenant_id,p.hospital_id,p.staff_id,p.id) AND submitted.version=1").
			Select(postingProposalProjection+",staff.employee_code,staff.display_name,(submitted.actor_subject=?) AS is_submitter", p.Subject).
			Where("p.tenant_id=? AND p.hospital_id=? AND p.status=?", p.TenantID, hospital, status)
		var records []postingInboxRecord
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS inbox_rows", q), page).Find(&records).Error; err != nil {
			return err
		}
		for _, record := range records {
			rows = append(rows, StaffPostingInboxItem{Proposal: publicPostingProposal(record.StaffPostingProposal), Staff: StaffPostingTarget{ID: record.StaffID, EmployeeCode: record.EmployeeCode, DisplayName: record.DisplayName}, IsSubmitter: record.IsSubmitter})
		}
		return postingProposalAuditRecord(tx, p, hospital, hospital, "inbox_read", 0, map[string]any{"status": status, "rows": min(len(rows), page.Limit)})
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	return rows, nil
}
