package workforce

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

var errPostingLifecycleHistoryInvalid = errors.New("retained posting lifecycle evidence is incomplete")

func postingLifecycleHistoryView(tx *gorm.DB, p authn.Principal, s postingDraftScope, base StaffPostingProposalContext, supervision *StaffPostingSupervisionIntent) (out StaffPostingLifecycleHistoryContext, err error) {
	out = StaffPostingLifecycleHistoryContext{Context: base, Supervision: supervision}
	if base.Proposal.Status != "approved" {
		return out, errPostingProposalConflict
	}
	var record StaffPostingLifecycleHistoryRecord
	err = tx.Raw(`SELECT revision_id,initial_revision_id,reviewer_staff_id,reviewer_link_id,reviewer_generation_id,
 review_assignment_id,review_assignment_version,scope_epoch,membership_epoch,support_permission,
 support_assignment_id,support_assignment_version,checked_at,effective_until,source_count
 FROM workforce_posting_lifecycle_reviews
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?`, p.TenantID, s.hospital, s.staff, s.draft, base.Proposal.RevisionID).Row().Scan(
		&record.RevisionID, &record.InitialRevisionID, &record.ReviewerStaffID, &record.ReviewerLinkID, &record.ReviewerGenerationID,
		&record.ReviewAssignmentID, &record.ReviewAssignmentVersion, &record.ScopeEpoch, &record.MembershipEpoch, &record.SupportPermission,
		&record.SupportAssignmentID, &record.SupportAssignmentVersion, &record.CheckedAt, &record.EffectiveUntil, &record.SourceCount)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if record.SourceCount < 1 || record.SourceCount > postingChainLimit {
		return out, errPostingLifecycleHistoryInvalid
	}
	count := int(record.SourceCount)
	rows, err := tx.Raw(`SELECT hop,source_staff_id,source_proposal_id,source_revision_id,cutoff_id,cutoff_version,cutoff_at
 FROM workforce_posting_lifecycle_sources WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND revision_id=?
 ORDER BY hop LIMIT 33`, p.TenantID, s.hospital, s.staff, s.draft, base.Proposal.RevisionID).Rows()
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	record.Sources = make([]StaffPostingLifecycleHistorySource, 0, count)
	for rows.Next() {
		var source StaffPostingLifecycleHistorySource
		if err := rows.Scan(&source.Hop, &source.StaffID, &source.ProposalID, &source.RevisionID, &source.Cutoff.ID, &source.Cutoff.Version, &source.Cutoff.CutoffAt); err != nil {
			return out, err
		}
		if source.Cutoff.CutoffAt != nil {
			utc := source.Cutoff.CutoffAt.UTC()
			source.Cutoff.CutoffAt = &utc
		}
		record.Sources = append(record.Sources, source)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(record.Sources) != count || supervision == nil || record.InitialRevisionID == record.RevisionID ||
		record.ReviewerStaffID == s.staff || !record.CheckedAt.Before(record.EffectiveUntil) ||
		!base.Proposal.StartsAt.Before(record.EffectiveUntil) || record.CheckedAt.After(base.Proposal.RecordedAt) ||
		record.EffectiveUntil.After(base.Proposal.EligibilityUntil) || base.Proposal.EndsAt != nil && record.EffectiveUntil.After(*base.Proposal.EndsAt) {
		return out, errPostingLifecycleHistoryInvalid
	}
	root := supervision.Kind == "hospital_management"
	if root && (count != 1 || record.SupportPermission != "workforce.posting.management.review" || record.ReviewAssignmentID == record.SupportAssignmentID) ||
		!root && (count < 2 || record.SupportPermission != "workforce.posting.read") {
		return out, errPostingLifecycleHistoryInvalid
	}
	staffIDs, proposalIDs := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for index, source := range record.Sources {
		if source.Hop != int64(index) || staffIDs[source.StaffID] || proposalIDs[source.ProposalID] {
			return out, errPostingLifecycleHistoryInvalid
		}
		staffIDs[source.StaffID], proposalIDs[source.ProposalID] = true, true
		if index == 0 && (source.StaffID != s.staff || source.ProposalID != s.draft || source.RevisionID != record.InitialRevisionID || source.Cutoff.Version != 0) {
			return out, errPostingLifecycleHistoryInvalid
		}
		if index == 1 && (supervision.SupervisorStaffID == nil || supervision.SupervisorProposalID == nil || supervision.SupervisorRevisionID == nil ||
			source.StaffID != *supervision.SupervisorStaffID || source.ProposalID != *supervision.SupervisorProposalID || source.RevisionID != *supervision.SupervisorRevisionID) {
			return out, errPostingLifecycleHistoryInvalid
		}
		if source.Cutoff.Version == 0 && (source.Cutoff.ID != nil || source.Cutoff.CutoffAt != nil) || source.Cutoff.Version > 0 && (source.Cutoff.ID == nil || source.Cutoff.CutoffAt == nil || record.EffectiveUntil.After(*source.Cutoff.CutoffAt)) {
			return out, errPostingLifecycleHistoryInvalid
		}
	}
	record.CheckedAt, record.EffectiveUntil = record.CheckedAt.UTC(), record.EffectiveUntil.UTC()
	out.Review = &record
	return out, nil
}
