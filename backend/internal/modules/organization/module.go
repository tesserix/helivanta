// Package organization owns structural departments and their canonical hospital references.
package organization

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PermStructureRead   authz.Permission = "organization.structure.read"
	PermStructureManage authz.Permission = "organization.structure.manage"
)

//go:embed schema.sql
var schema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "organization" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermStructureRead}, {Permission: PermStructureManage}}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_organization", SQL: schema}, {ID: "0002_organization_departments", SQL: departmentsSchema}, {ID: "0003_organization_department_revision_integrity", SQL: departmentRevisionIntegritySchema}}
}
func (*Module) Publishes() []string {
	return []string{organizationcontract.SubjectHospitalReferenceRequested, organizationcontract.SubjectDepartmentRevisionChanged}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer {
	return []events.Consumer{hospitalObservationConsumer()}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }
func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, bus: deps.Bus}
	g := r.Group("/organization")
	platform.ListRoute(g, "/hospitals", PermStructureRead, h.list)
	g.PUT("/hospitals/:hospital/registrations/:request", PermStructureManage, h.put)
	g.GET("/hospitals/:hospital/registrations/:request", PermStructureRead, h.get)
	g.PUT("/hospitals/:hospital/departments/:node/revisions/:revision", PermStructureManage, h.putDepartment)
	g.GET("/hospitals/:hospital/departments/:node/revisions/:revision", PermStructureRead, h.getDepartmentRevision)
	platform.ListRoute(g, "/hospitals/:hospital/departments", PermStructureRead, h.listDepartments)
	platform.ListRoute(g, "/hospitals/:hospital/departments/:node/revisions", PermStructureRead, h.listDepartmentRevisions)
}

type handlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}
type registration struct {
	TenantID  uuid.UUID `json:"-"`
	ID        uuid.UUID `json:"hospital_id"`
	RequestID uuid.UUID `json:"request_id"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt time.Time `json:"-" gorm:"autoUpdateTime:false;default:now()"`
}

func (r registration) MarshalJSON() ([]byte, error) {
	return json.Marshal(OrganizationHospitalHead{HospitalID: r.ID, RequestID: r.RequestID, Status: r.Status, Version: r.Version, CreatedAt: r.CreatedAt})
}
func (registration) TableName() string                 { return "organization_hospital_registrations" }
func (r registration) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type registrationRequest struct {
	TenantID       uuid.UUID
	ID             uuid.UUID
	HospitalID     uuid.UUID
	Reason         string
	ActorSubject   string
	Status         string
	ObservedExists *bool
	CreatedAt      time.Time `gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime:false;default:now()"`
}

func (registrationRequest) TableName() string { return "organization_hospital_requests" }
func (r registrationRequest) output() OrganizationRegistrationOutput {
	return OrganizationRegistrationOutput{RequestID: r.ID, HospitalID: r.HospitalID, Status: r.Status, CreatedAt: r.CreatedAt}
}

var errConflict = errors.New("organization registration conflicts")
var errUnavailable = errors.New("organization registration unavailable")

func principal(c *gin.Context) (authn.Principal, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, false
	}
	for name, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(name)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "session_changed", "Your workspace changed. Reopen organization setup.")
			return p, false
		}
	}
	return p, true
}
func target(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, ok := principal(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	hospital, e := uuid.Parse(c.Param("hospital"))
	if e != nil || hospital == uuid.Nil {
		respond.NotFound(c, "hospital registration")
		return p, uuid.Nil, uuid.Nil, false
	}
	request, e := uuid.Parse(c.Param("request"))
	if e != nil || request.Version() != 7 || request.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "hospital registration")
		return p, uuid.Nil, uuid.Nil, false
	}
	return p, hospital, request, true
}
func decodeReason(c *gin.Context) (string, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	var in OrganizationRegistrationInput
	if d.Decode(&in) != nil {
		respond.BadRequest(c, errors.New("provide one registration reason"))
		return "", false
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		respond.BadRequest(c, errors.New("provide one registration object"))
		return "", false
	}
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" || len([]rune(reason)) > 500 || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
		respond.BadRequest(c, errors.New("provide a reason of up to 500 characters"))
		return "", false
	}
	return reason, true
}
func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "hospital registration")
	case errors.Is(err, errConflict):
		respond.Error(c, 409, "conflict", "This registration no longer matches the requested intent.")
	case errors.Is(err, errUnavailable):
		respond.Error(c, 503, "organization_unavailable", "Organization reference verification is unavailable.")
	default:
		respond.InternalErr(c, err, "organization registration could not be saved")
	}
}
func audit(tx *gorm.DB, tenant, actor, action, resource string, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,?,?)`, tenant, actor, action, resource, id).Error
}
func (h *handlers) put(c *gin.Context) {
	p, hospital, id, ok := target(c)
	if !ok {
		return
	}
	reason, ok := decodeReason(c)
	if !ok {
		return
	}
	var result registrationRequest
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":organization-registration:"+id.String()).Error; e != nil {
			return e
		}
		e := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&result).Error
		if e == nil {
			if result.HospitalID != hospital || result.Reason != reason || result.ActorSubject != p.Subject {
				return errConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":organization-hospital:"+hospital.String()).Error; e != nil {
			return e
		}
		var head registration
		e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", p.TenantID, hospital).Take(&head).Error
		if e == nil && head.Status != "rejected" {
			return errConflict
		}
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if h.bus == nil {
			return errUnavailable
		}
		if e := tx.Exec(`SELECT set_config('app.organization_write','authorized',true)`).Error; e != nil {
			return e
		}
		result = registrationRequest{TenantID: uuid.MustParse(p.TenantID), ID: id, HospitalID: hospital, Reason: reason, ActorSubject: p.Subject, Status: "pending"}
		if e := tx.Create(&result).Error; e != nil {
			return e
		}
		if head.ID == uuid.Nil {
			head = registration{TenantID: uuid.MustParse(p.TenantID), ID: hospital, RequestID: id, Status: "pending", Version: 1}
			if e := tx.Create(&head).Error; e != nil {
				return e
			}
		} else {
			if e := tx.Model(&head).Where("tenant_id=? AND id=?", p.TenantID, hospital).Updates(map[string]any{"request_id": id, "status": "pending", "version": head.Version + 1, "updated_at": gorm.Expr("clock_timestamp()")}).Error; e != nil {
				return e
			}
		}
		if e := audit(tx, p.TenantID, p.Subject, "hospital_reference_requested", "organization_hospital_requests", id); e != nil {
			return e
		}
		raw, e := json.Marshal(organizationcontract.HospitalReferenceRequestedData{RequestID: id, HospitalID: hospital})
		if e != nil {
			return e
		}
		return h.bus.Publish(tx, organizationcontract.SubjectHospitalReferenceRequested, events.Event{Type: organizationcontract.SubjectHospitalReferenceRequested, Version: 1, TenantID: p.TenantID, Data: raw})
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, result.output())
}
func (h *handlers) get(c *gin.Context) {
	p, hospital, id, ok := target(c)
	if !ok {
		return
	}
	var result registrationRequest
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := tx.Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(&result).Error; e != nil {
			return e
		}
		return audit(tx, p.TenantID, p.Subject, "hospital_reference_read", "organization_hospital_requests", id)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, result.output())
}
func (h *handlers) list(c *gin.Context, params pagination.Params) ([]registration, error) {
	p, ok := principal(c)
	if !ok {
		return nil, nil
	}
	var rows []registration
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if e := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=?", p.TenantID).Find(&rows).Error; e != nil {
			return e
		}
		return audit(tx, p.TenantID, p.Subject, "hospital_reference_list", "organization_hospital_registrations", uuid.MustParse(p.TenantID))
	})
	return rows, err
}
