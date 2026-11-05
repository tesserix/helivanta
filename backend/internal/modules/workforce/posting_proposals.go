package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const PermPostingProposalRead authz.Permission = "workforce.posting.proposal.read"
const PermPostingSubmit authz.Permission = "workforce.posting.submit"
const PermPostingReview authz.Permission = "workforce.posting.review"
const PermPostingManagementReview authz.Permission = "workforce.posting.management.review"
const PermPostingCutoff authz.Permission = "workforce.posting.cutoff"

var errPostingProposalConflict = errors.New("posting proposal intent or source changed")

const postingProposalProjection = "p.id,r.id AS revision_id,r.command_id,p.hospital_id,p.staff_id,r.version,r.status,r.draft_id,r.draft_revision_id,r.draft_version,r.generation_id,r.staff_version,r.employment_epoch,r.department_id,r.department_revision_id,r.starts_at,r.ends_at,r.eligibility_until,p.created_at,r.created_at AS recorded_at"

func (r StaffPostingProposal) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type postingProposalHistory struct{ StaffPostingProposal }

func (r postingProposalHistory) PageKey() (time.Time, uuid.UUID) { return r.RecordedAt, r.RevisionID }

func (h *handlers) postingProposalScope(c *gin.Context) (authn.Principal, postingDraftScope, bool) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return p, s, false
	}
	if c.Param("proposal") != "" {
		var err error
		s.draft, err = uuid.Parse(c.Param("proposal"))
		if err != nil || !postingV7(s.draft) {
			respond.NotFound(c, "Staff posting proposal")
			return p, s, false
		}
	}
	return p, s, true
}

func respondPostingProposalError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errPostingLifecycleReviewRequired):
		respond.Error(c, 409, "posting_lifecycle_review_required", "A retained posting cutoff requires a current lifecycle review. No approval was recorded.")
	case errors.Is(err, errPostingSupervisionRequired):
		respond.Error(c, 409, "posting_supervision_required", "The required posting supervision policy is not available. No approval was recorded.")
	case errors.Is(err, errPostingProposalConflict), errors.Is(err, authz.ErrDepartmentReferenceChanged), errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "workforce_posting_proposal_guard"):
		respond.Error(c, 409, "posting_proposal_conflict", "The proposal or its selected sources changed. Reload and review the current sources.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrDepartmentReferenceHidden):
		respond.NotFound(c, "Staff posting proposal")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credential revoked")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Staff posting proposal action is not permitted.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded):
		respond.Error(c, 503, "posting_source_unavailable", "The current posting source is unavailable. Retry when the source is available.")
	default:
		failure := fmt.Errorf("posting proposal persistence failed (%T)", err)
		if errors.As(err, &pg) {
			failure = fmt.Errorf("posting proposal persistence failed (SQLSTATE=%s, constraint=%s)", pg.Code, pg.ConstraintName)
		}
		respond.InternalErr(c, failure, "The Staff posting proposal could not be saved or loaded.")
	}
}

func (h *handlers) postingProposalTransaction(c *gin.Context, p authn.Principal, s postingDraftScope, writing bool, fn func(context.Context, *gorm.DB, EmploymentPeriodSource) error) error {
	permission, mode := PermPostingProposalRead, "read"
	if writing {
		permission, mode = PermPostingSubmit, "submit"
	}
	return h.scopedPostingTransaction(c, p, s, writing, permission, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_posting_review_mode',?,true)`, mode).Error; err != nil {
			return err
		}
		return fn(ctx, tx, source)
	})
}

func postingProposalQuery(tx *gorm.DB, p authn.Principal, s postingDraftScope, history bool) *gorm.DB {
	q := tx.Table("workforce_posting_proposals p").Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id)=(p.tenant_id,p.hospital_id,p.staff_id,p.id)").Where("p.tenant_id=? AND p.hospital_id=? AND p.staff_id=?", p.TenantID, s.hospital, s.staff)
	projection := postingProposalProjection
	if history {
		projection = strings.Replace(projection, "p.id,", "r.id AS id,", 1)
	} else {
		q = q.Where("r.id=p.revision_id AND r.version=p.version")
	}
	if s.draft != uuid.Nil {
		q = q.Where("p.id=?", s.draft)
	}
	return q.Select(projection)
}

func publicPostingProposal(row StaffPostingProposal) StaffPostingProposal {
	row.GrantsAccess = false
	row.StartsAt, row.EligibilityUntil, row.CreatedAt, row.RecordedAt = row.StartsAt.UTC(), row.EligibilityUntil.UTC(), row.CreatedAt.UTC(), row.RecordedAt.UTC()
	if row.EndsAt != nil {
		end := row.EndsAt.UTC()
		row.EndsAt = &end
	}
	return row
}

func postingProposalResult(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID) (StaffPostingProposal, error) {
	var out StaffPostingProposal
	history := revision != uuid.Nil
	q := postingProposalQuery(tx, p, s, history)
	if history {
		q = q.Where("r.id=?", revision)
	}
	err := q.Take(&out).Error
	if history {
		out.ID = s.draft
	}
	return publicPostingProposal(out), err
}

func postingProposalAudit(tx *gorm.DB, p authn.Principal, s postingDraftScope, action string, version int64) error {
	resource := s.draft
	if resource == uuid.Nil {
		resource = s.staff
	}
	return postingProposalAuditRecord(tx, p, s.hospital, resource, action, version, nil)
}

func postingProposalAuditRecord(tx *gorm.DB, p authn.Principal, hospital, resource uuid.UUID, action string, version int64, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,?::jsonb || jsonb_build_object('version',?::bigint,'assignment_id',current_setting('app.workforce_posting_assignment',true),'assignment_version',current_setting('app.workforce_posting_assignment_version',true)::bigint,'scope_epoch',current_setting('app.workforce_posting_scope_epoch',true)::bigint,'membership_epoch',current_setting('app.workforce_posting_membership_epoch',true)::bigint,'actor_staff_id',current_setting('app.workforce_posting_actor',true),'actor_link_id',current_setting('app.workforce_posting_link',true),'actor_generation_id',current_setting('app.workforce_posting_generation',true)))`, p.TenantID, hospital, p.Subject, "workforce.posting_proposal."+action, resource, string(payload), version).Error
}

func validPostingProposalInput(in *StaffPostingProposalInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	validVersion := func(v *int64) bool { return v != nil && *v >= 1 && *v <= 9007199254740990 }
	return in.ExpectedVersion != nil && *in.ExpectedVersion == 0 && postingV7(in.DraftID) && postingV7(in.DraftRevisionID) && postingV7(in.GenerationID) && validVersion(in.DraftVersion) && validVersion(in.StaffVersion) && validVersion(in.EmploymentEpoch) && in.SourcesExamined != nil && *in.SourcesExamined && validPostingReason(in.Reason) && validPostingSupervisionIntent(in.Supervision)
}

func (h *handlers) putPostingProposal(c *gin.Context) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	var in StaffPostingProposalInput
	if !decode(c, &in) {
		return
	}
	if !validPostingProposalInput(&in) {
		respond.Error(c, 400, "invalid_request", "Review the exact current draft and employment generation and provide a submission reason.")
		return
	}
	material, err := json.Marshal(struct {
		Hospital, Staff, Proposal uuid.UUID
		Input                     StaffPostingProposalInput
	}{s.hospital, s.staff, s.draft, in})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	digest := sha256.Sum256(material)
	fingerprint := hex.EncodeToString(digest[:])
	var out StaffPostingProposal
	err = h.postingProposalTransaction(c, p, s, true, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
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
		if source.StaffVersion != *in.StaffVersion || source.EmploymentEpoch != *in.EmploymentEpoch {
			return errPostingProposalConflict
		}
		draftScope := s
		draftScope.draft = in.DraftID
		draft, err := postingDraftResult(tx, p, draftScope, uuid.Nil)
		if err != nil {
			return err
		}
		if draft.RevisionID != in.DraftRevisionID || draft.Version != *in.DraftVersion || draft.StaffVersion != source.StaffVersion || draft.EmploymentEpoch != source.EmploymentEpoch {
			return errPostingProposalConflict
		}
		generation, state, until, err := readPostingTargetGeneration(tx, p, s, in.GenerationID)
		if err != nil {
			return err
		}
		if state != "effective" && state != "not_started" || generation.EmploymentEpoch != source.EmploymentEpoch || draft.StartsAt.Before(generation.StartsAt) || !draft.StartsAt.Before(until) || generation.EndsAt != nil && (draft.EndsAt == nil || draft.EndsAt.After(*generation.EndsAt)) {
			return errPostingProposalConflict
		}
		if err := h.db.ConstrainGuardedCommand(ctx, p.TenantID, until); err != nil {
			return errors.Join(errPostingProposalConflict, err)
		}
		ref := authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: draft.DepartmentID, DepartmentRevision: draft.DepartmentRevisionID}
		return h.departmentReferences.WithCurrentDepartment(ctx, ref, func(currentCtx context.Context) error {
			return h.db.WithTenantCommand(currentCtx, p.TenantID, func(tx *gorm.DB) error {
				revision, err := uuid.NewV7()
				if err != nil {
					return err
				}
				if err := generationContext(tx, s.staff, "read"); err != nil {
					return err
				}
				if err := tx.Exec(`SELECT set_config('app.workforce_posting_target_generation',?,true),set_config('app.workforce_posting_target_until',?,true),set_config('app.workforce_posting_department',?,true),set_config('app.workforce_posting_department_revision',?,true)`, in.GenerationID.String(), until.UTC().Format(time.RFC3339Nano), draft.DepartmentID.String(), draft.DepartmentRevisionID.String()).Error; err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO workforce_posting_proposals(tenant_id,hospital_id,staff_id,id,revision_id,version,draft_id,draft_revision_id,status) VALUES(?,?,?,?,?,1,?,?,'pending')`, p.TenantID, s.hospital, s.staff, s.draft, revision, in.DraftID, in.DraftRevisionID).Error; err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO workforce_posting_proposal_revisions(tenant_id,hospital_id,staff_id,proposal_id,id,command_id,expected_version,version,status,draft_id,draft_revision_id,draft_version,generation_id,staff_version,employment_epoch,department_id,department_revision_id,starts_at,ends_at,eligibility_until,reason,actor_subject,sources_examined,input_hash,supervision_requested) VALUES(?,?,?,?,?,?,0,1,'pending',?,?,?,?,?,?,?,?,?,?,?,?,?,true,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, s.command, in.DraftID, in.DraftRevisionID, *in.DraftVersion, in.GenerationID, source.StaffVersion, source.EmploymentEpoch, draft.DepartmentID, draft.DepartmentRevisionID, draft.StartsAt, draft.EndsAt, until, in.Reason, p.Subject, fingerprint, in.Supervision != nil).Error; err != nil {
					return err
				}
				if in.Supervision != nil {
					intent := in.Supervision
					if err := tx.Exec(`INSERT INTO workforce_posting_supervision_intents(tenant_id,hospital_id,staff_id,proposal_id,initial_revision_id,kind,supervisor_staff_id,supervisor_proposal_id,supervisor_revision_id) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, intent.Kind, intent.SupervisorStaffID, intent.SupervisorProposalID, intent.SupervisorRevisionID).Error; err != nil {
						return err
					}
				}
				out, err = postingProposalResult(tx, p, s, revision)
				if err != nil {
					return err
				}
				if err := postingProposalAudit(tx, p, s, "submitted", out.Version); err != nil {
					return err
				}
				payload, err := json.Marshal(workforcecontract.PostingProposalChangedData{HospitalID: s.hospital, StaffID: s.staff, ProposalID: s.draft, RevisionID: revision, Version: out.Version, Status: out.Status})
				if err != nil {
					return err
				}
				return h.bus.Publish(tx, workforcecontract.SubjectPostingProposalChanged, events.Event{Type: workforcecontract.SubjectPostingProposalChanged, Version: 1, TenantID: p.TenantID, Data: payload})
			})
		})
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

func (h *handlers) getPostingProposal(c *gin.Context) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	var out StaffPostingProposal
	err := h.postingProposalTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		var err error
		out, err = postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		return postingProposalAudit(tx, p, s, "read", out.Version)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

func (h *handlers) getPostingProposalReceipt(c *gin.Context) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	var out StaffPostingProposal
	err := h.postingProposalTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		var receipt struct{ ID uuid.UUID }
		if err := tx.Table("workforce_posting_proposal_revisions").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND proposal_id=? AND command_id=?", p.TenantID, s.hospital, s.staff, s.draft, s.command).Take(&receipt).Error; err != nil {
			return err
		}
		var err error
		out, err = postingProposalResult(tx, p, s, receipt.ID)
		if err != nil {
			return err
		}
		return postingProposalAudit(tx, p, s, "receipt_read", out.Version)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

func (h *handlers) listPostingProposals(c *gin.Context, params pagination.Params) ([]StaffPostingProposal, error) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffPostingProposal{}
	err := h.postingProposalTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS proposal_rows", postingProposalQuery(tx, p, s, false)), params).Find(&rows).Error; err != nil {
			return err
		}
		return postingProposalAudit(tx, p, s, "listed", 0)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i] = publicPostingProposal(rows[i])
	}
	return rows, nil
}

func (h *handlers) postingProposalHistory(c *gin.Context, params pagination.Params) ([]postingProposalHistory, error) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffPostingProposal{}
	err := h.postingProposalTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		current, err := postingProposalResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		q := tx.Table("(?) AS proposal_rows", postingProposalQuery(tx, p, s, true))
		if err := tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.NewestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		return postingProposalAudit(tx, p, s, "history_read", current.Version)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	out := make([]postingProposalHistory, len(rows))
	for i, row := range rows {
		row.ID = s.draft
		out[i] = postingProposalHistory{publicPostingProposal(row)}
	}
	return out, nil
}

func readPostingTargetGeneration(tx *gorm.DB, p authn.Principal, s postingDraftScope, id uuid.UUID) (employmentGenerationRecord, string, time.Time, error) {
	var actor struct{ StaffID, LinkID uuid.UUID }
	if err := tx.Raw(`SELECT current_setting('app.workforce_posting_actor')::uuid AS staff_id,current_setting('app.workforce_posting_link')::uuid AS link_id`).Scan(&actor).Error; err != nil {
		return employmentGenerationRecord{}, "", time.Time{}, err
	}
	return readProtectedStaffGenerationState(tx, p, s.hospital, s.staff, actor.StaffID, actor.LinkID, id)
}

func (h *handlers) getPostingReviewSource(c *gin.Context) {
	p, s, ok := h.postingProposalScope(c)
	if !ok {
		return
	}
	out := StaffPostingReviewSource{HospitalID: s.hospital, StaffID: s.staff, GenerationState: "no_current_generation"}
	err := h.postingProposalTransaction(c, p, s, false, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
		out.StaffVersion, out.EmploymentEpoch = source.StaffVersion, source.EmploymentEpoch
		if err := generationContext(tx, s.staff, "read"); err != nil {
			return err
		}
		var ids []uuid.UUID
		if err := tx.Table("workforce_employment_generations").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND employment_epoch=?", p.TenantID, s.hospital, s.staff, source.EmploymentEpoch).Limit(2).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 1 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if len(ids) == 1 {
			generation, state, until, err := readPostingTargetGeneration(tx, p, s, ids[0])
			if err != nil {
				return err
			}
			id, start := generation.ID, generation.StartsAt.UTC()
			out.GenerationID, out.StartsAt = &id, &start
			if generation.EndsAt != nil {
				end := generation.EndsAt.UTC()
				out.EndsAt = &end
			}
			if !until.IsZero() {
				value := until.UTC()
				out.EligibilityUntil = &value
			}
			switch state {
			case "effective":
				out.GenerationState = "current"
			case "not_started", "expired":
				out.GenerationState = state
			default:
				out.GenerationState = "source_changed"
			}
			if state == "effective" || state == "not_started" {
				if err := h.db.ConstrainGuardedCommand(ctx, p.TenantID, until); err != nil {
					return errors.Join(errPostingProposalConflict, err)
				}
			}
		}
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&out.CheckedAt).Error; err != nil {
			return err
		}
		out.CheckedAt = out.CheckedAt.UTC()
		return postingProposalAudit(tx, p, s, "source_read", 0)
	})
	if err != nil {
		respondPostingProposalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}
