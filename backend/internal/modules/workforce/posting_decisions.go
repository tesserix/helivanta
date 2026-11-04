package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

var errPostingSupervisionRequired = errors.New("posting supervision policy required")

func postingProposalDecisionContext(tx *gorm.DB, s postingDraftScope, decision string, revision uuid.UUID) error {
	return tx.Exec(`SELECT set_config('app.workforce_posting_review_mode',?,true),set_config('app.workforce_posting_proposal',?,true),set_config('app.workforce_posting_expected_revision',?,true)`, decision, s.draft.String(), revision.String()).Error
}

func authorizePostingProposalDecider(tx *gorm.DB, p authn.Principal, s postingDraftScope, decision string) error {
	if err := generationContext(tx, s.staff, "read"); err != nil {
		return err
	}
	var allowed bool
	if err := tx.Raw(`SELECT workforce_posting_proposal_decider(?::uuid,?::uuid,?::uuid,?::uuid,?)`, p.TenantID, s.hospital, s.staff, s.draft, decision).Scan(&allowed).Error; err != nil {
		return err
	}
	if !allowed {
		return authz.ErrScopedCommandDenied
	}
	return nil
}

func validPostingDecisionInput(in *StaffPostingDecisionInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.HospitalManagementReviewed != nil {
		if *in.HospitalManagementReviewed && in.Decision != "approve" {
			return false
		}
		if !*in.HospitalManagementReviewed {
			in.HospitalManagementReviewed = nil
		}
	}
	return in.ExpectedVersion != nil && *in.ExpectedVersion >= 1 && *in.ExpectedVersion <= 9007199254740989 &&
		postingV7(in.ExpectedRevisionID) && (in.Decision == "approve" || in.Decision == "reject" || in.Decision == "withdraw") &&
		in.SourcesExamined != nil && *in.SourcesExamined && validPostingReason(in.Reason)
}

func (h *handlers) putPostingProposalDecision(c *gin.Context) {
	if h.scopeAuthority == nil || h.staffActors == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	var in StaffPostingDecisionInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if !validPostingDecisionInput(&in) {
		respond.Error(c, 400, "invalid_request", "Review the exact pending proposal and enter a decision reason and source confirmation.")
		return
	}
	material, err := json.Marshal(struct {
		Hospital, Staff, Proposal uuid.UUID
		Decision                  StaffPostingDecisionInput
	}{s.hospital, s.staff, s.draft, in})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	digest := sha256.Sum256(material)
	fingerprint := hex.EncodeToString(digest[:])
	permission := PermPostingReview
	if in.Decision == "withdraw" {
		permission = PermPostingSubmit
	}
	var additional []authz.Permission
	if in.Decision == "approve" && in.HospitalManagementReviewed != nil && *in.HospitalManagementReviewed {
		additional = []authz.Permission{PermPostingManagementReview}
	}
	var out StaffPostingProposal
	err = h.scopedPostingTransaction(c, p, s, true, permission, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
		if err := postingProposalDecisionContext(tx, s, in.Decision, in.ExpectedRevisionID); err != nil {
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
			out, err = postingProposalResult(tx, p, s, receipt.ID)
			if err != nil {
				return err
			}
			return postingProposalAudit(tx, p, s, "receipt_read", out.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		if row.Version != *in.ExpectedVersion || row.RevisionID != in.ExpectedRevisionID || row.Status != "pending" {
			return errPostingProposalConflict
		}
		if err := authorizePostingProposalDecider(tx, p, s, in.Decision); err != nil {
			return err
		}
		var chain []postingChainSource
		persist := func(current *gorm.DB) error {
			out, err = h.recordPostingDecision(current, p, s, row, in, fingerprint, chain)
			return err
		}
		if in.Decision == "approve" {
			intent, readErr := readPostingSupervisionIntent(tx, p.TenantID, s)
			if readErr != nil {
				return readErr
			}
			if intent == nil {
				return errPostingSupervisionRequired
			}
			if intent.Kind != "hospital_management" {
				if len(additional) != 0 {
					return errPostingSupervisionRequired
				}
				return h.withSupervisedPostingApproval(ctx, tx, p, s, row, source, *intent, func(current *gorm.DB, verified []postingChainSource) error {
					chain = verified
					return persist(current)
				})
			}
			if len(additional) == 0 {
				return errPostingSupervisionRequired
			}
			return h.withRootPostingApproval(ctx, tx, p, s, row, source, persist)
		}
		return persist(tx)
	}, additional...)
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

func (h *handlers) recordPostingDecision(tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, in StaffPostingDecisionInput, fingerprint string, chain []postingChainSource) (StaffPostingProposal, error) {
	revision, err := uuid.NewV7()
	if err != nil {
		return StaffPostingProposal{}, err
	}
	status := map[string]string{"approve": "approved", "reject": "rejected", "withdraw": "withdrawn"}[in.Decision]
	if err := tx.Exec(`INSERT INTO workforce_posting_proposal_revisions(tenant_id,hospital_id,staff_id,proposal_id,id,command_id,expected_version,version,status,draft_id,draft_revision_id,draft_version,generation_id,staff_version,employment_epoch,department_id,department_revision_id,starts_at,ends_at,eligibility_until,reason,actor_subject,sources_examined,input_hash)
 SELECT tenant_id,hospital_id,staff_id,proposal_id,?,?,version,version+1,?,draft_id,draft_revision_id,draft_version,generation_id,staff_version,employment_epoch,department_id,department_revision_id,starts_at,ends_at,eligibility_until,?,?,true,? FROM workforce_posting_proposal_revisions WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND id=?`, revision, s.command, status, in.Reason, p.Subject, fingerprint, p.TenantID, s.hospital, s.staff, s.draft, row.RevisionID).Error; err != nil {
		return StaffPostingProposal{}, err
	}
	if status == "approved" {
		if len(chain) == 0 {
			if err := recordPostingManagementReview(tx, p, s, revision, row.RevisionID); err != nil {
				return StaffPostingProposal{}, err
			}
		} else if err := recordPostingChainReview(tx, p, s, revision, row.RevisionID, chain); err != nil {
			return StaffPostingProposal{}, err
		}
	}
	updated := tx.Exec(`UPDATE workforce_posting_proposals SET revision_id=?,version=version+1,status=? WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND revision_id=? AND version=? AND status='pending'`, revision, status, p.TenantID, s.hospital, s.staff, s.draft, row.RevisionID, row.Version)
	if updated.Error != nil {
		return StaffPostingProposal{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return StaffPostingProposal{}, errPostingProposalConflict
	}
	out, err := postingProposalResult(tx, p, s, revision)
	if err != nil {
		return StaffPostingProposal{}, err
	}
	if err := postingProposalAudit(tx, p, s, status, out.Version); err != nil {
		return StaffPostingProposal{}, err
	}
	payload, err := json.Marshal(workforcecontract.PostingProposalChangedData{HospitalID: s.hospital, StaffID: s.staff, ProposalID: s.draft, RevisionID: revision, Version: out.Version, Status: out.Status})
	if err != nil {
		return StaffPostingProposal{}, err
	}
	if err := h.bus.Publish(tx, workforcecontract.SubjectPostingProposalChanged, events.Event{Type: workforcecontract.SubjectPostingProposalChanged, Version: 1, TenantID: p.TenantID, Data: payload}); err != nil {
		return StaffPostingProposal{}, err
	}
	return out, nil
}
