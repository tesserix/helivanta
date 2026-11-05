package workforce

import (
	"context"
	"encoding/json"
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
	valid := len(c.Request.URL.RawQuery) <= 1024 && err == nil && (len(query) >= 3 && len(query) <= 6)
	for _, key := range []string{"purpose", "expected_version", "expected_revision_id"} {
		valid = valid && len(query[key]) == 1
	}
	if values, exists := query["supervision_context"]; exists {
		valid = valid && len(values) == 1 && values[0] == "v1"
		value := query.Get("supervision_context")
		in.SupervisionContext = &value
	}
	if values, exists := query["management_review"]; exists {
		valid = valid && len(values) == 1 && values[0] == "current" && in.SupervisionContext != nil && query.Get("purpose") == "review"
		value := query.Get("management_review")
		in.ManagementReview = &value
	}
	if values, exists := query["chain_review"]; exists {
		valid = valid && len(values) == 1 && values[0] == "current" && in.SupervisionContext != nil && in.ManagementReview == nil && query.Get("purpose") == "review"
		value := query.Get("chain_review")
		in.ChainReview = &value
	}
	if values, exists := query["lifecycle_review"]; exists {
		valid = valid && len(values) == 1 && (values[0] == "current" || values[0] == "recorded" && in.ManagementReview == nil) && in.SupervisionContext != nil && in.ChainReview == nil && query.Get("purpose") == "review"
		value := query.Get("lifecycle_review")
		in.LifecycleReview = &value
	}
	fields := 3
	if in.SupervisionContext != nil {
		fields++
	}
	if in.ManagementReview != nil {
		fields++
	}
	if in.ChainReview != nil {
		fields++
	}
	if in.LifecycleReview != nil {
		fields++
	}
	valid = valid && len(query) == fields
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
	recorded := in.LifecycleReview != nil && *in.LifecycleReview == "recorded"
	permission, decision := PermPostingReview, "reject"
	if in.Purpose == "submission" {
		permission, decision = PermPostingSubmit, "withdraw"
	}
	if in.ChainReview != nil || in.LifecycleReview != nil && !recorded {
		decision = "approve"
	}
	var additional []authz.Permission
	if in.ManagementReview != nil {
		additional = []authz.Permission{PermPostingManagementReview}
	}
	if in.ChainReview != nil || in.LifecycleReview != nil && !recorded && in.ManagementReview == nil {
		additional = append(additional, PermPostingRead)
	}
	out := StaffPostingProposalContext{Purpose: in.Purpose}
	var supervision *StaffPostingSupervisionIntent
	var chainView StaffPostingChainReviewContext
	var lifecycleView StaffPostingLifecycleReviewContext
	var historyView StaffPostingLifecycleHistoryContext
	err := h.scopedPostingTransaction(c, p, s, true, permission, func(commandCtx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
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
		if in.SupervisionContext != nil {
			supervision, err = readPostingSupervisionIntent(tx, p.TenantID, s)
			if err != nil {
				return err
			}
			if in.ManagementReview != nil && (supervision == nil || supervision.Kind != "hospital_management" || out.Proposal.Status != "pending") {
				return errPostingSupervisionRequired
			}
		}
		if recorded {
			historyView, err = postingLifecycleHistoryView(tx, p, s, out, supervision)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(historyView)
			if err != nil {
				return err
			}
			if len(encoded) > 128*1024 {
				return authz.ErrScopedAuthorizationUnavailable
			}
			sources := 0
			if historyView.Review != nil {
				sources = len(historyView.Review.Sources)
			}
			return postingProposalAuditRecord(tx, p, s.hospital, s.draft, "context_read_lifecycle_history", out.Proposal.Version, map[string]any{"sources": sources})
		}
		if in.LifecycleReview != nil {
			if supervision == nil || out.Proposal.Status != "pending" || (supervision.Kind == "hospital_management") != (in.ManagementReview != nil) {
				return errPostingSupervisionRequired
			}
			lifecycle, err := newPostingLifecycleReview(tx)
			if err != nil {
				return err
			}
			finish := func(current *gorm.DB, sources []postingChainSource) error {
				var err error
				lifecycleView, err = postingLifecycleReviewView(current, p, s, out, *supervision, sources, lifecycle)
				if err != nil {
					return err
				}
				return postingProposalAuditRecord(current, p, s.hospital, s.draft, "context_read_lifecycle_review", out.Proposal.Version, map[string]any{"ancestors": len(sources)})
			}
			if supervision.Kind == "hospital_management" {
				return h.withRootPostingLifecycleApproval(commandCtx, tx, p, s, out.Proposal, source, lifecycle, func(current *gorm.DB) error { return finish(current, nil) })
			}
			return h.withSupervisedPostingLifecycleApproval(commandCtx, tx, p, s, out.Proposal, source, *supervision, lifecycle, finish)
		}
		if in.ChainReview != nil {
			if supervision == nil || supervision.Kind == "hospital_management" || out.Proposal.Status != "pending" {
				return errPostingSupervisionRequired
			}
			return h.withSupervisedPostingApproval(commandCtx, tx, p, s, out.Proposal, source, *supervision, func(current *gorm.DB, sources []postingChainSource) error {
				var err error
				chainView, err = postingChainReviewView(current, p, s, out, *supervision, sources)
				if err != nil {
					return err
				}
				encoded, err := json.Marshal(chainView)
				if err != nil {
					return err
				}
				if len(encoded) > 128*1024 {
					return authz.ErrScopedAuthorizationUnavailable
				}
				return postingProposalAuditRecord(current, p, s.hospital, s.draft, "context_read_chain_review", out.Proposal.Version, map[string]any{"ancestors": len(sources)})
			})
		}
		if in.ManagementReview != nil {
			grant, current := authz.ScopedCommandGrant(commandCtx, authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital.String(), Permission: PermPostingManagementReview})
			if !current {
				return authz.ErrScopedAuthorizationUnavailable
			}
			return postingProposalAuditRecord(tx, p, s.hospital, s.draft, "context_read_management_review", out.Proposal.Version, map[string]any{"management_assignment_id": grant.AssignmentID, "management_assignment_version": grant.Version})
		}
		return postingProposalAudit(tx, p, s, "context_read_"+in.Purpose, out.Proposal.Version)
	}, additional...)
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	if recorded {
		respond.OK(c, historyView)
		return
	}
	if in.LifecycleReview != nil {
		respond.OK(c, lifecycleView)
		return
	}
	if in.ChainReview != nil {
		respond.OK(c, chainView)
		return
	}
	if in.SupervisionContext != nil {
		respond.OK(c, StaffPostingSupervisionContext{Context: out, Supervision: supervision})
		return
	}
	respond.OK(c, out)
}

func readPostingSupervisionIntent(tx *gorm.DB, tenant string, s postingDraftScope) (*StaffPostingSupervisionIntent, error) {
	var row struct {
		Requested                                                     bool
		InitialRevisionID                                             *uuid.UUID
		Kind                                                          string
		SupervisorStaffID, SupervisorProposalID, SupervisorRevisionID *uuid.UUID
	}
	err := tx.Raw(`SELECT r.supervision_requested AS requested,i.initial_revision_id,i.kind,i.supervisor_staff_id,i.supervisor_proposal_id,i.supervisor_revision_id
 FROM workforce_posting_proposal_revisions r
 LEFT JOIN workforce_posting_supervision_intents i ON
 (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.proposal_id=? AND r.version=1`, tenant, s.hospital, s.staff, s.draft).Take(&row).Error
	if err != nil {
		return nil, err
	}
	if !row.Requested {
		if row.InitialRevisionID != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		return nil, nil
	}
	intent := &StaffPostingSupervisionIntent{Kind: row.Kind, SupervisorStaffID: row.SupervisorStaffID, SupervisorProposalID: row.SupervisorProposalID, SupervisorRevisionID: row.SupervisorRevisionID}
	if row.InitialRevisionID == nil || !validPostingSupervisionIntent(intent) {
		// Missing retained evidence must never be interpreted as an unrequested intent.
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return intent, nil
}
