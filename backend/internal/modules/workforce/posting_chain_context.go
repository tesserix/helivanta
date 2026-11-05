package workforce

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

type postingChainLabel struct {
	ID, StaffID               uuid.UUID
	EmployeeCode, DisplayName string
	CreatedAt                 time.Time
}

func postingChainLabelQuery(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, keys [][]any) *gorm.DB {
	return tx.Table("workforce_posting_supervision_intents i").Joins("JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(i.tenant_id,i.hospital_id,i.staff_id)").Select("i.proposal_id AS id,i.staff_id,i.created_at,staff.employee_code,staff.display_name").Where("i.tenant_id=? AND i.hospital_id=? AND (i.staff_id,i.proposal_id) IN ?", p.TenantID, hospital, keys)
}

func postingChainReviewView(tx *gorm.DB, p authn.Principal, s postingDraftScope, selected StaffPostingProposalContext, intent StaffPostingSupervisionIntent, sources []postingChainSource) (StaffPostingChainReviewContext, error) {
	view := StaffPostingChainReviewContext{Context: selected, Supervision: intent, CheckedAt: time.Now().UTC(), EligibilityUntil: postingApprovalDeadline(selected.Proposal), Chain: make([]StaffPostingChainNode, 0, len(sources))}
	keys := make([][]any, 0, len(sources))
	for _, source := range sources {
		keys = append(keys, []any{source.Posting.StaffID, source.Posting.ID})
	}
	var previous string
	if err := tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_posting_directory','app.workforce_posting_supervisor_choices','app.workforce_posting','app.workforce_posting_scoped','app.workforce_posting_review_mode','app.workforce_posting_staff']) AS key`).Scan(&previous).Error; err != nil {
		return view, err
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_posting_directory','read',true),set_config('app.workforce_posting_supervisor_choices','read',true),set_config('app.workforce_posting','read',true),set_config('app.workforce_posting_scoped','read',true),set_config('app.workforce_posting_review_mode','read',true),set_config('app.workforce_posting_staff',current_setting('app.workforce_posting_actor'),true)`).Error; err != nil {
		return view, err
	}
	var labels []postingChainLabel
	err := postingChainLabelQuery(tx, p, s.hospital, keys).Find(&labels).Error
	err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, previous).Error)
	if err != nil {
		return view, err
	}
	for _, source := range sources {
		row := source.Posting
		var posting StaffPostingSupervisorChoice
		for _, label := range labels {
			if label.ID == row.ID && label.StaffID == row.StaffID {
				posting = StaffPostingSupervisorChoice{ID: row.ID, RevisionID: row.RevisionID, HospitalID: row.HospitalID, Staff: StaffPostingTarget{ID: row.StaffID, EmployeeCode: label.EmployeeCode, DisplayName: label.DisplayName}, Kind: source.Intent.Kind, DepartmentID: row.DepartmentID, DepartmentRevisionID: row.DepartmentRevisionID, StartsAt: row.StartsAt, EndsAt: row.EndsAt, EligibilityUntil: row.EligibilityUntil, CreatedAt: label.CreatedAt}
				break
			}
		}
		if posting.ID == uuid.Nil {
			return view, authz.ErrScopedAuthorizationUnavailable
		}
		view.Chain = append(view.Chain, StaffPostingChainNode{Posting: posting, Supervision: source.Intent})
		if deadline := postingApprovalDeadline(row); deadline.Before(view.EligibilityUntil) {
			view.EligibilityUntil = deadline
		}
	}
	return view, nil
}
