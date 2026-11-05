package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

const postingCutoffProjection = "id,hospital_id,staff_id,proposal_id,expected_revision_id,expected_cutoff_version,version,mode,requested_cutoff_at,cutoff_at,recorded_at,reason,sources_examined"

var errPostingCutoffConflict = errors.New("posting cutoff intent changed")
var errPostingLifecycleReviewRequired = errors.New("posting cutoff requires lifecycle-aware review")

type postingRetainedCutoff struct {
	ID                 uuid.UUID
	RecordedAt         time.Time
	Version            int64
	ExpectedRevisionID uuid.UUID
	CutoffAt           time.Time
}

func readPostingRetainedCutoff(tx *gorm.DB, p authn.Principal, s postingDraftScope) (*postingRetainedCutoff, error) {
	var allowed bool
	if err := tx.Raw(`SELECT workforce_posting_cutoff_context(?::uuid,?::uuid,?::uuid,false) OR workforce_posting_proposal_context(?::uuid,?::uuid,?::uuid,false) OR workforce_posting_supervisor_choices_context(?::uuid,?::uuid)`, p.TenantID, s.hospital, s.staff, p.TenantID, s.hospital, s.staff, p.TenantID, s.hospital).Scan(&allowed).Error; err != nil {
		return nil, err
	}
	if !allowed {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	var out postingRetainedCutoff
	err := postingCutoffQuery(tx, p, s).Select("id,version,expected_revision_id,cutoff_at,recorded_at").Order("version DESC").Take(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &out, err
}

func postingCutoffScope(c *gin.Context) (authn.Principal, postingDraftScope, bool) {
	p, s, ok := postingRequestScope(c)
	if !ok {
		return p, s, false
	}
	id, err := uuid.Parse(c.Param("proposal"))
	if err != nil || !postingV7(id) {
		respond.NotFound(c, "Staff posting cutoff")
		return p, s, false
	}
	s.draft = id
	return p, s, true
}

func respondPostingCutoffError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, errPostingCutoffConflict) || errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "workforce_posting_cutoff_guard") {
		respond.Error(c, 409, "posting_cutoff_conflict", "The approved posting or cutoff changed. Reload its current history before continuing.")
		return
	}
	respondPostingProposalError(c, err)
}

func validPostingCutoffInput(in *StaffPostingCutoffInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.CutoffAt != nil {
		instant := in.CutoffAt.UTC().Truncate(time.Microsecond)
		in.CutoffAt = &instant
	}
	return postingV7(in.ExpectedRevisionID) && in.ExpectedCutoffVersion != nil && *in.ExpectedCutoffVersion >= 0 && *in.ExpectedCutoffVersion <= 9007199254740989 &&
		validPostingReason(in.Reason) && in.SourcesExamined != nil && *in.SourcesExamined &&
		(in.Mode == "immediate" && in.CutoffAt == nil || in.Mode == "scheduled" && in.CutoffAt != nil && !in.CutoffAt.IsZero() && in.CutoffAt.Year() >= 1 && in.CutoffAt.Year() <= 9999)
}

func postingCutoffContext(tx *gorm.DB, s postingDraftScope, writing bool, revision uuid.UUID) error {
	mode := "read"
	if writing {
		mode = "write"
	}
	return tx.Exec(`SELECT set_config('app.workforce_posting_cutoff',?,true),set_config('app.workforce_posting_proposal',?,true),set_config('app.workforce_posting_expected_revision',?,true)`, mode, s.draft.String(), revision.String()).Error
}

func postingCutoffQuery(tx *gorm.DB, p authn.Principal, s postingDraftScope) *gorm.DB {
	return tx.Table("workforce_posting_cutoffs").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=?", p.TenantID, s.hospital, s.staff, s.draft)
}

func publicPostingCutoff(out StaffPostingCutoff) StaffPostingCutoff {
	out.CutoffAt, out.RecordedAt = out.CutoffAt.UTC(), out.RecordedAt.UTC()
	if out.RequestedCutoffAt != nil {
		instant := out.RequestedCutoffAt.UTC()
		out.RequestedCutoffAt = &instant
	}
	return out
}

func (h *handlers) putPostingCutoff(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, s, ok := postingCutoffScope(c)
	if !ok {
		return
	}
	var in StaffPostingCutoffInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if !validPostingCutoffInput(&in) {
		respond.Error(c, 400, "invalid_request", "Examine the exact approved posting and cutoff version, then provide an ending mode, reason and confirmation.")
		return
	}
	material, err := json.Marshal(struct {
		Hospital, Staff, Proposal uuid.UUID
		Input                     StaffPostingCutoffInput
	}{s.hospital, s.staff, s.draft, in})
	if err != nil {
		respondPostingCutoffError(c, err)
		return
	}
	digest := sha256.Sum256(material)
	fingerprint := hex.EncodeToString(digest[:])
	var out StaffPostingCutoff
	err = h.scopedPostingTransaction(c, p, s, true, PermPostingCutoff, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := postingCutoffContext(tx, s, true, in.ExpectedRevisionID); err != nil {
			return err
		}
		var existing struct {
			StaffPostingCutoff
			ActorSubject, InputHash string
		}
		err := postingCutoffQuery(tx, p, s).Select(postingCutoffProjection+",actor_subject,input_hash").Where("id=?", s.command).Take(&existing).Error
		if err == nil {
			if existing.ActorSubject != p.Subject || existing.InputHash != fingerprint {
				return errPostingCutoffConflict
			}
			out = existing.StaffPostingCutoff
			return postingProposalAudit(tx, p, s, "cutoff_receipt_read", out.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		if row.Status != "approved" || row.RevisionID != in.ExpectedRevisionID {
			return errPostingCutoffConflict
		}
		err = tx.Raw(`INSERT INTO workforce_posting_cutoffs(tenant_id,hospital_id,staff_id,proposal_id,id,expected_revision_id,expected_cutoff_version,version,mode,requested_cutoff_at,reason,sources_examined,input_hash,actor_subject,actor_staff_id,actor_link_id,actor_generation_id,assignment_id,assignment_version,scope_epoch,membership_epoch)
 VALUES(?,?,?,?,?,?,?,?+1,?,?,?,true,?,?,current_setting('app.workforce_posting_actor')::uuid,current_setting('app.workforce_posting_link')::uuid,current_setting('app.workforce_posting_generation')::uuid,current_setting('app.workforce_posting_assignment')::uuid,current_setting('app.workforce_posting_assignment_version')::bigint,current_setting('app.workforce_posting_scope_epoch')::bigint,current_setting('app.workforce_posting_membership_epoch')::bigint)
 RETURNING `+postingCutoffProjection, p.TenantID, s.hospital, s.staff, s.draft, s.command, in.ExpectedRevisionID, *in.ExpectedCutoffVersion, *in.ExpectedCutoffVersion, in.Mode, in.CutoffAt, in.Reason, fingerprint, p.Subject).Scan(&out).Error
		if err != nil {
			return err
		}
		if err := postingProposalAudit(tx, p, s, "cutoff_recorded", out.Version); err != nil {
			return err
		}
		payload, err := json.Marshal(workforcecontract.PostingCutoffChangedData{HospitalID: s.hospital, StaffID: s.staff, ProposalID: s.draft, CutoffID: out.ID, Version: out.Version})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, workforcecontract.SubjectPostingCutoffChanged, events.Event{Type: workforcecontract.SubjectPostingCutoffChanged, Version: 1, TenantID: p.TenantID, Data: payload})
	})
	if err != nil {
		respondPostingCutoffError(c, err)
		return
	}
	respond.OK(c, publicPostingCutoff(out))
}

func (h *handlers) getPostingCutoffReceipt(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, s, ok := postingCutoffScope(c)
	if !ok {
		return
	}
	var out StaffPostingCutoff
	err := h.scopedPostingTransaction(c, p, s, false, PermPostingCutoff, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := postingCutoffContext(tx, s, false, uuid.Nil); err != nil {
			return err
		}
		if err := postingCutoffQuery(tx, p, s).Select(postingCutoffProjection).Where("id=? AND actor_subject=?", s.command, p.Subject).Take(&out).Error; err != nil {
			return err
		}
		return postingProposalAudit(tx, p, s, "cutoff_receipt_read", out.Version)
	})
	if err != nil {
		respondPostingCutoffError(c, err)
		return
	}
	respond.OK(c, publicPostingCutoff(out))
}
