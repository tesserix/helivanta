package workforce

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func validPostingLifecycleDecision(in *StaffPostingLifecycleReviewInput) bool {
	if in == nil {
		return true
	}
	in.EffectiveUntil = in.EffectiveUntil.UTC().Truncate(time.Microsecond)
	if len(in.CutoffVersions) < 1 || len(in.CutoffVersions) > postingChainLimit || in.CutoffVersions[0] != 0 || in.EffectiveUntil.IsZero() || in.EffectiveUntil.Year() < 1 || in.EffectiveUntil.Year() > 9999 {
		return false
	}
	for _, version := range in.CutoffVersions {
		if version < 0 || version > 9007199254740990 {
			return false
		}
	}
	return true
}

func (view *postingLifecycleReview) matchesDecision(row StaffPostingProposal, chain []postingChainSource, in *StaffPostingLifecycleReviewInput) bool {
	if view == nil || in == nil || len(in.CutoffVersions) != len(chain)+1 || len(view.Cutoffs) != len(chain)+1 || !in.EffectiveUntil.Equal(view.EffectiveUntil) {
		return false
	}
	selected, found := view.Cutoffs[row.ID]
	if !found || selected.Version != in.CutoffVersions[0] {
		return false
	}
	for index, source := range chain {
		observation, found := view.Cutoffs[source.Posting.ID]
		if !found || observation.Version != in.CutoffVersions[index+1] {
			return false
		}
	}
	return true
}

func recordPostingLifecycleReview(ctx context.Context, tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID, row StaffPostingProposal, chain []postingChainSource, view *postingLifecycleReview) error {
	permission := PermPostingManagementReview
	if len(chain) > 0 {
		permission = PermPostingRead
	}
	grant, current := authz.ScopedCommandGrant(ctx, authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital.String(), Permission: permission})
	if !current {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_posting_lifecycle_support_permission',?,true),set_config('app.workforce_posting_lifecycle_support_assignment',?,true),set_config('app.workforce_posting_lifecycle_support_version',?,true),set_config('app.workforce_posting_lifecycle_support_scope_epoch',?,true),set_config('app.workforce_posting_lifecycle_support_membership_epoch',?,true)`, string(permission), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), strconv.FormatInt(grant.ScopeEpoch, 10), strconv.FormatInt(grant.MembershipEpoch, 10)).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_posting_lifecycle_reviews(tenant_id,hospital_id,staff_id,proposal_id,revision_id,initial_revision_id,reviewer_subject,reviewer_staff_id,reviewer_link_id,reviewer_generation_id,review_assignment_id,review_assignment_version,scope_epoch,membership_epoch,support_permission,support_assignment_id,support_assignment_version,source_count,checked_at,effective_until)
 VALUES(?,?,?,?,?,?,?,current_setting('app.workforce_posting_actor')::uuid,current_setting('app.workforce_posting_link')::uuid,current_setting('app.workforce_posting_generation')::uuid,current_setting('app.workforce_posting_assignment')::uuid,current_setting('app.workforce_posting_assignment_version')::bigint,current_setting('app.workforce_posting_scope_epoch')::bigint,current_setting('app.workforce_posting_membership_epoch')::bigint,?,?,?,?,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, row.RevisionID, p.Subject, string(permission), grant.AssignmentID, grant.Version, len(chain)+1, view.CheckedAt, view.EffectiveUntil).Error; err != nil {
		return err
	}
	sources := make([]StaffPostingProposal, 1, len(chain)+1)
	sources[0] = row
	for _, source := range chain {
		sources = append(sources, source.Posting)
	}
	for hop, source := range sources {
		cutoff := view.Cutoffs[source.ID]
		if err := tx.Exec(`INSERT INTO workforce_posting_lifecycle_sources(tenant_id,hospital_id,staff_id,proposal_id,revision_id,hop,source_staff_id,source_proposal_id,source_revision_id,cutoff_id,cutoff_version,cutoff_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, hop, source.StaffID, source.ID, source.RevisionID, cutoff.ID, cutoff.Version, cutoff.CutoffAt).Error; err != nil {
			return err
		}
	}
	return nil
}
