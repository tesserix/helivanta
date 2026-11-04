package workforce

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

type postingLifecycleReview struct {
	CheckedAt      time.Time
	EffectiveUntil time.Time
	Cutoffs        map[uuid.UUID]StaffPostingCutoffObservation
}

func newPostingLifecycleReview(tx *gorm.DB) (*postingLifecycleReview, error) {
	view := &postingLifecycleReview{Cutoffs: make(map[uuid.UUID]StaffPostingCutoffObservation)}
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&view.CheckedAt).Error; err != nil {
		return nil, err
	}
	if view.CheckedAt.IsZero() {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	view.CheckedAt = view.CheckedAt.UTC()
	return view, nil
}

func (view *postingLifecycleReview) observe(row StaffPostingProposal, cutoff *postingRetainedCutoff) error {
	if _, duplicate := view.Cutoffs[row.ID]; duplicate || view.CheckedAt.IsZero() {
		return errPostingProposalConflict
	}
	observation := StaffPostingCutoffObservation{}
	until := postingApprovalDeadline(row)
	if cutoff != nil {
		if !postingV7(cutoff.ID) || cutoff.Version < 1 || cutoff.Version > 9007199254740990 || cutoff.ExpectedRevisionID != row.RevisionID || cutoff.RecordedAt.IsZero() || cutoff.RecordedAt.After(view.CheckedAt) || cutoff.CutoffAt.IsZero() {
			return authz.ErrScopedAuthorizationUnavailable
		}
		instant := cutoff.CutoffAt.UTC()
		observation = StaffPostingCutoffObservation{ID: &cutoff.ID, Version: cutoff.Version, CutoffAt: &instant}
		if cutoff.CutoffAt.Before(until) {
			until = cutoff.CutoffAt
		}
	}
	if !view.CheckedAt.Before(until) || !row.StartsAt.Before(until) {
		return errPostingProposalConflict
	}
	view.Cutoffs[row.ID] = observation
	if view.EffectiveUntil.IsZero() || until.Before(view.EffectiveUntil) {
		view.EffectiveUntil = until.UTC()
	}
	return nil
}

func postingLifecycleReviewView(tx *gorm.DB, p authn.Principal, s postingDraftScope, selected StaffPostingProposalContext, intent StaffPostingSupervisionIntent, sources []postingChainSource, lifecycle *postingLifecycleReview) (StaffPostingLifecycleReviewContext, error) {
	view := StaffPostingLifecycleReviewContext{Context: selected, Supervision: intent, Cutoff: lifecycle.Cutoffs[selected.Proposal.ID], Chain: make([]StaffPostingLifecycleChainNode, 0, len(sources)), CheckedAt: lifecycle.CheckedAt, EffectiveUntil: lifecycle.EffectiveUntil}
	if len(lifecycle.Cutoffs) != 1+len(sources) {
		return view, authz.ErrScopedAuthorizationUnavailable
	}
	if len(sources) > 0 {
		named, err := postingChainReviewView(tx, p, s, selected, intent, sources)
		if err != nil {
			return view, err
		}
		for _, node := range named.Chain {
			cutoff, found := lifecycle.Cutoffs[node.Posting.ID]
			if !found {
				return view, authz.ErrScopedAuthorizationUnavailable
			}
			view.Chain = append(view.Chain, StaffPostingLifecycleChainNode{Posting: node.Posting, Supervision: node.Supervision, Cutoff: cutoff})
		}
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		return view, err
	}
	if len(encoded) > 128*1024 {
		return view, authz.ErrScopedAuthorizationUnavailable
	}
	return view, nil
}
