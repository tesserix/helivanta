package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func validPostingTransferInput(in *StaffPostingTransferInput) bool {
	return postingV7(in.FromPostingID) && postingV7(in.FromRevisionID) && in.FromVersion != nil && *in.FromVersion >= 2 && *in.FromVersion <= 9007199254740990 && in.FromCutoffVersion != nil && *in.FromCutoffVersion >= 0 && *in.FromCutoffVersion <= 9007199254740989 && validPostingDecisionInput(&in.Decision) && in.Decision.Decision == "approve" && in.Decision.LifecycleReview != nil
}

// withTransferEnding uses the independently reserved cutoff grant; the reviewer grant cannot substitute for it.
func withTransferEnding(ctx context.Context, tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID, action func() error) (err error) {
	grant, ok := authz.ScopedCommandGrant(ctx, authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital.String(), Permission: PermPostingCutoff})
	if !ok || grant.ScopeKind != "hospital" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	restore, err := savePostingSourceFrame(tx, []string{"app.workforce_posting_cutoff", "app.workforce_posting_proposal", "app.workforce_posting_expected_revision", "app.workforce_posting_permission", "app.workforce_posting_assignment", "app.workforce_posting_assignment_version", "app.workforce_posting_scope_epoch", "app.workforce_posting_membership_epoch"})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()
	if err = tx.Exec(`SELECT set_config('app.workforce_posting_permission',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true)`, string(PermPostingCutoff), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), strconv.FormatInt(grant.ScopeEpoch, 10), strconv.FormatInt(grant.MembershipEpoch, 10)).Error; err != nil {
		return err
	}
	if err = postingCutoffContext(tx, s, true, revision); err != nil {
		return err
	}
	return action()
}

func (h *handlers) putPostingTransfer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	p, s, ok := postingRequestScope(c)
	if !ok {
		return
	}
	proposal, parseErr := uuid.Parse(c.Param("proposal"))
	if parseErr != nil || !postingV7(proposal) {
		respond.NotFound(c, "Staff posting proposal")
		return
	}
	s.draft = proposal
	s.department, ok = postingDepartmentHint(c)
	if !ok {
		return
	}
	if err := h.teamHospital(c.Request.Context(), p, s.hospital); err != nil {
		respondPostingProposalError(c, err)
		return
	}
	if h.scopeAuthority == nil || h.staffActors == nil || h.departmentReferences == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	// This coordinated command requires hospital-wide review and ending authority across both departments.
	if s.department != uuid.Nil {
		respond.Error(c, 400, "invalid_request", "Open hospital-wide review to coordinate a transfer.")
		return
	}
	var in StaffPostingTransferInput
	if !decodeSized(c, &in, 8192) {
		return
	}
	if !validPostingTransferInput(&in) || in.FromPostingID == s.draft {
		respond.Error(c, 400, "invalid_request", "Review the exact old posting, cutoff and replacement lifecycle before approving a transfer.")
		return
	}
	material, err := json.Marshal(struct {
		Hospital, Staff, Proposal uuid.UUID
		Transfer                  StaffPostingTransferInput
	}{s.hospital, s.staff, s.draft, in})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	digest := sha256.Sum256(material)
	fingerprint := hex.EncodeToString(digest[:])
	additional := []authz.Permission{PermPostingRead, PermPostingCutoff, authz.StaffPostingSourceUse}
	if in.Decision.HospitalManagementReviewed != nil {
		additional[0] = PermPostingManagementReview
	}
	var out StaffPostingTransferReceipt
	old := s
	old.draft = in.FromPostingID
	started := time.Now()
	stage := "scope_authorization"
	err = h.scopedPostingTransaction(c, p, s, true, PermPostingReview, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
		stage = "proposal_basis"
		if err := postingProposalDecisionContext(tx, s, "approve", in.Decision.ExpectedRevisionID); err != nil {
			return err
		}
		var receipt struct {
			ID, ProposalID          uuid.UUID
			ActorSubject, InputHash string
		}
		err := tx.Table("workforce_posting_proposal_revisions").Select("id,proposal_id,actor_subject,input_hash").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND command_id=?", p.TenantID, s.hospital, s.staff, s.command).Take(&receipt).Error
		if err == nil {
			if receipt.ProposalID != s.draft || receipt.ActorSubject != p.Subject || receipt.InputHash != fingerprint {
				return errPostingProposalConflict
			}
			out.Replacement, err = postingProposalResult(tx, p, s, receipt.ID)
			if err != nil {
				return err
			}
			err = withTransferEnding(ctx, tx, p, old, in.FromRevisionID, func() error {
				var ending struct {
					StaffPostingCutoff
					ActorSubject, InputHash string
				}
				if e := postingCutoffQuery(tx, p, old).Select(postingCutoffProjection+",actor_subject,input_hash").Where("id=?", s.command).Take(&ending).Error; e != nil {
					return e
				}
				if ending.ActorSubject != p.Subject || ending.InputHash != fingerprint {
					return errPostingCutoffConflict
				}
				out.Ending = publicPostingCutoff(ending.StaffPostingCutoff)
				return postingProposalAudit(tx, p, old, "cutoff_receipt_read", out.Ending.Version)
			})
			if err != nil {
				return err
			}
			return postingProposalAudit(tx, p, s, "receipt_read", out.Replacement.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		if row.Status != "pending" || row.Version != *in.Decision.ExpectedVersion || row.RevisionID != in.Decision.ExpectedRevisionID {
			return errPostingProposalConflict
		}
		if err = authorizePostingProposalDecider(tx, p, s, "approve"); err != nil {
			return err
		}
		lifecycle, err := newPostingLifecycleReview(tx)
		if err != nil {
			return err
		}
		if !row.StartsAt.After(lifecycle.CheckedAt) {
			return errPostingProposalConflict
		}
		var previous StaffPostingProposal
		err = withTransferEnding(ctx, tx, p, old, in.FromRevisionID, func() error {
			var e error
			previous, e = postingApprovedCutoffProposal(tx, p, old, in.FromRevisionID)
			if e != nil {
				return e
			}
			if previous.Version != *in.FromVersion {
				return errPostingCutoffConflict
			}
			var consumed bool
			if e = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_posting_cutoffs c JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.command_id,r.input_hash)=(c.tenant_id,c.hospital_id,c.staff_id,c.id,c.input_hash) WHERE c.tenant_id=? AND c.hospital_id=? AND c.staff_id=? AND c.proposal_id=? AND r.status='approved')`, p.TenantID, s.hospital, s.staff, old.draft).Scan(&consumed).Error; e != nil {
				return e
			}
			if consumed {
				return errPostingCutoffConflict
			}
			return nil
		})
		if err != nil {
			return err
		}
		intent, err := readPostingSupervisionIntent(tx, p.TenantID, s)
		if err != nil {
			return err
		}
		if intent == nil {
			return errPostingSupervisionRequired
		}
		guard := postingSourceGuard{db: h.db, departments: h.departmentReferences, actors: h.staffActors}
		persist := func(current *gorm.DB, chain []postingChainSource) error {
			for _, ancestor := range chain {
				if ancestor.Posting.ID == old.draft {
					return errPostingProposalConflict
				}
			}
			stage = "original_posting_source"
			sourceErr := guard.WithCurrentStaffPosting(ctx, authz.StaffPostingSourceScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: s.hospital, StaffID: s.staff, PostingID: previous.ID, RevisionID: previous.RevisionID, DepartmentID: previous.DepartmentID, DepartmentRevisionID: previous.DepartmentRevisionID, Version: previous.Version}, func(held context.Context, ref authz.StaffPostingSourceReference) error {
				if row.StartsAt.After(ref.SourceUntil) {
					return errPostingProposalConflict
				}
				if err := h.db.ConstrainGuardedCommand(held, p.TenantID, row.StartsAt); err != nil {
					return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
				}
				stage = "ending_original"
				if err := withTransferEnding(held, current, p, old, in.FromRevisionID, func() error {
					out.Ending, err = h.recordPostingCutoff(current, p, old, StaffPostingCutoffInput{ExpectedRevisionID: in.FromRevisionID, ExpectedCutoffVersion: in.FromCutoffVersion, Mode: "scheduled", CutoffAt: &row.StartsAt, Reason: in.Decision.Reason, SourcesExamined: in.Decision.SourcesExamined}, fingerprint)
					return err
				}); err != nil {
					return err
				}
				stage = "replacement_decision"
				out.Replacement, err = h.recordPostingDecision(held, current, p, s, row, in.Decision, fingerprint, chain, lifecycle)
				return err
			})
			if errors.Is(sourceErr, authz.ErrScopedCommandDenied) {
				return errPostingProposalConflict
			}
			return sourceErr
		}
		stage = "supervision_sources"
		if intent.Kind == "hospital_management" {
			if in.Decision.HospitalManagementReviewed == nil {
				return errPostingSupervisionRequired
			}
			return h.withRootPostingLifecycleApproval(ctx, tx, p, s, row, source, lifecycle, func(current *gorm.DB) error { return persist(current, nil) })
		}
		if in.Decision.HospitalManagementReviewed != nil {
			return errPostingSupervisionRequired
		}
		return h.withSupervisedPostingLifecycleApproval(ctx, tx, p, s, row, source, *intent, lifecycle, persist)
	}, additional...)
	if err != nil {
		if errors.Is(err, authz.ErrScopedAuthorizationUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			requestid.Logger(c).WarnContext(c.Request.Context(), "posting transfer unavailable", "stage", stage, "elapsed_ms", time.Since(started).Milliseconds(), "deadline", errors.Is(err, context.DeadlineExceeded))
		}
		respondPostingCutoffError(c, err)
		return
	}
	out.Ending = publicPostingCutoff(out.Ending)
	respond.OK(c, out)
}
