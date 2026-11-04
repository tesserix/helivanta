package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

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
	"gorm.io/gorm/clause"
)

var errPostingDraftConflict = errors.New("posting draft intent or source changed")

const postingDraftProjection = "d.id,r.id AS revision_id,d.hospital_id,d.staff_id,r.version,r.staff_version,r.employment_epoch,r.department_id,r.department_revision_id,r.starts_at,r.ends_at,d.created_at,r.created_at AS recorded_at"

type postingDraftScope struct{ hospital, staff, draft, command uuid.UUID }

func (r StaffPostingDraft) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type postingDraftHistory struct{ StaffPostingDraft }

func (r postingDraftHistory) PageKey() (time.Time, uuid.UUID) { return r.RecordedAt, r.RevisionID }

func validPostingReason(reason string) bool {
	return validBenefitText(reason, 1, 500) && strings.IndexFunc(reason, unicode.IsControl) < 0
}

func postingV7(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

func (h *handlers) postingScope(c *gin.Context) (authn.Principal, postingDraftScope, bool) {
	if h.departmentReferences == nil {
		p, hospital, ok := profileScope(c)
		if ok {
			respondPostingDraftError(c, authz.ErrScopedAuthorizationUnavailable)
		}
		return p, postingDraftScope{hospital: hospital}, false
	}
	return postingRequestScope(c)
}

func postingRequestScope(c *gin.Context) (authn.Principal, postingDraftScope, bool) {
	p, hospital, ok := profileScope(c)
	s := postingDraftScope{hospital: hospital}
	if !ok {
		return p, s, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your posting workspace changed. Reload before continuing.")
		return p, s, false
	}
	var err error
	s.staff, err = uuid.Parse(c.Param("id"))
	if err != nil || s.staff == uuid.Nil {
		respond.NotFound(c, "Staff posting draft")
		return p, s, false
	}
	for name, target := range map[string]*uuid.UUID{"posting": &s.draft, "command": &s.command} {
		if c.Param(name) == "" {
			continue
		}
		*target, err = uuid.Parse(c.Param(name))
		if err != nil || !postingV7(*target) {
			respond.NotFound(c, "Staff posting draft")
			return p, s, false
		}
	}
	return p, s, true
}

func respondPostingDraftError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credential revoked")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Staff posting action is not permitted.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded):
		respond.Error(c, 503, "posting_source_unavailable", "The canonical posting source is unavailable. Retry when the source is available.")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrDepartmentReferenceHidden):
		respond.NotFound(c, "Staff posting draft")
	case errors.Is(err, errPostingDraftConflict), errors.Is(err, authz.ErrDepartmentReferenceChanged), errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "workforce_posting_draft_guard"):
		respond.Error(c, 409, "posting_draft_conflict", "The posting draft or its source changed. Reload and review the current version.")
	default:
		respond.InternalErr(c, errors.New("posting draft persistence failed"), "The Staff posting draft could not be saved or loaded.")
	}
}

func postingOwnerContext(tx *gorm.DB, p authn.Principal, s postingDraftScope, writing, scoped bool) (EmploymentPeriodSource, error) {
	mode := "read"
	if writing {
		mode = "write"
	}
	source := EmploymentPeriodSource{HospitalID: s.hospital, StaffID: s.staff}
	if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting',?,true)`, p.Subject, s.hospital.String(), s.staff.String(), mode).Error; err != nil {
		return source, err
	}
	if writing {
		if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, s.hospital).Error; err != nil {
			return source, err
		}
	}
	if !scoped {
		var grant struct{ Level string }
		if err := tx.Table("workforce_hospital_access").Select("level").Where("tenant_id=? AND hospital_id=? AND subject=?", p.TenantID, s.hospital, p.Subject).Clauses(clause.Locking{Strength: "SHARE"}).Take(&grant).Error; err != nil {
			return source, err
		}
		if writing && grant.Level != "manager" || grant.Level != "manager" && grant.Level != "reader" {
			return source, gorm.ErrRecordNotFound
		}
	}
	err := tx.Table("workforce_staff").Select("version AS staff_version,employment_epoch").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, s.hospital, s.staff).Clauses(clause.Locking{Strength: "SHARE"}).Take(&source).Error
	return source, err
}

func postingDraftQuery(tx *gorm.DB, p authn.Principal, s postingDraftScope, history bool) *gorm.DB {
	q := tx.Table("workforce_posting_drafts d").Joins("JOIN workforce_posting_draft_revisions r ON r.tenant_id=d.tenant_id AND r.hospital_id=d.hospital_id AND r.staff_id=d.staff_id AND r.posting_id=d.id")
	projection := postingDraftProjection
	if history {
		projection = strings.Replace(projection, "d.id,", "r.id AS id,", 1)
	} else {
		q = q.Where("r.id=d.revision_id AND r.version=d.version")
	}
	q = q.Select(projection).Where("d.tenant_id=? AND d.hospital_id=? AND d.staff_id=?", p.TenantID, s.hospital, s.staff)
	if s.draft != uuid.Nil {
		q = q.Where("d.id=?", s.draft)
	}
	return q
}

func postingDraftResult(tx *gorm.DB, p authn.Principal, s postingDraftScope, revision uuid.UUID) (StaffPostingDraft, error) {
	var out StaffPostingDraft
	history := revision != uuid.Nil
	q := postingDraftQuery(tx, p, s, history)
	if history {
		q = q.Where("r.id=?", revision)
	}
	err := q.Take(&out).Error
	if history {
		out.ID = s.draft
	}
	return publicPostingDraft(out), err
}

func publicPostingDraft(row StaffPostingDraft) StaffPostingDraft {
	row.Status = "draft"
	row.StartsAt, row.CreatedAt, row.RecordedAt = row.StartsAt.UTC(), row.CreatedAt.UTC(), row.RecordedAt.UTC()
	if row.EndsAt != nil {
		end := row.EndsAt.UTC()
		row.EndsAt = &end
	}
	return row
}

func postingDraftAudit(tx *gorm.DB, p authn.Principal, s postingDraftScope, action string, version int64) error {
	resource := s.draft
	if resource == uuid.Nil {
		resource = s.staff
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint) || CASE WHEN current_setting('app.workforce_posting_scoped',true) IN ('read','write') THEN jsonb_build_object('assignment_id',current_setting('app.workforce_posting_assignment',true),'assignment_version',current_setting('app.workforce_posting_assignment_version',true)::bigint,'scope_epoch',current_setting('app.workforce_posting_scope_epoch',true)::bigint,'membership_epoch',current_setting('app.workforce_posting_membership_epoch',true)::bigint,'actor_staff_id',current_setting('app.workforce_posting_actor',true),'actor_link_id',current_setting('app.workforce_posting_link',true),'generation_id',current_setting('app.workforce_posting_generation',true)) ELSE '{}'::jsonb END)`, p.TenantID, s.hospital, p.Subject, "workforce.posting_draft."+action, resource, version).Error
}

func validPostingDraftInput(in *StaffPostingDraftInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	in.StartsAt = in.StartsAt.UTC().Truncate(time.Microsecond)
	if in.EndsAt != nil {
		end := in.EndsAt.UTC().Truncate(time.Microsecond)
		in.EndsAt = &end
	}
	return in.ExpectedVersion != nil && *in.ExpectedVersion >= 0 && *in.ExpectedVersion < 9007199254740990 &&
		in.StaffVersion >= 1 && in.StaffVersion <= 9007199254740990 && in.EmploymentEpoch >= 1 && in.EmploymentEpoch <= 9007199254740990 &&
		postingV7(in.DepartmentID) && postingV7(in.DepartmentRevisionID) && validPostingReason(in.Reason) &&
		!in.StartsAt.IsZero() && in.StartsAt.Year() >= 1 && in.StartsAt.Year() <= 9999 && (in.EndsAt == nil || in.EndsAt.Year() <= 9999 && in.EndsAt.After(in.StartsAt))
}

func (h *handlers) putPostingDraft(c *gin.Context) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return
	}
	var in StaffPostingDraftInput
	if !decode(c, &in) {
		return
	}
	if !validPostingDraftInput(&in) {
		respond.Error(c, 400, "invalid_request", "Choose a current department, valid dates and a reason for the posting draft.")
		return
	}
	raw, err := json.Marshal(in)
	if err != nil {
		respondPostingDraftError(c, err)
		return
	}
	digest := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(digest[:])
	var out StaffPostingDraft
	err = h.postingTransaction(c, p, s, true, func(ctx context.Context, tx *gorm.DB, source EmploymentPeriodSource) error {
		var receipt struct {
			ID, PostingID uuid.UUID
			ActorSubject  string
			InputHash     string
		}
		err := tx.Table("workforce_posting_draft_revisions").Select("id,posting_id,actor_subject,input_hash").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND command_id=?", p.TenantID, s.hospital, s.staff, s.command).Take(&receipt).Error
		if err == nil {
			if receipt.PostingID != s.draft || receipt.ActorSubject != p.Subject || receipt.InputHash != fingerprint {
				return errPostingDraftConflict
			}
			out, err = postingDraftResult(tx, p, s, receipt.ID)
			if err != nil {
				return err
			}
			return postingDraftAudit(tx, p, s, "retry_read", out.Version)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if source.StaffVersion != in.StaffVersion || source.EmploymentEpoch != in.EmploymentEpoch {
			return errPostingDraftConflict
		}
		var head struct{ Version int64 }
		err = tx.Table("workforce_posting_drafts").Select("version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, s.hospital, s.staff, s.draft).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&head).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if head.Version != *in.ExpectedVersion {
			return errPostingDraftConflict
		}
		ref := authz.DepartmentReference{TenantID: p.TenantID, HospitalID: s.hospital, DepartmentID: in.DepartmentID, DepartmentRevision: in.DepartmentRevisionID}
		return h.departmentReferences.WithCurrentDepartment(ctx, ref, func(ctx context.Context) error {
			return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
				revision, err := uuid.NewV7()
				if err != nil {
					return err
				}
				if err := tx.Exec(`SELECT set_config('app.workforce_posting_department',?,true),set_config('app.workforce_posting_department_revision',?,true)`, in.DepartmentID.String(), in.DepartmentRevisionID.String()).Error; err != nil {
					return err
				}
				if head.Version == 0 {
					err = tx.Exec(`INSERT INTO workforce_posting_drafts(tenant_id,hospital_id,staff_id,id,revision_id,version) VALUES(?,?,?,?,?,1)`, p.TenantID, s.hospital, s.staff, s.draft, revision).Error
				} else {
					err = tx.Exec(`UPDATE workforce_posting_drafts SET revision_id=?,version=version+1 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=?`, revision, p.TenantID, s.hospital, s.staff, s.draft, head.Version).Error
				}
				if err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO workforce_posting_draft_revisions(tenant_id,hospital_id,staff_id,posting_id,id,command_id,expected_version,version,staff_version,employment_epoch,department_id,department_revision_id,starts_at,ends_at,reason,actor_subject,input_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, s.hospital, s.staff, s.draft, revision, s.command, *in.ExpectedVersion, head.Version+1, in.StaffVersion, in.EmploymentEpoch, in.DepartmentID, in.DepartmentRevisionID, in.StartsAt, in.EndsAt, in.Reason, p.Subject, fingerprint).Error; err != nil {
					return err
				}
				out, err = postingDraftResult(tx, p, s, revision)
				if err != nil {
					return err
				}
				if err := postingDraftAudit(tx, p, s, "saved", out.Version); err != nil {
					return err
				}
				data, err := json.Marshal(workforcecontract.PostingDraftChangedData{HospitalID: s.hospital, StaffID: s.staff, DraftID: s.draft, RevisionID: revision, Version: out.Version})
				if err != nil {
					return err
				}
				return h.bus.Publish(tx, workforcecontract.SubjectPostingDraftChanged, events.Event{Type: workforcecontract.SubjectPostingDraftChanged, Version: 1, TenantID: p.TenantID, Data: data})
			})
		})
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) readPostingDraft(c *gin.Context, receipt bool) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return
	}
	var out StaffPostingDraft
	err := h.postingTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		var revision uuid.UUID
		if receipt {
			var row struct{ ID uuid.UUID }
			if err := tx.Table("workforce_posting_draft_revisions").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND posting_id=? AND command_id=?", p.TenantID, s.hospital, s.staff, s.draft, s.command).Take(&row).Error; err != nil {
				return err
			}
			revision = row.ID
		}
		var err error
		out, err = postingDraftResult(tx, p, s, revision)
		if err != nil {
			return err
		}
		return postingDraftAudit(tx, p, s, "read", out.Version)
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) getPostingDraft(c *gin.Context)        { h.readPostingDraft(c, false) }
func (h *handlers) getPostingDraftReceipt(c *gin.Context) { h.readPostingDraft(c, true) }

func (h *handlers) getPostingDraftSource(c *gin.Context) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return
	}
	var source EmploymentPeriodSource
	err := h.postingTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, current EmploymentPeriodSource) error {
		source = current
		return postingDraftAudit(tx, p, s, "source_read", source.StaffVersion)
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return
	}
	respond.OK(c, source)
}

func (h *handlers) listPostingDrafts(c *gin.Context, params pagination.Params) ([]StaffPostingDraft, error) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffPostingDraft{}
	err := h.postingTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		q := tx.Table("(?) AS posting_rows", postingDraftQuery(tx, p, s, false))
		if err := tenantdb.ApplyKeyset(q, params).Find(&rows).Error; err != nil {
			return err
		}
		return postingDraftAudit(tx, p, s, "listed", 0)
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i] = publicPostingDraft(rows[i])
	}
	return rows, nil
}

func (h *handlers) postingDraftHistory(c *gin.Context, params pagination.Params) ([]postingDraftHistory, error) {
	p, s, ok := h.postingScope(c)
	if !ok {
		return nil, nil
	}
	rows := []StaffPostingDraft{}
	err := h.postingTransaction(c, p, s, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		var current StaffPostingDraft
		current, err := postingDraftResult(tx, p, s, uuid.Nil)
		if err != nil {
			return err
		}
		q := tx.Table("(?) AS posting_rows", postingDraftQuery(tx, p, s, true))
		if err := tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "recorded_at", Direction: pagination.NewestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		return postingDraftAudit(tx, p, s, "history_read", current.Version)
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return nil, nil
	}
	out := make([]postingDraftHistory, len(rows))
	for i, row := range rows {
		row.ID = s.draft
		out[i] = postingDraftHistory{publicPostingDraft(row)}
	}
	return out, nil
}
