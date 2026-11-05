package workforce

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r StaffPostingCutoff) PageKey() (time.Time, uuid.UUID) { return r.RecordedAt, r.ID }

func postingCutoffSelection(c *gin.Context, history bool) (uuid.UUID, bool) {
	var query url.Values
	var err error
	if len(c.Request.URL.RawQuery) <= 4096 {
		query, err = url.ParseQuery(c.Request.URL.RawQuery)
	}
	valid := len(c.Request.URL.RawQuery) <= 4096 && err == nil && len(query["expected_revision_id"]) == 1
	for key, values := range query {
		valid = valid && len(values) == 1 && (key == "expected_revision_id" || history && contains(key, "limit", "cursor"))
	}
	revision, parseErr := uuid.Parse(query.Get("expected_revision_id"))
	valid = valid && parseErr == nil && postingV7(revision) && len(query.Get("cursor")) <= 2048
	if !valid {
		respond.Error(c, 400, "invalid_request", "Choose the exact approved posting revision and a bounded cutoff history page.")
		return uuid.Nil, false
	}
	return revision, true
}

func postingApprovedCutoffProposal(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID) (StaffPostingProposal, error) {
	row, err := postingProposalResult(tx, p, s, uuid.Nil)
	if err != nil {
		return row, err
	}
	if row.Status != "approved" || row.RevisionID != revision {
		return row, errPostingCutoffConflict
	}
	return row, nil
}

func (h *handlers) getPostingCutoffContext(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, s, ok := postingCutoffScope(c)
	if !ok {
		return
	}
	revision, ok := postingCutoffSelection(c, false)
	if !ok {
		return
	}
	var out StaffPostingCutoffContext
	err := h.scopedPostingTransaction(c, p, s, false, PermPostingCutoff, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := postingCutoffContext(tx, s, false, revision); err != nil {
			return err
		}
		var err error
		out.Proposal, err = postingApprovedCutoffProposal(tx, p, s, revision)
		if err != nil {
			return err
		}
		var cutoff StaffPostingCutoff
		err = postingCutoffQuery(tx, p, s).Select(postingCutoffProjection).Order("version DESC").Take(&cutoff).Error
		if err == nil {
			row := publicPostingCutoff(cutoff)
			out.Cutoff = &row
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&out.CheckedAt); err != nil {
			return err
		}
		out.CheckedAt = out.CheckedAt.UTC()
		return postingProposalAudit(tx, p, s, "cutoff_context_read", out.Proposal.Version)
	})
	if err != nil {
		respondPostingCutoffError(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) listPostingCutoffHistory(c *gin.Context, page pagination.Params) ([]StaffPostingCutoff, error) {
	c.Header("Cache-Control", "no-store")
	p, s, ok := postingCutoffScope(c)
	if !ok {
		return nil, nil
	}
	revision, ok := postingCutoffSelection(c, true)
	if !ok {
		return nil, nil
	}
	rows := []StaffPostingCutoff{}
	err := h.scopedPostingTransaction(c, p, s, false, PermPostingCutoff, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := postingCutoffContext(tx, s, false, revision); err != nil {
			return err
		}
		if _, err := postingApprovedCutoffProposal(tx, p, s, revision); err != nil {
			return err
		}
		query := postingCutoffQuery(tx, p, s).Select(postingCutoffProjection + ",recorded_at AS created_at")
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS cutoff_history", query).Select(postingCutoffProjection), page).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i] = publicPostingCutoff(rows[i])
		}
		return postingProposalAuditRecord(tx, p, s.hospital, s.draft, "cutoff_history_read", 0, map[string]any{"rows": min(len(rows), page.Limit)})
	})
	if err != nil {
		respondPostingCutoffError(c, err)
		return nil, nil
	}
	return rows, nil
}
