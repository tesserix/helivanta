package workforce

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func readCurrentPostingNode(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID, lifecycle *postingLifecycleReview) (result postingChainSource, until time.Time, err error) {
	restore, err := savePostingSourceFrame(tx, []string{"app.workforce_subject", "app.workforce_hospital", "app.workforce_posting_staff", "app.workforce_posting"})
	if err != nil {
		return result, until, err
	}
	defer func() { err = errors.Join(err, restore()) }()
	source, err := postingOwnerContext(tx, p, s, false, true)
	if err != nil {
		return result, until, err
	}
	result.Posting, err = postingProposalResult(tx, p, s, uuid.Nil)
	if err != nil {
		return result, until, err
	}
	if result.Posting.Status != "approved" || result.Posting.RevisionID != revision {
		return result, until, authz.ErrScopedCommandDenied
	}
	intent, err := readPostingSupervisionIntent(tx, p.TenantID, s)
	if err != nil {
		return result, until, err
	}
	if intent == nil || !validPostingSupervisionIntent(intent) {
		return result, until, authz.ErrScopedCommandDenied
	}
	result.Intent = *intent
	generation, state, generationUntil, err := readPostingTargetGeneration(tx, p, s, result.Posting.GenerationID)
	if err != nil {
		return result, until, err
	}
	until, err = currentPostingUseDeadline(result.Posting, source, generation, state, generationUntil, lifecycle.CheckedAt)
	if err != nil {
		return result, until, err
	}
	cutoff, err := readPostingRetainedCutoff(tx, p, s)
	if err != nil {
		return result, until, err
	}
	if err = lifecycle.observe(result.Posting, cutoff); err != nil {
		return result, until, err
	}
	if until.Before(lifecycle.EffectiveUntil) {
		lifecycle.EffectiveUntil = until
	}
	table := "workforce_posting_chain_reviews"
	if intent.Kind == "hospital_management" {
		table = "workforce_posting_management_reviews"
	}
	var count int64
	if err = tx.Table(table).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?", p.TenantID, s.hospital, s.staff, s.draft, revision).Count(&count).Error; err != nil {
		return result, until, err
	}
	if count != 1 {
		return result, until, authz.ErrScopedCommandDenied
	}
	return
}

func validatePostingSourceChainProof(tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, plan postingSourcePlan) error {
	if len(plan.Sources) == 0 {
		return nil
	}
	var header struct {
		AncestorCount    int
		EligibilityUntil time.Time
	}
	if err := tx.Table("workforce_posting_chain_reviews").Select("ancestor_count,eligibility_until").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?", p.TenantID, s.hospital, s.staff, s.draft, row.RevisionID).Take(&header).Error; err != nil {
		return err
	}
	if header.AncestorCount != len(plan.Sources) || !header.EligibilityUntil.Equal(plan.EligibilityUntil) {
		return authz.ErrScopedCommandDenied
	}
	var sources []struct {
		Hop                                                     int
		AncestorStaffID, AncestorProposalID, AncestorRevisionID uuid.UUID
	}
	if err := tx.Table("workforce_posting_chain_sources").Select("hop,ancestor_staff_id,ancestor_proposal_id,ancestor_revision_id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?", p.TenantID, s.hospital, s.staff, s.draft, row.RevisionID).Order("hop").Limit(postingChainLimit).Find(&sources).Error; err != nil {
		return err
	}
	if len(sources) != len(plan.Sources) {
		return authz.ErrScopedCommandDenied
	}
	for index, source := range sources {
		expected := plan.Sources[index].Posting
		if source.Hop != index+1 || source.AncestorStaffID != expected.StaffID || source.AncestorProposalID != expected.ID || source.AncestorRevisionID != expected.RevisionID {
			return authz.ErrScopedCommandDenied
		}
	}
	return nil
}
