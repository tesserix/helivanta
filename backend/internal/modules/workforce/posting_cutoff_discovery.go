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

type postingCutoffScopeRow struct {
	Scope     StaffPostingCutoffHospitalScope
	Candidate leaveReviewScopeRow
}

func (r postingCutoffScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Scope) }
func (r postingCutoffScopeRow) PageKey() (time.Time, uuid.UUID) { return r.Candidate.PageKey() }

func (h *handlers) postingCutoffScopeCandidates(c *gin.Context, page pagination.Params) ([]postingCutoffScopeRow, error) {
	c.Header("Cache-Control", "no-store")
	if _, ok := postingAccount(c); !ok {
		return nil, nil
	}
	if _, ok := h.scopeAuthority.(authz.CompoundScopedCommandGuard); !ok || h.staffActors == nil || h.db == nil {
		respondPostingCutoffError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	candidates, err := h.postingScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]postingCutoffScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, postingCutoffScopeRow{Scope: StaffPostingCutoffHospitalScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate.Candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedPostingCutoffScopes(c *gin.Context, candidates []postingCutoffScopeRow) ([]postingCutoffScopeRow, error) {
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	markScopedPosting(c)
	rows := make([]postingCutoffScopeRow, 0, len(candidates))
	for _, row := range candidates {
		s := postingDraftScope{hospital: row.Scope.HospitalID, staff: row.Candidate.StaffID}
		err := h.scopedPostingTransaction(c, p, s, false, PermPostingCutoff, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
			return postingProposalAudit(tx, p, s, "cutoff_scope_read", 0)
		})
		if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			respondPostingCutoffError(c, err)
			return nil, nil
		}
		rows = append(rows, row)
	}
	return rows, nil
}
