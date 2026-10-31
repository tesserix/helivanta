package workforce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const employmentEvidenceColumns = "id,revision_id,hospital_id,staff_id,period_id,period_revision_id,period_version,version,status,staff_version,employment_epoch,document_id,document_record_version,filename,content_type,size,sha256,source_generation::text AS source_generation,released_generation::text AS released_generation,scan_attempt,scanned_at,proposed_by_subject,actor_subject,reason,created_at,updated_at,input_hash"

var errEmploymentEvidenceConflict = errors.New("employment evidence conflicts with current source or state")

type employmentEvidenceRecord struct {
	EmploymentEvidenceBinding
	InputHash string `json:"-"`
}
type employmentEvidenceRevision struct{ EmploymentEvidenceBinding }

func (r EmploymentEvidenceBinding) PageKey() (time.Time, uuid.UUID)  { return r.CreatedAt, r.ID }
func (r employmentEvidenceRevision) PageKey() (time.Time, uuid.UUID) { return r.UpdatedAt, r.ID }
func employmentEvidenceUTC(r EmploymentEvidenceBinding) EmploymentEvidenceBinding {
	r.ScannedAt = r.ScannedAt.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r
}

type employmentEvidenceScope struct {
	p                                authn.Principal
	hospital, staff, period, binding uuid.UUID
	version                          int64
}

func employmentEvidenceParams(c *gin.Context) (employmentEvidenceScope, bool) {
	p, hospital, staff, period, ok := employmentPeriodScope(c)
	s := employmentEvidenceScope{p: p, hospital: hospital, staff: staff, period: period}
	if !ok {
		return s, false
	}
	if !authz.Refresh(c, PermDocumentManage) {
		return s, false
	}
	permissions, ok := authz.PermissionsFrom(c)
	if !ok || !permissions.Has(PermEnrollmentManage) {
		respond.Forbidden(c, "missing permission "+string(PermEnrollmentManage))
		return s, false
	}
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version < 1 || version > 9007199254740990 {
		respond.NotFound(c, "employment revision")
		return s, false
	}
	s.version = version
	if c.Param("binding") != "" {
		s.binding, err = uuid.Parse(c.Param("binding"))
		if err != nil || s.binding.Version() != 7 {
			respond.NotFound(c, "employment evidence")
			return s, false
		}
	}
	return s, true
}
func evidenceReason(reason *string) bool {
	*reason = strings.TrimSpace(*reason)
	return utf8.ValidString(*reason) && utf8.RuneCountInString(*reason) >= 1 && utf8.RuneCountInString(*reason) <= 500 && !strings.ContainsRune(*reason, '\x00')
}
func employmentEvidenceIntent(action string, input any) (string, error) {
	data, err := json.Marshal(struct {
		Action string
		Input  any
	}{action, input})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
func evidenceWhere(tx *gorm.DB, s employmentEvidenceScope, table string) *gorm.DB {
	return tx.Table(table).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND period_id=? AND period_version=?", s.p.TenantID, s.hospital, s.staff, s.period, s.version)
}
func (h *handlers) employmentEvidenceTransaction(c *gin.Context, s employmentEvidenceScope, writing bool, fn func(*gorm.DB, EmploymentPeriodSource, EmploymentPeriodDraft) error) error {
	return h.employmentPeriodTransaction(c, s.p, s.hospital, s.staff, func(tx *gorm.DB, source EmploymentPeriodSource) error {
		mode := "read"
		if writing {
			mode = "write"
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_evidence',?,true)`, mode).Error; err != nil {
			return err
		}
		var period EmploymentPeriodDraft
		if err := tx.Table("workforce_employment_period_history").Select(employmentPeriodColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=?", s.p.TenantID, s.hospital, s.staff, s.period, s.version).Take(&period).Error; err != nil {
			return err
		}
		return fn(tx, source, period)
	})
}
func evidenceAudit(tx *gorm.DB, s employmentEvidenceScope, action string, version int64) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('period_id',?::uuid,'period_version',?::bigint,'binding_version',?::bigint))`, s.p.TenantID, s.hospital, s.p.Subject, action, s.binding, s.period, s.version, version).Error
}
func evidenceReceipt(tx *gorm.DB, s employmentEvidenceScope, command uuid.UUID, intent string) (EmploymentEvidenceBinding, bool, error) {
	var receipt employmentEvidenceRecord
	err := tx.Table("workforce_employment_evidence_history").Select(employmentEvidenceColumns).Where("tenant_id=? AND hospital_id=? AND revision_id=?", s.p.TenantID, s.hospital, command).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return EmploymentEvidenceBinding{}, false, nil
	}
	if err != nil {
		return receipt.EmploymentEvidenceBinding, false, err
	}
	if receipt.ID != s.binding || receipt.StaffID != s.staff || receipt.PeriodID != s.period || receipt.PeriodVersion != s.version || receipt.ActorSubject != s.p.Subject || receipt.InputHash != intent {
		return receipt.EmploymentEvidenceBinding, false, errEmploymentEvidenceConflict
	}
	return receipt.EmploymentEvidenceBinding, true, evidenceAudit(tx, s, "workforce.employment_evidence.receipt.read", receipt.Version)
}
func evidenceError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, errEmploymentEvidenceConflict) || errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "workforce_employment_evidence_source" || pg.ConstraintName == "workforce_employment_evidence_release" || pg.ConstraintName == "workforce_employment_evidence_limit") {
		respond.Error(c, 409, "employment_evidence_conflict", "Evidence, Staff source or terms changed, or the evidence is not available. Reload before a new command.")
		return
	}
	if errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "40P01" || pg.Code == "40001") {
		respond.Error(c, 503, "employment_evidence_busy", "Evidence is temporarily busy. Retry the same command.")
		return
	}
	respondError(c, err)
}
func (h *handlers) attachEmploymentEvidence(c *gin.Context) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return
	}
	var in EmploymentEvidenceAttachInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.DocumentID == uuid.Nil || !evidenceReason(&in.Reason) {
		respond.Error(c, 400, "invalid_request", "Choose a protected document and enter a reason.")
		return
	}
	intent, err := employmentEvidenceIntent("attach", in)
	if err != nil {
		evidenceError(c, err)
		return
	}
	var result EmploymentEvidenceBinding
	err = h.employmentEvidenceTransaction(c, s, true, func(tx *gorm.DB, source EmploymentPeriodSource, period EmploymentPeriodDraft) error {
		receipt, found, err := evidenceReceipt(tx, s, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt
			return nil
		}
		var head EmploymentPeriodDraft
		if err := tx.Table("workforce_employment_period_drafts").Select("version,status,staff_version,employment_epoch").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", s.p.TenantID, s.hospital, s.staff, s.period).Take(&head).Error; err != nil {
			return err
		}
		if head.Status != "draft" || head.Version != s.version || source.StaffVersion != period.StaffVersion || source.EmploymentEpoch != period.EmploymentEpoch {
			return errEmploymentEvidenceConflict
		}
		var file document
		if err := tx.Select(documentColumns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", s.p.TenantID, s.hospital, s.staff, in.DocumentID).Take(&file).Error; err != nil {
			return err
		}
		if file.RecordKind != "enrollment" || file.RecordID != s.staff || file.Category != "employment_evidence" || file.RecordVersion != period.StaffVersion || file.Status != "available" || file.SourceGeneration == nil || file.ReleasedGeneration == nil || file.ScannedAt == nil || file.EngineVersion == nil || file.DefinitionVersion == nil || file.DefinitionsUpdatedAt == nil {
			return errEmploymentEvidenceConflict
		}
		values := map[string]any{"tenant_id": s.p.TenantID, "hospital_id": s.hospital, "staff_id": s.staff, "id": s.binding, "revision_id": in.CommandID, "period_id": s.period, "period_revision_id": period.RevisionID, "period_version": s.version, "version": 1, "status": "attached", "staff_version": period.StaffVersion, "employment_epoch": period.EmploymentEpoch, "document_id": in.DocumentID, "document_record_version": file.RecordVersion, "filename": file.Filename, "content_type": file.ContentType, "size": file.Size, "sha256": file.SHA256, "source_generation": *file.SourceGeneration, "released_generation": *file.ReleasedGeneration, "scan_attempt": file.ScanAttempt, "scanned_at": *file.ScannedAt, "engine_version": *file.EngineVersion, "definition_version": *file.DefinitionVersion, "definitions_updated_at": *file.DefinitionsUpdatedAt, "proposed_by_subject": s.p.Subject, "actor_subject": s.p.Subject, "reason": in.Reason, "input_hash": intent}
		if err := tx.Table("workforce_employment_evidence_bindings").Create(values).Error; err != nil {
			return err
		}
		if err := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", s.binding).Take(&result).Error; err != nil {
			return err
		}
		return evidenceAudit(tx, s, "workforce.employment_evidence.attached", result.Version)
	})
	if err != nil {
		evidenceError(c, err)
		return
	}
	respond.OK(c, employmentEvidenceUTC(result))
}
func (h *handlers) withdrawEmploymentEvidence(c *gin.Context) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return
	}
	var in EmploymentEvidenceWithdrawInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion > 9007199254740990 || !evidenceReason(&in.Reason) {
		respond.Error(c, 400, "invalid_request", "Enter a reason and current evidence version.")
		return
	}
	intent, err := employmentEvidenceIntent("withdraw", in)
	if err != nil {
		evidenceError(c, err)
		return
	}
	var result EmploymentEvidenceBinding
	err = h.employmentEvidenceTransaction(c, s, true, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
		receipt, found, err := evidenceReceipt(tx, s, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt
			return nil
		}
		var current EmploymentEvidenceBinding
		if err := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", s.binding).Take(&current).Error; err != nil {
			return err
		}
		// Downloads also lock document before binding, including their final recheck.
		var file document
		if err := tx.Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", s.p.TenantID, s.hospital, s.staff, current.DocumentID).Take(&file).Error; err != nil {
			return err
		}
		if current.Version != *in.ExpectedVersion || current.Status != "attached" {
			return errEmploymentEvidenceConflict
		}
		changed := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Where("id=? AND version=? AND status='attached'", s.binding, *in.ExpectedVersion).Updates(map[string]any{"version": *in.ExpectedVersion + 1, "revision_id": in.CommandID, "status": "withdrawn", "reason": in.Reason, "actor_subject": s.p.Subject, "input_hash": intent})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return errEmploymentEvidenceConflict
		}
		if err := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", s.binding).Take(&result).Error; err != nil {
			return err
		}
		return evidenceAudit(tx, s, "workforce.employment_evidence.withdrawn", result.Version)
	})
	if err != nil {
		evidenceError(c, err)
		return
	}
	respond.OK(c, employmentEvidenceUTC(result))
}
func (h *handlers) getEmploymentEvidence(c *gin.Context) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return
	}
	var result EmploymentEvidenceBinding
	err := h.employmentEvidenceTransaction(c, s, false, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
		if err := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", s.binding).Take(&result).Error; err != nil {
			return err
		}
		return evidenceAudit(tx, s, "workforce.employment_evidence.read", result.Version)
	})
	if err != nil {
		evidenceError(c, err)
		return
	}
	respond.OK(c, employmentEvidenceUTC(result))
}
func (h *handlers) listEmploymentEvidence(c *gin.Context, params pagination.Params) ([]EmploymentEvidenceBinding, error) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return nil, nil
	}
	rows := []EmploymentEvidenceBinding{}
	err := h.employmentEvidenceTransaction(c, s, false, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
		if err := tenantdb.ApplyKeyset(evidenceWhere(tx, s, "workforce_employment_evidence_bindings"), params).Select(employmentEvidenceColumns).Find(&rows).Error; err != nil {
			return err
		}
		s.binding = s.period
		return evidenceAudit(tx, s, "workforce.employment_evidence.list", 0)
	})
	if err != nil {
		evidenceError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i] = employmentEvidenceUTC(rows[i])
	}
	return rows, nil
}
func (h *handlers) employmentEvidenceHistory(c *gin.Context, params pagination.Params) ([]employmentEvidenceRevision, error) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return nil, nil
	}
	rows := []employmentEvidenceRevision{}
	err := h.employmentEvidenceTransaction(c, s, false, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
		var head EmploymentEvidenceBinding
		if err := evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select("id,version").Where("id=?", s.binding).Take(&head).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(evidenceWhere(tx, s, "workforce_employment_evidence_history").Where("id=?", s.binding), params, tenantdb.Keyset{Column: "updated_at", Direction: pagination.NewestFirst}).Select(employmentEvidenceColumns).Find(&rows).Error; err != nil {
			return err
		}
		return evidenceAudit(tx, s, "workforce.employment_evidence.history.read", head.Version)
	})
	if err != nil {
		evidenceError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].EmploymentEvidenceBinding = employmentEvidenceUTC(rows[i].EmploymentEvidenceBinding)
	}
	return rows, nil
}

func (employmentEvidenceGate) permissions(bool) (authz.Permission, authz.Permission) {
	return PermDocumentManage, PermEnrollmentManage
}

type employmentEvidenceGate struct {
	scope employmentEvidenceScope
	file  uuid.UUID
}

func (g employmentEvidenceGate) authorize(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, _ bool) error {
	if _, err := documentOwner(tx, p, hospital, staff); err != nil {
		return err
	}
	return tx.Exec(`SELECT set_config('app.workforce_employment_evidence','read',true)`).Error
}
func (employmentEvidenceGate) bind(*gorm.DB, authn.Principal, uuid.UUID, uuid.UUID, string, uuid.UUID) (int64, error) {
	return 0, gorm.ErrRecordNotFound
}
func (g employmentEvidenceGate) owns(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, kind string, record uuid.UUID) error {
	if kind != "enrollment" || record != staff {
		return gorm.ErrRecordNotFound
	}
	var row struct{ ID uuid.UUID }
	err := tx.Table("workforce_employment_evidence_bindings AS b").Select("b.id").Joins("JOIN workforce_documents d ON d.tenant_id=b.tenant_id AND d.hospital_id=b.hospital_id AND d.staff_id=b.staff_id AND d.id=b.document_id").Where("b.tenant_id=? AND b.hospital_id=? AND b.staff_id=? AND b.period_id=? AND b.period_version=? AND b.id=? AND b.document_id=? AND b.status='attached' AND d.status='available' AND d.released_generation=b.released_generation AND d.source_generation=b.source_generation AND d.sha256=b.sha256 AND d.record_version=b.document_record_version", p.TenantID, hospital, staff, g.scope.period, g.scope.version, g.scope.binding, g.file).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "b"}}).Take(&row).Error
	if err != nil {
		return err
	}
	return evidenceAudit(tx, g.scope, "workforce.employment_evidence.disclosure.authorized", 0)
}
func (employmentEvidenceGate) downloadAction() string {
	return "workforce.employment_evidence.downloaded"
}
func (h *handlers) downloadEmploymentEvidence(c *gin.Context) {
	s, ok := employmentEvidenceParams(c)
	if !ok {
		return
	}
	var binding EmploymentEvidenceBinding
	err := h.employmentEvidenceTransaction(c, s, false, func(tx *gorm.DB, _ EmploymentPeriodSource, _ EmploymentPeriodDraft) error {
		return evidenceWhere(tx, s, "workforce_employment_evidence_bindings").Select(employmentEvidenceColumns).Where("id=?", s.binding).Take(&binding).Error
	})
	if err != nil {
		evidenceError(c, err)
		return
	}
	h.sendDocument(c, employmentEvidenceGate{scope: s, file: binding.DocumentID}, s.p, s.hospital, s.staff, binding.DocumentID)
}
