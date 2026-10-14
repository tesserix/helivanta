package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type hrMaster struct {
	ID          uuid.UUID `json:"id"`
	RevisionID  uuid.UUID `json:"revision_id"`
	Kind        string    `json:"kind"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (hrMaster) TableName() string                 { return "workforce_hr_masters" }
func (m hrMaster) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

type hrMasterRevision struct {
	ID          uuid.UUID `json:"id"`
	MasterID    uuid.UUID `json:"master_id"`
	Kind        string    `json:"kind"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}

func (hrMasterRevision) TableName() string                 { return "workforce_hr_master_revisions" }
func (m hrMasterRevision) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

type hrMasterInput struct {
	ExpectedVersion *int64 `json:"expected_version" binding:"required,gte=0"`
	Kind            string `json:"kind" binding:"required,oneof=department designation grade qualification document_category exit_reason relationship language contract_type"`
	Code            string `json:"code" binding:"required,max=40"`
	Name            string `json:"name" binding:"required,max=80"`
	Description     string `json:"description" binding:"max=500"`
	Active          *bool  `json:"active" binding:"required"`
}

var errMasterVersion = errors.New("master version conflict")
var errMasterInactive = errors.New("master revision is not available for new assignments")

func validMasterKind(kind string) bool {
	return contains(kind, "department", "designation", "grade", "qualification", "document_category", "exit_reason", "relationship", "language", "contract_type")
}

func (h *handlers) listMasters(c *gin.Context, params pagination.Params) ([]hrMaster, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	kind := c.Query("kind")
	if kind != "" && !validMasterKind(kind) {
		respond.Error(c, 400, "invalid_request", "invalid HR category")
		return nil, nil
	}
	rows := []hrMaster{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital)
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) masterHistory(c *gin.Context, params pagination.Params) ([]hrMasterRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "master")
		return nil, nil
	}
	rows := []hrMasterRevision{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var master hrMaster
		if err := tx.Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&master).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ? AND master_id = ?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putMaster(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "master")
		return
	}
	var in hrMasterInput
	if !decode(c, &in) {
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.TrimSpace(in.Description)
	if !employeeCode.MatchString(in.Code) || in.Name == "" || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) {
		respond.Error(c, 400, "invalid_request", "invalid HR setup fields")
		return
	}
	var result hrMaster
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var grant struct{ Level string }
		if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
			return err
		}
		if grant.Level != "manager" {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_master','authorized',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":master:"+id.String()).Error; err != nil {
			return err
		}
		var old hrMaster
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&old).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		version := int64(1)
		if err == nil {
			if old.Kind != in.Kind || old.Code != in.Code {
				return errMasterVersion
			}
			if old.Name == in.Name && old.Description == in.Description && old.Active == *in.Active {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errMasterVersion
			}
			version = old.Version + 1
		} else if *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_hr_masters (tenant_id,hospital_id,id,kind,code,name,description,active,version,revision_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT (tenant_id,hospital_id,id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,active=EXCLUDED.active,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, id, in.Kind, in.Code, in.Name, in.Description, *in.Active, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_hr_master_revisions (tenant_id,hospital_id,id,master_id,version,kind,code,name,description,active) VALUES (?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, id, version, in.Kind, in.Code, in.Name, in.Description, *in.Active).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES (?,?,?,'workforce.master.changed',?,jsonb_build_object('kind',?::text,'version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, id, in.Kind, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.MasterChangedData{HospitalID: hospital.String(), MasterID: id.String(), RevisionID: revision.String(), Kind: in.Kind, Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, MasterChanged, events.Event{Type: MasterChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&result).Error
	})
	if errors.Is(err, errMasterVersion) {
		respond.Conflict(c, "HR setup changed since you opened it, or its code/category was changed. Reload before saving.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		respond.Conflict(c, "Code or name already exists in this hospital and category.")
		return
	}
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, result)
}

func validateMasterReferences(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, in *enrollmentProfile, previous enrollmentProfile, provided map[string]bool) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_hr_references','supported',true)`).Error; err != nil {
		return err
	}
	for _, ref := range []struct {
		kind     string
		id, name *string
		previous string
	}{
		{"department", &in.DepartmentRevisionID, &in.Department, previous.DepartmentRevisionID},
		{"designation", &in.DesignationRevisionID, &in.Designation, previous.DesignationRevisionID},
		{"grade", &in.GradeRevisionID, &in.Grade, previous.GradeRevisionID},
	} {
		if !provided[ref.kind] {
			*ref.id = ref.previous
		}
		if *ref.id == "" {
			continue
		}
		id, err := uuid.Parse(*ref.id)
		if err != nil || id == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		*ref.id = id.String()
		var row struct {
			Name           string
			Active         bool
			RevisionActive bool
			CurrentID      uuid.UUID
		}
		q := tx.Raw(`SELECT r.name,m.active,r.active AS revision_active,m.revision_id AS current_id FROM workforce_hr_master_revisions r
		 JOIN workforce_hr_masters m ON m.tenant_id=r.tenant_id AND m.hospital_id=r.hospital_id AND m.id=r.master_id
		 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.kind=? FOR SHARE OF m`, p.TenantID, hospital, id, ref.kind).Scan(&row)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if *ref.id != ref.previous && (!row.Active || !row.RevisionActive || row.CurrentID != id) {
			return errMasterInactive
		}
		*ref.name = row.Name
	}
	return nil
}

func respondMasterReferenceError(c *gin.Context, err error) bool {
	if errors.Is(err, errMasterInactive) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose an active current HR setup record for a new assignment.")
		return true
	}
	return false
}
