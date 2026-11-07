package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
)

type handlers struct {
	staffActors                 authz.CurrentStaffActorGuard
	departmentReferences        authz.DepartmentReferenceGuard
	departmentScopes            authz.DepartmentScopeGuard
	pharmacyEmployers           authz.PharmacyEmployerGuard
	employmentActivationEnabled bool
	scopeAuthority              authz.ScopedAuthorizer
	roles                       platform.RoleLister
	db                          *tenantdb.DB
	bus                         *events.Bus
	documents                   *attachments.GCS
	transit                     *transit.Client
	employmentEvidenceSlots     chan struct{}
}

type hospital struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	AccessLevel string    `json:"access_level"`
	CreatedAt   time.Time `json:"created_at"`
}

func (hospital) TableName() string                 { return "workforce_hospitals" }
func (h hospital) PageKey() (time.Time, uuid.UUID) { return h.CreatedAt, h.ID }

func (h *handlers) listHospitals(c *gin.Context, params pagination.Params) ([]hospital, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []hospital
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(`id, name, created_at,
		 (SELECT level FROM workforce_hospital_access a WHERE a.tenant_id = workforce_hospitals.tenant_id
		 AND a.hospital_id = workforce_hospitals.id AND a.subject = ?) AS access_level`, p.Subject).
			Where(`tenant_id = ? AND establishment_kind = 'hospital' AND EXISTS (SELECT 1 FROM workforce_hospital_access a
		 WHERE a.tenant_id = workforce_hospitals.tenant_id AND a.hospital_id = workforce_hospitals.id AND a.subject = ?)`, p.TenantID, p.Subject).
			Find(&rows).Error
	})
	if err != nil {
		return nil, errors.New("hospital discovery failed")
	}
	return rows, nil
}

type staff struct {
	ID               uuid.UUID `json:"id"`
	HospitalID       uuid.UUID `json:"hospital_id"`
	EmployeeCode     string    `json:"employee_code"`
	DisplayName      string    `json:"display_name"`
	JobCategory      string    `json:"job_category"`
	EmploymentStatus string    `json:"employment_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Version          int64     `json:"-"`
}

func (staff) TableName() string                 { return "workforce_staff" }
func (s staff) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

type staffInput struct {
	EmployeeCode     string `json:"employee_code" binding:"required,max=40"`
	DisplayName      string `json:"display_name" binding:"required,max=150"`
	JobCategory      string `json:"job_category" binding:"required,oneof=doctor nurse pharmacist lab_technician receptionist administration other"`
	EmploymentStatus string `json:"employment_status" binding:"required,oneof=active inactive"`
}

var employeeCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

func decode(c *gin.Context, into any) bool {
	return decodeSized(c, into, 8192)
}

func decodeSized(c *gin.Context, into any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid request body")
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "expected one JSON object")
		return false
	}
	if err := binding.Validator.ValidateStruct(into); err != nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid request fields")
		return false
	}
	return true
}

func staffPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, tenant, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, tenant, false
	}
	if expected := c.Request.Header.Values("X-Helivanta-Expected-Subject"); len(expected) > 0 && (len(expected) != 1 || expected[0] != p.Subject) {
		respond.Error(c, 409, "session_changed", "Your signed-in account changed. Refresh before continuing.")
		return p, tenant, false
	}
	return p, tenant, true
}

func scope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "hospital")
		return p, uuid.Nil, false
	}
	return p, hospital, true
}

func profileScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	return scope(c)
}

func (h *handlers) transaction(ctx context.Context, p authn.Principal, hospital uuid.UUID, fn func(*gorm.DB) error) error {
	return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject', ?, true), set_config('app.workforce_hospital', ?, true)`, p.Subject, hospital.String()).Error; err != nil {
			return err
		}
		if h.db.GuardedCommandActive(ctx) {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
				return err
			}
		}
		return fn(tx)
	})
}

func requireStaffAccess(tx *gorm.DB, tenant string, hospital uuid.UUID, writing bool) error {
	var allowed bool
	if err := tx.Raw(`SELECT COALESCE(workforce_staff_allowed(?::uuid, ?::uuid, ?), false)`, tenant, hospital, writing).Scan(&allowed).Error; err != nil {
		return err
	}
	if !allowed {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func respondError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "resource")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			respond.Conflict(c, "employee code already exists in this hospital")
			return
		case "23503", "42501":
			respond.NotFound(c, "resource")
			return
		case "23514", "23502":
			respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid request fields")
			return
		}
	}
	// PostgreSQL error details can contain staff names and employee codes.
	respond.InternalErr(c, errors.New("workforce database operation failed"), "could not process workforce request")
}

func (h *handlers) putHospital(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name" binding:"required,max=150"`
	}
	if !decode(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO workforce_hospitals (tenant_id, id, name) VALUES (?, ?, ?)
		 ON CONFLICT (tenant_id, id) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		 WHERE workforce_hospitals.establishment_kind = 'hospital' AND workforce_hospitals.name IS DISTINCT FROM EXCLUDED.name`, p.TenantID, hospital, in.Name).Error; err != nil {
			return err
		}
		// A pharmacy registered after the route admission keeps its row; this command must not claim it as a hospital.
		var kind string
		if err := tx.Raw(`SELECT establishment_kind FROM workforce_hospitals WHERE tenant_id = ? AND id = ?`, p.TenantID, hospital).Scan(&kind).Error; err != nil {
			return err
		}
		if kind != hospitalEstablishmentKind {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, gin.H{"id": hospital, "name": in.Name})
}

func (h *handlers) putAccess(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	if subject, level, ok := h.putEstablishmentAccess(c, p, establishment{hospitalEstablishmentKind, hospital}); ok {
		respond.OK(c, gin.H{"hospital_id": hospital, "subject": subject, "level": level})
	}
}

func (h *handlers) putEstablishmentAccess(c *gin.Context, p authn.Principal, e establishment) (string, string, bool) {
	var in struct {
		Level string `json:"level" binding:"required,oneof=reader manager"`
	}
	if !decode(c, &in) {
		return "", "", false
	}
	subject := c.Param("subject")
	if len(subject) > 200 || strings.TrimSpace(subject) == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid subject")
		return "", "", false
	}
	err := h.transaction(c.Request.Context(), p, e.id, func(tx *gorm.DB) error {
		if err := e.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, ?, ?)
		 ON CONFLICT (tenant_id, hospital_id, subject) DO UPDATE SET level = EXCLUDED.level, updated_at = now()
		 WHERE workforce_hospital_access.level IS DISTINCT FROM EXCLUDED.level`, p.TenantID, e.id, subject, in.Level).Error
	})
	if err != nil {
		respondError(c, err)
		return "", "", false
	}
	return subject, in.Level, true
}

func (h *handlers) deleteAccess(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	h.deleteEstablishmentAccess(c, p, establishment{hospitalEstablishmentKind, hospital})
}

func (h *handlers) deleteEstablishmentAccess(c *gin.Context, p authn.Principal, e establishment) {
	err := h.transaction(c.Request.Context(), p, e.id, func(tx *gorm.DB) error {
		if err := e.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM workforce_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ?`, p.TenantID, e.id, c.Param("subject")).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

const staffColumns = "id, hospital_id, employee_code, display_name, job_category, employment_status, created_at, updated_at"

func (h *handlers) putStaff(c *gin.Context) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return
	}
	projection, ok := directoryContext(c)
	if !ok {
		return
	}
	if row, ok := h.putDirectoryStaff(c, p, establishment{hospitalEstablishmentKind, hospital}, projection); ok {
		respondDirectoryStaff(c, row, projection)
	}
}

func (h *handlers) putDirectoryStaff(c *gin.Context, p authn.Principal, e establishment, projection bool) (staff, bool) {
	var row staff
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return row, false
	}
	var in staffDirectoryInput
	if !decode(c, &in) {
		return row, false
	}
	if (projection && (in.ExpectedVersion == nil || in.DirectoryContextVersion != staffDirectoryContextVersion)) || (!projection && in.metadataSupplied) {
		respond.Error(c, 400, "invalid_request", "This Staff directory request is not supported. Refresh and retry.")
		return row, false
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !employeeCode.MatchString(in.EmployeeCode) || in.DisplayName == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid staff fields")
		return row, false
	}
	err = h.transaction(c.Request.Context(), p, e.id, func(tx *gorm.DB) error {
		if err := e.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		if err := lockDirectoryManager(tx, p, e.id); err != nil {
			return err
		}
		if projection {
			if err := checkDirectoryVersion(tx, p, e.id, id, in); err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_staff (tenant_id, hospital_id, id, employee_code, display_name, job_category, employment_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, hospital_id, id) DO UPDATE SET employee_code = EXCLUDED.employee_code,
		 display_name = EXCLUDED.display_name, job_category = EXCLUDED.job_category, employment_status = EXCLUDED.employment_status, updated_at = now()
		 WHERE (workforce_staff.employee_code, workforce_staff.display_name, workforce_staff.job_category, workforce_staff.employment_status)
		 IS DISTINCT FROM (EXCLUDED.employee_code, EXCLUDED.display_name, EXCLUDED.job_category, EXCLUDED.employment_status)`,
			p.TenantID, e.id, id, in.EmployeeCode, in.DisplayName, in.JobCategory, in.EmploymentStatus).Error; err != nil {
			return err
		}
		if in.EmploymentStatus == "inactive" {
			if err := h.revokeInactiveStaffLinks(tx, p, e.id, id); err != nil {
				return err
			}
		}
		return tx.Select(staffColumns+", version").Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, e.id, id).First(&row).Error
	})
	if errors.Is(err, errStaffDirectoryVersion) {
		respond.Error(c, 409, "staff_version_conflict", "This Staff record changed. Your draft has been kept. Reload the current record before saving.")
		return row, false
	}
	if err != nil {
		respondError(c, err)
		return row, false
	}
	return row, true
}

func (h *handlers) getStaff(c *gin.Context) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return
	}
	projection, ok := directoryContext(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "staff")
		return
	}
	var row staff
	err = h.intakeReadTransaction(c, p, hospital, func(tx *gorm.DB) error {
		return readDirectoryStaff(tx, p.TenantID, establishment{hospitalEstablishmentKind, hospital}, id, &row)
	})
	if err != nil {
		respondIntakeError(c, err)
		return
	}
	respondDirectoryStaff(c, row, projection)
}

func readDirectoryStaff(tx *gorm.DB, tenant string, e establishment, id uuid.UUID, row *staff) error {
	return e.staffRows(tx, tenant).Select(staffColumns+", version").Where("id = ?", id).First(row).Error
}

func (h *handlers) listStaff(c *gin.Context, params pagination.Params) ([]staff, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil {
		return nil, nil
	}
	var rows []staff
	err = h.intakeReadTransaction(c, p, hospital, func(tx *gorm.DB) error {
		return listDirectoryStaff(tx, p.TenantID, establishment{hospitalEstablishmentKind, hospital}, params, &rows)
	})
	if err != nil {
		if _, projected := c.Request.URL.Query()[hrIntakeQuery]; projected || errors.Is(err, gorm.ErrRecordNotFound) {
			respondIntakeError(c, err)
			return nil, nil
		}
		return nil, errors.New("workforce list operation failed")
	}
	return rows, nil
}

func listDirectoryStaff(tx *gorm.DB, tenant string, e establishment, params pagination.Params, rows *[]staff) error {
	if err := e.refuseOtherKind(tx, tenant); err != nil {
		return err
	}
	return e.staffRows(tenantdb.ApplyKeyset(tx, params), tenant).Select(staffColumns).Find(rows).Error
}
