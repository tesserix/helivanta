package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

type postingReviewScopeRow struct {
	Scope     StaffPostingReviewHospitalScope
	Candidate leaveReviewScopeRow
}

func (r postingReviewScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Scope) }
func (r postingReviewScopeRow) PageKey() (time.Time, uuid.UUID) { return r.Candidate.PageKey() }

func (h *handlers) postingReviewScopeCandidates(c *gin.Context, page pagination.Params) ([]postingReviewScopeRow, error) {
	c.Header("Cache-Control", "no-store")
	if _, ok := postingAccount(c); !ok {
		return nil, nil
	}
	if _, ok := h.scopeAuthority.(authz.CompoundScopedCommandGuard); !ok || h.staffActors == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	candidates, err := h.postingScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]postingReviewScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, postingReviewScopeRow{Scope: StaffPostingReviewHospitalScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate.Candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedPostingReviewScopes(c *gin.Context, candidates []postingReviewScopeRow) ([]postingReviewScopeRow, error) {
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	markScopedPosting(c)
	rows := make([]postingReviewScopeRow, 0, len(candidates))
	for _, row := range candidates {
		scope := postingDraftScope{hospital: row.Scope.HospitalID, staff: row.Candidate.StaffID}
		permitted := true
		for _, permission := range []authz.Permission{PermPostingProposalRead, PermPostingSubmit, PermPostingReview} {
			err := h.scopedPostingTransaction(c, p, scope, false, permission, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
				return postingProposalAudit(tx, p, scope, "scope_read", 0)
			})
			if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) {
				if permission == PermPostingProposalRead {
					permitted = false
					break
				}
				continue
			}
			if err != nil {
				respondPostingProposalError(c, err)
				return nil, nil
			}
			switch permission {
			case PermPostingSubmit:
				row.Scope.CanSubmit = true
			case PermPostingReview:
				row.Scope.CanReview = true
			}
		}
		if permitted {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
