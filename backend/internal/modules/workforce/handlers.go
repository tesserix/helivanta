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
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type handlers struct{ db *tenantdb.DB }

type staff struct {
	ID               uuid.UUID `json:"id"`
	HospitalID       uuid.UUID `json:"hospital_id"`
	EmployeeCode     string    `json:"employee_code"`
	DisplayName      string    `json:"display_name"`
	JobCategory      string    `json:"job_category"`
	EmploymentStatus string    `json:"employment_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
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

func scope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
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

func (h *handlers) transaction(ctx context.Context, p authn.Principal, hospital uuid.UUID, fn func(*gorm.DB) error) error {
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject', ?, true), set_config('app.workforce_hospital', ?, true)`, p.Subject, hospital.String()).Error; err != nil {
			return err
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
		return tx.Exec(`INSERT INTO workforce_hospitals (tenant_id, id, name) VALUES (?, ?, ?)
		 ON CONFLICT (tenant_id, id) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		 WHERE workforce_hospitals.name IS DISTINCT FROM EXCLUDED.name`, p.TenantID, hospital, in.Name).Error
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
	var in struct {
		Level string `json:"level" binding:"required,oneof=reader manager"`
	}
	if !decode(c, &in) {
		return
	}
	subject := c.Param("subject")
	if len(subject) > 200 || strings.TrimSpace(subject) == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid subject")
		return
	}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, ?, ?)
		 ON CONFLICT (tenant_id, hospital_id, subject) DO UPDATE SET level = EXCLUDED.level, updated_at = now()
		 WHERE workforce_hospital_access.level IS DISTINCT FROM EXCLUDED.level`, p.TenantID, hospital, subject, in.Level).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "subject": subject, "level": in.Level})
}

func (h *handlers) deleteAccess(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("workforce_hospitals").Where("tenant_id = ? AND id = ?", p.TenantID, hospital).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Exec(`DELETE FROM workforce_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ?`, p.TenantID, hospital, c.Param("subject")).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

const staffColumns = "id, hospital_id, employee_code, display_name, job_category, employment_status, created_at, updated_at"

func (h *handlers) putStaff(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var in staffInput
	if !decode(c, &in) {
		return
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !employeeCode.MatchString(in.EmployeeCode) || in.DisplayName == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid staff fields")
		return
	}
	var row staff
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, true); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_staff (tenant_id, hospital_id, id, employee_code, display_name, job_category, employment_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, hospital_id, id) DO UPDATE SET employee_code = EXCLUDED.employee_code,
		 display_name = EXCLUDED.display_name, job_category = EXCLUDED.job_category, employment_status = EXCLUDED.employment_status, updated_at = now()
		 WHERE (workforce_staff.employee_code, workforce_staff.display_name, workforce_staff.job_category, workforce_staff.employment_status)
		 IS DISTINCT FROM (EXCLUDED.employee_code, EXCLUDED.display_name, EXCLUDED.job_category, EXCLUDED.employment_status)`,
			p.TenantID, hospital, id, in.EmployeeCode, in.DisplayName, in.JobCategory, in.EmploymentStatus).Error; err != nil {
			return err
		}
		return tx.Select(staffColumns).Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&row).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) getStaff(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "staff")
		return
	}
	var row staff
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		return tx.Select(staffColumns).Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id).First(&row).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *handlers) listStaff(c *gin.Context, params pagination.Params) ([]staff, error) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil {
		return nil, nil
	}
	var rows []staff
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(staffColumns).Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		return nil, errors.New("workforce list operation failed")
	}
	return rows, nil
}
