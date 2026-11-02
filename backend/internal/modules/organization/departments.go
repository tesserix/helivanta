package organization

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed departments.sql
var departmentsSchema string

//go:embed department_revision_integrity.sql
var departmentRevisionIntegritySchema string

var departmentCode = regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)

type departmentRecord struct {
	TenantID         uuid.UUID `json:"-"`
	DepartmentOutput `gorm:"embedded"`
	UpdatedAt        time.Time `json:"-" gorm:"autoUpdateTime:false"`
}

func (departmentRecord) TableName() string                 { return "organization_departments" }
func (d departmentRecord) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

type departmentRevision struct {
	TenantID        uuid.UUID
	ID              uuid.UUID
	HospitalID      uuid.UUID
	NodeID          uuid.UUID
	Version         int64
	Code            string
	Name            string
	Kind            string
	ParentID        *uuid.UUID
	Status          string
	EffectiveAt     time.Time
	NodeCreatedAt   time.Time
	CreatedAt       time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime:false"`
	ExpectedVersion int64
	ActorSubject    string
	Reason          string
}

func (departmentRevision) TableName() string                 { return "organization_department_revisions" }
func (d departmentRevision) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }
func (d departmentRevision) output() DepartmentOutput {
	return DepartmentOutput{ID: d.NodeID, HospitalID: d.HospitalID, RevisionID: d.ID, Version: d.Version, Code: d.Code, Name: d.Name, Kind: d.Kind, ParentID: d.ParentID, Status: d.Status, EffectiveAt: d.EffectiveAt, CreatedAt: d.NodeCreatedAt}
}
func (d departmentRevision) MarshalJSON() ([]byte, error) { return json.Marshal(d.output()) }

func departmentHospital(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, ok := principal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "department")
		return p, uuid.Nil, false
	}
	return p, hospital, true
}
func departmentIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	node, e := uuid.Parse(c.Param("node"))
	revision, r := uuid.Parse(c.Param("revision"))
	if e != nil || r != nil || node.Version() != 7 || node.Variant() != uuid.RFC4122 || revision.Version() != 7 || revision.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "department revision")
		return node, revision, false
	}
	return node, revision, true
}
func validDepartmentText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= max && strings.IndexFunc(value, unicode.IsControl) < 0
}
func departmentInput(c *gin.Context) (DepartmentCommand, bool) {
	var in DepartmentCommand
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Supply a complete department command.")
		return in, false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		respond.Error(c, 400, "invalid_request", "Supply one department command.")
		return in, false
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Reason = strings.Join(strings.Fields(in.Reason), " ")
	valid := in.ExpectedVersion != nil && *in.ExpectedVersion >= 0 && *in.ExpectedVersion < 9007199254740991 && departmentCode.MatchString(in.Code) && validDepartmentText(in.Name, 120) && validDepartmentText(in.Reason, 500)
	valid = valid && (in.Kind == "department" || in.Kind == "sub_unit") && (in.Status == "active" || in.Status == "retired")
	valid = valid && ((in.Kind == "department" && in.ParentID == nil) || (in.Kind == "sub_unit" && in.ParentID != nil && *in.ParentID != uuid.Nil))
	if !valid {
		respond.Error(c, 400, "invalid_request", "Supply a valid version, immutable code and kind, name, parent, status and reason.")
		return in, false
	}
	return in, true
}
func requireDepartmentHospital(tx *gorm.DB, tenant string, hospital uuid.UUID) error {
	var ref registration
	if err := tx.Where("tenant_id=? AND id=?", tenant, hospital).Take(&ref).Error; err != nil {
		return err
	}
	if ref.Status != "verified" {
		return errConflict
	}
	return nil
}
func departmentCommandMatches(r departmentRevision, p authn.Principal, hospital, node uuid.UUID, in DepartmentCommand) bool {
	parentEqual := (r.ParentID == nil && in.ParentID == nil) || (r.ParentID != nil && in.ParentID != nil && *r.ParentID == *in.ParentID)
	return r.HospitalID == hospital && r.NodeID == node && r.ExpectedVersion == *in.ExpectedVersion && r.ActorSubject == p.Subject && r.Code == in.Code && r.Name == in.Name && r.Kind == in.Kind && parentEqual && r.Status == in.Status && r.Reason == in.Reason
}
func (h *handlers) putDepartment(c *gin.Context) {
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return
	}
	node, id, ok := departmentIDs(c)
	if !ok {
		return
	}
	in, ok := departmentInput(c)
	if !ok {
		return
	}
	var result departmentRevision
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":organization-department-command:"+id.String()).Error; err != nil {
			return err
		}
		if err := requireDepartmentHospital(tx, p.TenantID, hospital); err != nil {
			return err
		}
		err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&result).Error
		if err == nil {
			if !departmentCommandMatches(result, p, hospital, node, in) {
				return errConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":organization-departments:"+hospital.String()).Error; err != nil {
			return err
		}
		var head departmentRecord
		err = tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, node).Take(&head).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		if exists {
			if head.Version != *in.ExpectedVersion || head.Code != in.Code || head.Kind != in.Kind || head.Status != "active" {
				return errConflict
			}
		} else if *in.ExpectedVersion != 0 || in.Status != "active" {
			return errConflict
		}
		if in.ParentID != nil {
			var parent departmentRecord
			if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, *in.ParentID).Take(&parent).Error; err != nil {
				return err
			}
			if parent.Status != "active" {
				return errConflict
			}
		}
		if h.bus == nil {
			return errUnavailable
		}
		var now time.Time
		if err := tx.Raw(`SELECT GREATEST(clock_timestamp(), ?::timestamptz + INTERVAL '1 microsecond')`, head.EffectiveAt).Scan(&now).Error; err != nil {
			return err
		}
		result = departmentRevision{TenantID: uuid.MustParse(p.TenantID), ID: id, HospitalID: hospital, NodeID: node, Version: head.Version + 1, Code: in.Code, Name: in.Name, Kind: in.Kind, ParentID: in.ParentID, Status: in.Status, EffectiveAt: now, NodeCreatedAt: now, CreatedAt: now, UpdatedAt: now, ExpectedVersion: *in.ExpectedVersion, ActorSubject: p.Subject, Reason: in.Reason}
		if exists {
			result.NodeCreatedAt = head.CreatedAt
		}
		if err := tx.Exec(`SELECT set_config('app.organization_department_write','authorized',true)`).Error; err != nil {
			return err
		}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		head = departmentRecord{TenantID: result.TenantID, DepartmentOutput: result.output(), UpdatedAt: now}
		if exists {
			saved := tx.Model(&departmentRecord{}).Where("tenant_id=? AND hospital_id=? AND id=? AND version=?", p.TenantID, hospital, node, *in.ExpectedVersion).Updates(map[string]any{"name": head.Name, "parent_id": head.ParentID, "status": head.Status, "version": head.Version, "revision_id": head.RevisionID, "effective_at": head.EffectiveAt, "updated_at": head.UpdatedAt})
			if saved.Error != nil {
				return saved.Error
			}
			if saved.RowsAffected != 1 {
				return errConflict
			}
		} else if err := tx.Create(&head).Error; err != nil {
			return err
		}
		if err := audit(tx, p.TenantID, p.Subject, "department_revision_recorded", "organization_department_revisions", id); err != nil {
			return err
		}
		raw, err := json.Marshal(organizationcontract.DepartmentRevisionChangedData{HospitalID: hospital, NodeID: node, RevisionID: id, Version: result.Version})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, organizationcontract.SubjectDepartmentRevisionChanged, events.Event{Type: organizationcontract.SubjectDepartmentRevisionChanged, Version: 1, TenantID: p.TenantID, Data: raw})
	})
	if err != nil {
		respondDepartmentError(c, err)
		return
	}
	respond.OK(c, result.output())
}
func respondDepartmentError(c *gin.Context, err error) {
	var postgres *pgconn.PgError
	if errors.Is(err, errConflict) || errors.As(err, &postgres) && strings.HasPrefix(postgres.ConstraintName, "organization_department") && (postgres.Code == "23514" || postgres.Code == "23505" || postgres.Code == "23503") {
		respond.Error(c, 409, "conflict", "The department changed or this hierarchy change is invalid. Reload the current structure.")
		return
	}
	respondError(c, err)
}
func (h *handlers) getDepartmentRevision(c *gin.Context) {
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return
	}
	node, id, ok := departmentIDs(c)
	if !ok {
		return
	}
	var result departmentRevision
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id=? AND hospital_id=? AND node_id=? AND id=?", p.TenantID, hospital, node, id).Take(&result).Error; err != nil {
			return err
		}
		return audit(tx, p.TenantID, p.Subject, "department_revision_read", "organization_department_revisions", id)
	})
	if err != nil {
		respondDepartmentError(c, err)
		return
	}
	respond.OK(c, result.output())
}
func (h *handlers) listDepartments(c *gin.Context, params pagination.Params) ([]departmentRecord, error) {
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return nil, nil
	}
	rows := []departmentRecord{}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := requireDepartmentHospital(tx, p.TenantID, hospital); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		return audit(tx, p.TenantID, p.Subject, "department_list", "organization_departments", hospital)
	})
	if err != nil {
		respondDepartmentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) listDepartmentRevisions(c *gin.Context, params pagination.Params) ([]departmentRevision, error) {
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return nil, nil
	}
	node, err := uuid.Parse(c.Param("node"))
	if err != nil || node.Version() != 7 || node.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "department")
		return nil, nil
	}
	rows := []departmentRevision{}
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var head departmentRecord
		if err := tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, node).Take(&head).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND hospital_id=? AND node_id=?", p.TenantID, hospital, node).Find(&rows).Error; err != nil {
			return err
		}
		return audit(tx, p.TenantID, p.Subject, "department_history_read", "organization_department_revisions", node)
	})
	if err != nil {
		respondDepartmentError(c, err)
		return nil, nil
	}
	return rows, nil
}
