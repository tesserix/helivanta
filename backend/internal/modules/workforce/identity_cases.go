package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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

const identityCaseColumns = "id,revision_id,hospital_id,staff_id,version,status,kind,policy_reference,check_reference,to_char(observed_on,'YYYY-MM-DD') AS observed_on,valid_until,identity_hash,employment_epoch,proposed_by_subject,actor_subject,created_at,updated_at,input_hash"

var (
	errIdentityCaseConflict = errors.New("identity case command conflicts with current state")
	errIdentityCaseSource   = errors.New("identity case source changed")
	errIdentityCaseInactive = errors.New("identity case employment inactive")
)

type identityCaseRecord struct {
	IdentityCase
	InputHash string `json:"-"`
}

func identityCaseUTC(row IdentityCase) IdentityCase {
	row.ValidUntil = row.ValidUntil.UTC()
	row.CreatedAt = row.CreatedAt.UTC()
	row.UpdatedAt = row.UpdatedAt.UTC()
	return row
}

func (c IdentityCase) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

type identityCaseHistory struct{ IdentityCase }

func (c identityCaseHistory) PageKey() (time.Time, uuid.UUID) { return c.UpdatedAt, c.ID }

func identityCaseStaffScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, hospital, ok := scope(c)
	if !ok {
		return p, hospital, uuid.Nil, false
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening or changing identity cases.")
		return p, hospital, uuid.Nil, false
	}
	if !identityWorkspaceMatches(c, p) {
		return p, hospital, uuid.Nil, false
	}
	staff, err := uuid.Parse(c.Param("id"))
	if err != nil || staff == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, hospital, staff, false
	}
	return p, hospital, staff, true
}

func identityCaseScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, hospital, staff, ok := identityCaseStaffScope(c)
	if !ok {
		return p, hospital, uuid.Nil, uuid.Nil, false
	}
	check, checkErr := uuid.Parse(c.Param("check"))
	if checkErr != nil || check.Version() != 7 {
		respond.NotFound(c, "identity case")
		return p, hospital, staff, check, false
	}
	return p, hospital, staff, check, true
}

func normalizeIdentityCaseDraft(in *IdentityCaseDraftInput) bool {
	in.PolicyReference = strings.TrimSpace(in.PolicyReference)
	in.CheckReference = strings.TrimSpace(in.CheckReference)
	in.ValidUntil = in.ValidUntil.UTC()
	observed, err := time.Parse(time.DateOnly, in.ObservedOn)
	return in.CommandID.Version() == 7 && in.ExpectedVersion != nil && *in.ExpectedVersion >= 0 &&
		*in.ExpectedVersion < 9007199254740991 &&
		(in.Kind == "identity" || in.Kind == "background" || in.Kind == "photograph") &&
		utf8.ValidString(in.PolicyReference) && utf8.ValidString(in.CheckReference) &&
		utf8.RuneCountInString(in.PolicyReference) >= 1 && utf8.RuneCountInString(in.PolicyReference) <= 300 &&
		utf8.RuneCountInString(in.CheckReference) >= 1 && utf8.RuneCountInString(in.CheckReference) <= 500 &&
		!strings.ContainsRune(in.PolicyReference+in.CheckReference, '\x00') &&
		err == nil && observed.Year() >= 1 && !observed.After(time.Now().UTC()) &&
		in.ValidUntil.After(observed) && in.ValidUntil.Year() <= 9999 &&
		in.EmploymentEpoch >= 1 && in.EmploymentEpoch <= 9007199254740991 &&
		len(in.IdentityHash) == 64 && validIdentityHash(in.IdentityHash)
}

func validIdentityHash(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func identityCaseAudit(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, action string, version int64, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'assignment_id',?::text,'assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint,'actor_link_id',?::text,'actor_link_version',?::bigint))`, p.TenantID, hospital, p.Subject, action, id, version, grant.AssignmentID, grant.Version, grant.ScopeEpoch, grant.MembershipEpoch, actor.ID.String(), actor.Version).Error
}

func (h *handlers) putIdentityCaseDraft(c *gin.Context) {
	p, hospital, staff, id, ok := identityCaseScope(c)
	if !ok {
		return
	}
	var in IdentityCaseDraftInput
	if !decodeSized(c, &in, 16<<10) {
		return
	}
	if !normalizeIdentityCaseDraft(&in) {
		respond.Error(c, 400, "invalid_request", "Complete the policy, check, observation, validity and current identity source fields.")
		return
	}
	material, err := json.Marshal(in)
	if err != nil {
		respondIdentityError(c, err)
		return
	}
	digest := sha256.Sum256(material)
	intent := hex.EncodeToString(digest[:])
	var result IdentityCase
	err = h.identityScopeTransaction(c, p, hospital, PermIdentityChecksManage, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_identity_case','manage',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":identity-case:"+id.String()).Error; err != nil {
			return err
		}
		var receipt identityCaseRecord
		err = tx.Table("workforce_identity_case_history").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND revision_id=?", p.TenantID, hospital, in.CommandID).Take(&receipt).Error
		if err == nil {
			if receipt.ID != id || receipt.StaffID != staff || receipt.ActorSubject != p.Subject || receipt.InputHash != intent {
				return errIdentityCaseConflict
			}
			result = receipt.IdentityCase
			return identityCaseAudit(tx, p, hospital, id, "workforce.identity_case.receipt.read", result.Version, actor, grant)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if source.EmploymentStatus != "active" {
			return errIdentityCaseInactive
		}
		if source.IdentityHash != in.IdentityHash || source.EmploymentEpoch != in.EmploymentEpoch {
			return errIdentityCaseSource
		}
		var current identityCaseRecord
		err = tx.Table("workforce_identity_cases").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&current).Error
		values := map[string]any{"revision_id": in.CommandID, "version": *in.ExpectedVersion + 1, "policy_reference": in.PolicyReference, "check_reference": in.CheckReference, "observed_on": in.ObservedOn, "valid_until": in.ValidUntil, "identity_hash": in.IdentityHash, "employment_epoch": in.EmploymentEpoch, "actor_subject": p.Subject, "input_hash": intent}
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound) && *in.ExpectedVersion == 0:
			values["tenant_id"], values["hospital_id"], values["staff_id"], values["id"] = p.TenantID, hospital, staff, id
			values["kind"], values["status"], values["proposed_by_subject"] = in.Kind, "draft", p.Subject
			if err := tx.Table("workforce_identity_cases").Create(values).Error; err != nil {
				return err
			}
		case err == nil && current.Version == *in.ExpectedVersion && current.Status == "draft" && current.Kind == in.Kind:
			changed := tx.Table("workforce_identity_cases").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=?", p.TenantID, hospital, staff, id, *in.ExpectedVersion).Updates(values)
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return errIdentityCaseConflict
			}
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		default:
			return errIdentityCaseConflict
		}
		if err := tx.Table("workforce_identity_cases").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&result).Error; err != nil {
			return err
		}
		if err := identityCaseAudit(tx, p, hospital, id, "workforce.identity_case.draft.saved", result.Version, actor, grant); err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.IdentityCaseChangedData{HospitalID: hospital.String(), StaffID: staff.String(), CaseID: id.String(), Version: result.Version, Status: result.Status})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, workforcecontract.SubjectIdentityCaseChanged, events.Event{Type: workforcecontract.SubjectIdentityCaseChanged, Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		respondIdentityCaseError(c, err)
		return
	}
	respond.OK(c, identityCaseUTC(result))
}

func (h *handlers) getIdentityCase(c *gin.Context) {
	p, hospital, staff, id, ok := identityCaseScope(c)
	if !ok {
		return
	}
	var result IdentityCase
	read := func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		if err := tx.Table("workforce_identity_cases").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&result).Error; err != nil {
			return err
		}
		return identityCaseAudit(tx, p, hospital, id, "workforce.identity_case.read", result.Version, actor, grant)
	}
	err := h.identityCaseReadTransaction(c, p, hospital, staff, read)
	if err != nil {
		respondIdentityCaseError(c, err)
		return
	}
	respond.OK(c, identityCaseUTC(result))
}

func (h *handlers) identityCaseReadTransaction(c *gin.Context, p authn.Principal, hospital, staff uuid.UUID, fn func(*gorm.DB, reviewedLeaveActor, authz.ScopeGrant) error) error {
	read := func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		if _, err := loadIdentitySource(tx, p, hospital, staff); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_identity_case','read',true)`).Error; err != nil {
			return err
		}
		return fn(tx, actor, grant)
	}
	err := h.identityScopeTransaction(c, p, hospital, PermIdentityChecksManage, read)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		err = h.identityScopeTransaction(c, p, hospital, PermIdentityChecksReview, read)
	}
	return err
}

func (h *handlers) listIdentityCases(c *gin.Context, params pagination.Params) ([]IdentityCase, error) {
	p, hospital, staff, ok := identityCaseStaffScope(c)
	if !ok {
		return nil, nil
	}
	rows := []IdentityCase{}
	err := h.identityCaseReadTransaction(c, p, hospital, staff, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_identity_cases").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Find(&rows).Error; err != nil {
			return err
		}
		return identityCaseAudit(tx, p, hospital, staff, "workforce.identity_case.list", 0, actor, grant)
	})
	if err != nil {
		respondIdentityCaseError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i] = identityCaseUTC(rows[i])
	}
	return rows, nil
}

func (h *handlers) identityCaseHistory(c *gin.Context, params pagination.Params) ([]identityCaseHistory, error) {
	p, hospital, staff, id, ok := identityCaseScope(c)
	if !ok {
		return nil, nil
	}
	rows := []identityCaseHistory{}
	err := h.identityCaseReadTransaction(c, p, hospital, staff, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		var current IdentityCase
		if err := tx.Table("workforce_identity_cases").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&current).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "updated_at", Direction: pagination.NewestFirst}).Table("workforce_identity_case_history").Select(identityCaseColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Find(&rows).Error; err != nil {
			return err
		}
		return identityCaseAudit(tx, p, hospital, id, "workforce.identity_case.history.read", current.Version, actor, grant)
	})
	if err != nil {
		respondIdentityCaseError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].IdentityCase = identityCaseUTC(rows[i].IdentityCase)
	}
	return rows, nil
}

func respondIdentityCaseError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errIdentityCaseConflict), errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514" || pg.Code == "23503"):
		respond.Error(c, 409, "identity_case_conflict", "The identity case changed or this command was already used. Refresh before continuing.")
	case errors.Is(err, errIdentityCaseSource):
		respond.Error(c, 409, "identity_source_changed", "Staff identity details changed. Reload the current source before preparing this case.")
	case errors.Is(err, errIdentityCaseInactive):
		respond.Error(c, 409, "staff_inactive", "A new identity review draft requires active employment.")
	default:
		respondIdentityError(c, err)
	}
}
