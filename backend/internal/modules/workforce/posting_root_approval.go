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

func (h *handlers) withRootPostingApproval(ctx context.Context, tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, source EmploymentPeriodSource, persist func(*gorm.DB) error) error {
	intent, err := readPostingSupervisionIntent(tx, p.TenantID, s)
	if err != nil {
		return err
	}
	if intent == nil || intent.Kind != "hospital_management" {
		return errPostingSupervisionRequired
	}
	if h.departmentReferences == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if err := validatePostingApprovalSource(tx, p, s, row, source); err != nil {
		return err
	}
	approvalUntil := row.EligibilityUntil
	if row.EndsAt != nil && row.EndsAt.Before(approvalUntil) {
		approvalUntil = *row.EndsAt
	}
	if err := h.db.ConstrainGuardedCommand(ctx, p.TenantID, approvalUntil); err != nil {
		return errors.Join(errPostingProposalConflict, err)
	}
	ref := authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: row.DepartmentID, DepartmentRevision: row.DepartmentRevisionID, AncestorID: row.DepartmentID, AncestorRevision: row.DepartmentRevisionID}
	return h.departmentReferences.WithCurrentDepartment(ctx, ref, func(held context.Context) error {
		return h.db.WithTenantCommand(held, p.TenantID, func(current *gorm.DB) error {
			if err := current.Exec(`SELECT set_config('app.workforce_posting_target_generation',?,true),set_config('app.workforce_posting_target_until',?,true),set_config('app.workforce_posting_department',?,true),set_config('app.workforce_posting_department_revision',?,true),set_config('app.workforce_posting_root_approval','verified',true)`, row.GenerationID.String(), row.EligibilityUntil.UTC().Format(time.RFC3339Nano), row.DepartmentID.String(), row.DepartmentRevisionID.String()).Error; err != nil {
				return err
			}
			return persist(current)
		})
	})
}

func recordPostingManagementReview(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision, initial uuid.UUID) error {
	return tx.Exec(`INSERT INTO workforce_posting_management_reviews(tenant_id,hospital_id,staff_id,proposal_id,revision_id,initial_revision_id,reviewer_subject,reviewer_staff_id,reviewer_link_id,reviewer_generation_id,review_assignment_id,review_assignment_version,review_scope_epoch,review_membership_epoch,management_assignment_id,management_assignment_version,management_scope_epoch,management_membership_epoch,hospital_management_reviewed)
 VALUES(?,?,?,?,?,?,?,current_setting('app.workforce_posting_actor')::uuid,current_setting('app.workforce_posting_link')::uuid,current_setting('app.workforce_posting_generation')::uuid,current_setting('app.workforce_posting_assignment')::uuid,current_setting('app.workforce_posting_assignment_version')::bigint,current_setting('app.workforce_posting_scope_epoch')::bigint,current_setting('app.workforce_posting_membership_epoch')::bigint,current_setting('app.workforce_posting_management_assignment')::uuid,current_setting('app.workforce_posting_management_assignment_version')::bigint,current_setting('app.workforce_posting_management_scope_epoch')::bigint,current_setting('app.workforce_posting_management_membership_epoch')::bigint,true)`, p.TenantID, s.hospital, s.staff, s.draft, revision, initial, p.Subject).Error
}
