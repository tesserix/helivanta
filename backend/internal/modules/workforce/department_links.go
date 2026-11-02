package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

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

//go:embed department_links.sql
var departmentLinksSchema string

type departmentLink struct {
	TenantID             uuid.UUID
	HospitalID           uuid.UUID
	MasterID             uuid.UUID
	ID                   uuid.UUID
	MasterRevisionID     uuid.UUID
	DepartmentID         uuid.UUID
	DepartmentRevisionID uuid.UUID
	Version              int64
	ExpectedVersion      int64
	ActorSubject         string
	Reason               string
	Status               string
	ObservedAt           *time.Time
	CreatedAt            time.Time `gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime:false;default:now()"`
}

func (departmentLink) TableName() string { return "workforce_department_links" }
func (r departmentLink) output() DepartmentLinkOutput {
	return DepartmentLinkOutput{RequestID: r.ID, HospitalID: r.HospitalID, MasterID: r.MasterID, MasterRevisionID: r.MasterRevisionID, DepartmentID: r.DepartmentID, DepartmentRevisionID: r.DepartmentRevisionID, Version: r.Version, Status: r.Status, CreatedAt: r.CreatedAt, ObservedAt: r.ObservedAt}
}

func (r DepartmentLinkOutput) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.RequestID }

var errDepartmentLinkConflict = errors.New("department handoff intent or source conflicts")

func departmentLinkScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return p, hospital, uuid.Nil, uuid.Nil, false
	}
	master, err := uuid.Parse(c.Param("id"))
	if err != nil || master == uuid.Nil {
		respond.NotFound(c, "department handoff")
		return p, hospital, master, uuid.Nil, false
	}
	var request uuid.UUID
	if c.Param("request") != "" {
		request, err = uuid.Parse(c.Param("request"))
		if err != nil || request.Version() != 7 || request.Variant() != uuid.RFC4122 {
			respond.NotFound(c, "department handoff")
			return p, hospital, master, request, false
		}
	}
	return p, hospital, master, request, true
}

func (h *handlers) departmentLinkTransaction(ctx context.Context, p authn.Principal, hospital uuid.UUID, writing bool, fn func(*gorm.DB) error) error {
	return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
		var grant struct{ Level string }
		if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
			return err
		}
		if (writing && grant.Level != "manager") || (grant.Level != "reader" && grant.Level != "manager") {
			return gorm.ErrRecordNotFound
		}

		return fn(tx)
	})
}

func respondDepartmentLinkError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "department mapping")
		return
	}
	var pg *pgconn.PgError
	if errors.Is(err, errDepartmentLinkConflict) || (errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "workforce_department_link_guard")) {
		respond.Conflict(c, "The department handoff or source changed. Refresh and review the current revisions.")
		return
	}
	respond.InternalErr(c, errors.New("department handoff persistence failed"), "The department mapping could not be saved or loaded.")
}

func (h *handlers) publishDepartmentLink(tx *gorm.DB, row departmentLink) error {
	raw, err := json.Marshal(workforcecontract.DepartmentLinkRequestedData{RequestID: row.ID, HospitalID: row.HospitalID, MasterID: row.MasterID, MasterRevisionID: row.MasterRevisionID, DepartmentID: row.DepartmentID, DepartmentRevisionID: row.DepartmentRevisionID})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, workforcecontract.SubjectDepartmentLinkRequested, events.Event{Type: workforcecontract.SubjectDepartmentLinkRequested, Version: 1, TenantID: row.TenantID.String(), Data: raw})
}

func (h *handlers) putDepartmentLink(c *gin.Context) {
	p, hospital, master, id, ok := departmentLinkScope(c)
	if !ok {
		return
	}
	var in DepartmentLinkInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.Join(strings.Fields(in.Reason), " ")
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || *in.ExpectedVersion > 9007199254740990 || in.MasterRevisionID == uuid.Nil ||
		in.DepartmentID.Version() != 7 || in.DepartmentID.Variant() != uuid.RFC4122 || in.DepartmentRevisionID.Version() != 7 || in.DepartmentRevisionID.Variant() != uuid.RFC4122 ||
		in.Reason == "" || len([]rune(in.Reason)) > 500 || strings.IndexFunc(in.Reason, unicode.IsControl) >= 0 {
		respond.Error(c, 400, "invalid_request", "Provide current source and department revisions, mapping version and reason.")
		return
	}
	var row departmentLink
	err := h.departmentLinkTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":department-link:"+hospital.String()+":"+master.String()).Error; err != nil {
			return err
		}
		err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error
		if err == nil {
			if row.HospitalID != hospital || row.MasterID != master || row.ActorSubject != p.Subject || row.ExpectedVersion != *in.ExpectedVersion || row.MasterRevisionID != in.MasterRevisionID || row.DepartmentID != in.DepartmentID || row.DepartmentRevisionID != in.DepartmentRevisionID || row.Reason != in.Reason {
				return errDepartmentLinkConflict
			}
			if row.Status == "pending" {
				return h.publishDepartmentLink(tx, row)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var source hrMaster
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=? AND kind='department'", p.TenantID, hospital, master).Take(&source).Error; err != nil {
			return err
		}
		if !source.Active || source.RevisionID != in.MasterRevisionID {
			return errDepartmentLinkConflict
		}
		var head struct {
			Version int64
			Pending bool
		}
		if err := tx.Raw(`SELECT COALESCE(max(version),0) AS version,COALESCE(bool_or(status='pending'),false) AS pending FROM workforce_department_links WHERE tenant_id=? AND hospital_id=? AND master_id=?`, p.TenantID, hospital, master).Scan(&head).Error; err != nil {
			return err
		}
		if head.Pending || head.Version != *in.ExpectedVersion {
			return errDepartmentLinkConflict
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_department_link','authorized',true)`).Error; err != nil {
			return err
		}
		row = departmentLink{TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, MasterID: master, ID: id, MasterRevisionID: in.MasterRevisionID, DepartmentID: in.DepartmentID, DepartmentRevisionID: in.DepartmentRevisionID, Version: head.Version + 1, ExpectedVersion: *in.ExpectedVersion, ActorSubject: p.Subject, Reason: in.Reason, Status: "pending"}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.department_link.requested',?,jsonb_build_object('version',?::bigint))`, p.TenantID, hospital, p.Subject, id, row.Version).Error; err != nil {
			return err
		}
		return h.publishDepartmentLink(tx, row)
	})
	if err != nil {
		respondDepartmentLinkError(c, err)
		return
	}
	respond.OK(c, row.output())
}

func (h *handlers) getDepartmentLink(c *gin.Context) {
	p, hospital, master, id, ok := departmentLinkScope(c)
	if !ok {
		return
	}
	var row departmentLink
	err := h.departmentLinkTransaction(c.Request.Context(), p, hospital, false, func(tx *gorm.DB) error {
		return tx.Where("tenant_id=? AND hospital_id=? AND master_id=? AND id=?", p.TenantID, hospital, master, id).Take(&row).Error
	})
	if err != nil {
		respondDepartmentLinkError(c, err)
		return
	}
	respond.OK(c, row.output())
}

func (h *handlers) listDepartmentLinks(c *gin.Context, params pagination.Params) ([]DepartmentLinkOutput, error) {
	p, hospital, master, _, ok := departmentLinkScope(c)
	if !ok {
		return nil, nil
	}
	rows := []departmentLink{}
	err := h.departmentLinkTransaction(c.Request.Context(), p, hospital, false, func(tx *gorm.DB) error {
		var source hrMaster
		if err := tx.Where("tenant_id=? AND hospital_id=? AND id=? AND kind='department'", p.TenantID, hospital, master).Take(&source).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND hospital_id=? AND master_id=?", p.TenantID, hospital, master).Find(&rows).Error
	})
	if err != nil {
		respondDepartmentLinkError(c, err)
		return nil, nil
	}
	out := make([]DepartmentLinkOutput, len(rows))
	for i, row := range rows {
		out[i] = row.output()
	}
	return out, nil
}
