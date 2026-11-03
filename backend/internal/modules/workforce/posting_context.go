package workforce

import (
	"context"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func postingContextRequest(c *gin.Context) (StaffPostingProposalContextRequest, bool) {
	var in StaffPostingProposalContextRequest
	var query url.Values
	var err error
	if len(c.Request.URL.RawQuery) <= 1024 {
		query, err = url.ParseQuery(c.Request.URL.RawQuery)
	}
	valid := len(c.Request.URL.RawQuery) <= 1024 && err == nil && len(query) == 3
	for _, key := range []string{"purpose", "expected_version", "expected_revision_id"} {
		valid = valid && len(query[key]) == 1
	}
	in.Purpose = query.Get("purpose")
	version, versionErr := strconv.ParseInt(query.Get("expected_version"), 10, 64)
	in.ExpectedVersion = &version
	in.ExpectedRevisionID, err = uuid.Parse(query.Get("expected_revision_id"))
	valid = valid && (in.Purpose == "review" || in.Purpose == "submission") && versionErr == nil &&
		version >= 1 && version <= 9007199254740990 && strconv.FormatInt(version, 10) == query.Get("expected_version") &&
		err == nil && postingV7(in.ExpectedRevisionID)
	if !valid {
		respond.Error(c, 400, "invalid_request", "Choose the exact proposal version, revision and review purpose.")
	}
	return in, valid
}

func (h *handlers) getPostingProposalContext(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.scopeAuthority == nil || h.staffActors == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	in, ok := postingContextRequest(c)
	if !ok {
		return
	}
	permission, decision := PermPostingReview, "reject"
	if in.Purpose == "submission" {
		permission, decision = PermPostingSubmit, "withdraw"
	}
	out := StaffPostingProposalContext{Purpose: in.Purpose}
	err := h.scopedPostingTransaction(c, p, s, true, permission, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := postingProposalDecisionContext(tx, s, decision, in.ExpectedRevisionID); err != nil {
			return err
		}
		var err error
		out.Proposal, err = postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		if out.Proposal.Version != *in.ExpectedVersion || out.Proposal.RevisionID != in.ExpectedRevisionID {
			return errPostingProposalConflict
		}
		if err := authorizePostingProposalDecider(tx, p, s, decision); err != nil {
			return err
		}
		draftScope := s
		draftScope.draft = out.Proposal.DraftID
		out.Draft, err = postingDraftResult(tx, p, draftScope, out.Proposal.DraftRevisionID)
		if err != nil {
			return err
		}
		var reasons struct {
			DraftReason, SubmissionReason string
			DecisionReason                *string
		}
		err = tx.Raw(`SELECT draft.reason AS draft_reason,submitted.reason AS submission_reason,
 CASE WHEN selected.version>1 THEN selected.reason END AS decision_reason
 FROM workforce_posting_proposal_revisions submitted
 JOIN workforce_posting_draft_revisions draft ON
 (draft.tenant_id,draft.hospital_id,draft.staff_id,draft.posting_id,draft.id,draft.version)=
 (submitted.tenant_id,submitted.hospital_id,submitted.staff_id,submitted.draft_id,submitted.draft_revision_id,submitted.draft_version)
 JOIN workforce_posting_proposal_revisions selected ON
 (selected.tenant_id,selected.hospital_id,selected.staff_id,selected.proposal_id)=
 (submitted.tenant_id,submitted.hospital_id,submitted.staff_id,submitted.proposal_id)
 WHERE submitted.tenant_id=? AND submitted.hospital_id=? AND submitted.staff_id=? AND submitted.proposal_id=?
 AND submitted.version=1 AND selected.id=?`, p.TenantID, s.hospital, s.staff, s.draft, in.ExpectedRevisionID).Take(&reasons).Error
		if err != nil {
			return err
		}
		out.DraftReason, out.SubmissionReason, out.DecisionReason = reasons.DraftReason, reasons.SubmissionReason, reasons.DecisionReason
		return postingProposalAudit(tx, p, s, "context_read_"+in.Purpose, out.Proposal.Version)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	respond.OK(c, out)
}
