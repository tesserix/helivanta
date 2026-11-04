package workforce

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const postingChainLimit = 32

type postingChainSource struct {
	Posting StaffPostingProposal
	Intent  StaffPostingSupervisionIntent
}

func validatePostingApprovalSource(tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, source EmploymentPeriodSource) error {
	if source.StaffVersion != row.StaffVersion || source.EmploymentEpoch != row.EmploymentEpoch {
		return errPostingProposalConflict
	}
	generation, state, until, err := readPostingTargetGeneration(tx, p, s, row.GenerationID)
	if err != nil {
		return err
	}
	if (state != "effective" && state != "not_started") || generation.EmploymentEpoch != row.EmploymentEpoch || row.StartsAt.Before(generation.StartsAt) || !row.StartsAt.Before(until) || until.Before(row.EligibilityUntil) || generation.EndsAt != nil && (row.EndsAt == nil || row.EndsAt.After(*generation.EndsAt)) || !time.Now().Before(row.EligibilityUntil) {
		return errPostingProposalConflict
	}
	return nil
}

func readPostingSupervisor(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID) (result postingChainSource, err error) {
	var previous string
	if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_subject','app.workforce_hospital','app.workforce_posting_staff','app.workforce_posting']) AS key`).Scan(&previous).Error; err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, previous).Error)
	}()
	source, err := postingOwnerContext(tx, p, s, false, true)
	if err != nil {
		return result, err
	}
	result.Posting, err = postingProposalResult(tx, p, s, uuid.Nil)
	if err != nil {
		return result, err
	}
	if result.Posting.Status != "approved" || result.Posting.RevisionID != revision {
		return result, errPostingProposalConflict
	}
	intent, err := readPostingSupervisionIntent(tx, p.TenantID, s)
	if err != nil {
		return result, err
	}
	if intent == nil || (intent.Kind != "hospital_management" && intent.Kind != "department_supervisor") {
		return result, errPostingProposalConflict
	}
	result.Intent = *intent
	if err = validatePostingApprovalSource(tx, p, s, result.Posting, source); err != nil {
		return result, err
	}
	table := "workforce_posting_management_reviews"
	if intent.Kind != "hospital_management" {
		table = "workforce_posting_chain_reviews"
	}
	var count int64
	err = tx.Table(table).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?", p.TenantID, s.hospital, s.staff, s.draft, revision).Count(&count).Error
	if err == nil && count != 1 {
		err = errPostingProposalConflict
	}
	return
}

func (h *handlers) withSupervisedPostingApproval(ctx context.Context, tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, source EmploymentPeriodSource, intent StaffPostingSupervisionIntent, persist func(*gorm.DB, []postingChainSource) error) error {
	if h.departmentReferences == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if intent.Kind != "department_supervisor" && intent.Kind != "department_member" {
		return errPostingSupervisionRequired
	}
	if err := validatePostingApprovalSource(tx, p, s, row, source); err != nil {
		return err
	}
	refs := []authz.DepartmentReference{{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: row.DepartmentID, DepartmentRevision: row.DepartmentRevisionID}}
	sources := make([]postingChainSource, 0, postingChainLimit-1)
	seenStaff := map[uuid.UUID]bool{s.staff: true}
	seenPostings := map[uuid.UUID]bool{row.ID: true}
	current := row
	until := row.EligibilityUntil
	rooted := false
	for len(sources) < postingChainLimit-1 {
		if !validPostingSupervisionIntent(&intent) || intent.SupervisorStaffID == nil || intent.SupervisorProposalID == nil || intent.SupervisorRevisionID == nil {
			return errPostingProposalConflict
		}
		staff, proposal := *intent.SupervisorStaffID, *intent.SupervisorProposalID
		if seenStaff[staff] || seenPostings[proposal] {
			return errPostingProposalConflict
		}
		seenStaff[staff], seenPostings[proposal] = true, true
		ancestor, err := readPostingSupervisor(tx, p, postingDraftScope{hospital: s.hospital, staff: staff, draft: proposal}, *intent.SupervisorRevisionID)
		if err != nil {
			return err
		}
		parent := ancestor.Posting
		if current.StartsAt.Before(parent.StartsAt) || parent.EndsAt != nil && (current.EndsAt == nil || current.EndsAt.After(*parent.EndsAt)) {
			return errPostingProposalConflict
		}
		if parent.EligibilityUntil.Before(until) {
			until = parent.EligibilityUntil
		}
		ref := authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: parent.DepartmentID, DepartmentRevision: parent.DepartmentRevisionID}
		if ancestor.Intent.Kind == "department_supervisor" {
			ref = authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: current.DepartmentID, DepartmentRevision: current.DepartmentRevisionID, AncestorID: parent.DepartmentID, AncestorRevision: parent.DepartmentRevisionID}
		}
		refs = append(refs, ref)
		sources = append(sources, ancestor)
		if ancestor.Intent.Kind == "hospital_management" {
			rooted = true
			break
		}
		current, intent = parent, ancestor.Intent
	}
	if !rooted || !row.StartsAt.Before(until) {
		return errPostingProposalConflict
	}
	if err := h.db.ConstrainGuardedCommand(ctx, p.TenantID, until); err != nil {
		return errors.Join(errPostingProposalConflict, err)
	}
	var hold func(context.Context, int) error
	hold = func(current context.Context, index int) error {
		if index < len(refs) {
			return h.departmentReferences.WithCurrentDepartment(current, refs[index], func(held context.Context) error { return hold(held, index+1) })
		}
		return h.db.WithTenantCommand(current, p.TenantID, func(guarded *gorm.DB) error {
			if err := guarded.Exec(`SELECT set_config('app.workforce_posting_target_generation',?,true),set_config('app.workforce_posting_target_until',?,true),set_config('app.workforce_posting_department',?,true),set_config('app.workforce_posting_department_revision',?,true),set_config('app.workforce_posting_chain_approval','verified',true),set_config('app.workforce_posting_chain_until',?,true)`, row.GenerationID.String(), row.EligibilityUntil.UTC().Format(time.RFC3339Nano), row.DepartmentID.String(), row.DepartmentRevisionID.String(), until.UTC().Format(time.RFC3339Nano)).Error; err != nil {
				return err
			}
			return persist(guarded, sources)
		})
	}
	return hold(ctx, 0)
}

func recordPostingChainReview(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision, initial uuid.UUID, sources []postingChainSource) error {
	if err := tx.Exec(`INSERT INTO workforce_posting_chain_reviews(tenant_id,hospital_id,staff_id,proposal_id,revision_id,initial_revision_id,reviewer_subject,ancestor_count,eligibility_until) VALUES(?,?,?,?,?,?,?,?,current_setting('app.workforce_posting_chain_until')::timestamptz)`, p.TenantID, s.hospital, s.staff, s.draft, revision, initial, p.Subject, len(sources)).Error; err != nil {
		return err
	}
	for index, source := range sources {
		row, intent := source.Posting, source.Intent
		if err := tx.Exec(`INSERT INTO workforce_posting_chain_sources(tenant_id,hospital_id,staff_id,proposal_id,revision_id,hop,ancestor_staff_id,ancestor_proposal_id,ancestor_revision_id,generation_id,staff_version,employment_epoch,kind,department_id,department_revision_id,starts_at,ends_at,eligibility_until,supervisor_staff_id,supervisor_proposal_id,supervisor_revision_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, index+1, row.StaffID, row.ID, row.RevisionID, row.GenerationID, row.StaffVersion, row.EmploymentEpoch, intent.Kind, row.DepartmentID, row.DepartmentRevisionID, row.StartsAt, row.EndsAt, row.EligibilityUntil, intent.SupervisorStaffID, intent.SupervisorProposalID, intent.SupervisorRevisionID).Error; err != nil {
			return err
		}
	}
	return nil
}
