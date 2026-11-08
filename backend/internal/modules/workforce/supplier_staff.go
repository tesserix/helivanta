package workforce

import (
	"context"
	_ "embed"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

const supplierEstablishmentKind = "supplier"
const PermSupplierManage authz.Permission = "workforce.supplier.manage"

//go:embed supplier_staff.sql
var supplierStaffSchema string

type supplierEstablishment uuid.UUID

func (s supplierEstablishment) establishment() establishment {
	return establishment{kind: supplierEstablishmentKind, id: uuid.UUID(s)}
}
func (s SupplierStaff) PageKey() (time.Time, uuid.UUID)       { return s.CreatedAt, s.ID }
func (s SupplierStaffSource) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.SupplierID }

func registerSupplierStaffRoutes(r *platform.Router, h *handlers) {
	family := r.Group("/workforce/suppliers")
	suppliers := family.Establishment(supplierEstablishmentKind, supplierEstablishmentKind, h.establishmentAdmission(supplierEstablishmentKind))
	platform.ListRoute(family, "", authz.Public, h.listSupplierStaffSources)
	suppliers.PUT("/:supplier", PermSupplierManage, h.putSupplierEmployer)
	suppliers.GET("/:supplier", PermSupplierManage, h.getSupplierEmployer)
	suppliers.PUT("/:supplier/access/:subject", PermSupplierManage, h.putSupplierAccess)
	suppliers.DELETE("/:supplier/access/:subject", PermSupplierManage, h.deleteSupplierAccess)
	platform.ListRoute(suppliers, "/:supplier/staff", authz.Public, h.listSupplierStaff)
	suppliers.PUT("/:supplier/staff/:id", authz.Public, h.putSupplierStaff)
	suppliers.GET("/:supplier/staff/:id", authz.Public, h.getSupplierStaff)
}
func supplierScope(c *gin.Context) (authn.Principal, supplierEstablishment, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, supplierEstablishment{}, false
	}
	p, _, ok := staffPrincipal(c)
	if !ok {
		return p, supplierEstablishment{}, false
	}
	id, err := uuid.Parse(c.Param("supplier"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "supplier")
		return p, supplierEstablishment{}, false
	}
	return p, supplierEstablishment(id), true
}
func supplierDirectoryScope(c *gin.Context) (authn.Principal, supplierEstablishment, bool) {
	p, root, ok := supplierScope(c)
	if !ok {
		return p, root, false
	}
	projection, ok := directoryContext(c)
	if !ok {
		return p, root, false
	}
	if !projection {
		respond.Error(c, 400, "invalid_request", "Use the current Staff directory view.")
		return p, root, false
	}
	if _, present := c.Request.URL.Query()[hrIntakeQuery]; present {
		respond.Error(c, 400, "invalid_request", "This Staff intake view is unavailable.")
		return p, root, false
	}
	return p, root, true
}
func readSupplierEmployer(tx *gorm.DB, tenant string, root supplierEstablishment) (SupplierEmployer, error) {
	var result SupplierEmployer
	query := tx.Raw(`SELECT id AS supplier_id,name,supplier_source_version AS source_version,created_at FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind='supplier'`, tenant, uuid.UUID(root)).Scan(&result)
	if query.Error == nil && query.RowsAffected == 0 {
		return result, gorm.ErrRecordNotFound
	}
	return result, query.Error
}
func (h *handlers) putSupplierEmployer(c *gin.Context) {
	p, root, ok := supplierScope(c)
	if !ok {
		return
	}
	var in struct{}
	if !decode(c, &in) {
		return
	}
	if h.supplierEmployers == nil || h.db == nil {
		respond.Error(c, 503, "authz_unavailable", "Supplier employer authority is unavailable.")
		return
	}
	var result SupplierEmployer
	err := h.db.WithGuardedTenantCommand(c.Request.Context(), p.TenantID, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		source, err := h.supplierEmployers.CurrentSupplierEmployer(ctx, p.TenantID, uuid.UUID(root))
		if err != nil {
			return err
		}
		return h.transaction(ctx, p, uuid.UUID(root), func(tx *gorm.DB) error {
			inserted := tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,supplier_source_version)VALUES(?,?,?,'supplier',?)ON CONFLICT(tenant_id,id)DO NOTHING`, p.TenantID, source.SupplierID, source.Name, source.SourceVersion)
			if inserted.Error != nil {
				return inserted.Error
			}
			if err := root.establishment().requireKind(tx, p.TenantID); err != nil {
				return err
			}
			if inserted.RowsAffected == 1 {
				if err := tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level)VALUES(?,?,?,'manager')`, p.TenantID, source.SupplierID, p.Subject).Error; err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.supplier.registered',?,jsonb_build_object('source_version',?::bigint))`, p.TenantID, source.SupplierID, p.Subject, source.SupplierID, source.SourceVersion).Error; err != nil {
					return err
				}
			}
			result, err = readSupplierEmployer(tx, p.TenantID, root)
			return err
		})
	})
	if errors.Is(err, authz.ErrSupplierEmployerHidden) || errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "supplier")
		return
	}
	if err != nil {
		respond.Error(c, 503, "employer_unavailable", "Supplier employer registration could not be confirmed. Recover the same supplier registration.")
		return
	}
	respond.OK(c, result)
}
func (h *handlers) getSupplierEmployer(c *gin.Context) {
	p, root, ok := supplierScope(c)
	if !ok {
		return
	}
	var result SupplierEmployer
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		result, err = readSupplierEmployer(tx, p.TenantID, root)
		return err
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, result)
}
func supplierStaffView(row staff) SupplierStaff {
	return SupplierStaff{ID: row.ID, SupplierID: row.HospitalID, EmployeeCode: row.EmployeeCode, DisplayName: row.DisplayName, JobCategory: row.JobCategory, EmploymentStatus: row.EmploymentStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func respondSupplierStaff(c *gin.Context, row staff) {
	respond.OK(c, struct {
		SupplierStaff
		staffDirectoryReadMetadata
	}{supplierStaffView(row), staffDirectoryReadMetadata{Version: row.Version, DirectoryContextVersion: staffDirectoryContextVersion}})
}
func (h *handlers) putSupplierStaff(c *gin.Context) {
	p, root, ok := supplierDirectoryScope(c)
	if !ok {
		return
	}
	if row, ok := h.putDirectoryStaff(c, p, root.establishment(), true); ok {
		respondSupplierStaff(c, row)
	}
}
func (h *handlers) getSupplierStaff(c *gin.Context) {
	p, root, ok := supplierDirectoryScope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var row staff
	err = h.transaction(c.Request.Context(), p, uuid.UUID(root), func(tx *gorm.DB) error { return readDirectoryStaff(tx, p.TenantID, root.establishment(), id, &row) })
	if err != nil {
		respondError(c, err)
		return
	}
	respondSupplierStaff(c, row)
}
func (h *handlers) listSupplierStaff(c *gin.Context, page pagination.Params) ([]SupplierStaff, error) {
	p, root, ok := supplierScope(c)
	if !ok {
		return nil, nil
	}
	if _, present := c.Request.URL.Query()[hrIntakeQuery]; present {
		respond.Error(c, 400, "invalid_request", "This Staff intake view is unavailable.")
		return nil, nil
	}
	var rows []staff
	err := h.transaction(c.Request.Context(), p, uuid.UUID(root), func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, uuid.UUID(root), false); err != nil {
			return err
		}
		return listDirectoryStaff(tx, p.TenantID, root.establishment(), page, &rows)
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	result := make([]SupplierStaff, 0, len(rows))
	for _, row := range rows {
		result = append(result, supplierStaffView(row))
	}
	return result, nil
}
func (h *handlers) listSupplierStaffSources(c *gin.Context, page pagination.Params) ([]SupplierStaffSource, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	var result []SupplierStaffSource
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx.Table("workforce_hospitals"), page).Select(`id AS supplier_id,name,created_at,(SELECT level FROM workforce_hospital_access a WHERE a.tenant_id=workforce_hospitals.tenant_id AND a.hospital_id=workforce_hospitals.id AND a.subject=?) AS access_level`, p.Subject).Where(`tenant_id=? AND establishment_kind='supplier' AND EXISTS(SELECT 1 FROM workforce_hospital_access a WHERE a.tenant_id=workforce_hospitals.tenant_id AND a.hospital_id=workforce_hospitals.id AND a.subject=?)`, p.TenantID, p.Subject).Scan(&result).Error
	})
	if err != nil {
		return nil, errors.New("supplier Staff discovery unavailable")
	}
	return result, nil
}
func (h *handlers) putSupplierAccess(c *gin.Context) {
	p, root, ok := supplierScope(c)
	if !ok {
		return
	}
	if !validLoginSubject(c.Param("subject")) {
		respond.Error(c, 400, "invalid_request", "Choose a valid account subject.")
		return
	}
	if subject, level, ok := h.putEstablishmentAccess(c, p, root.establishment()); ok {
		respond.OK(c, SupplierAccess{SupplierID: uuid.UUID(root), Subject: subject, Level: level})
	}
}
func (h *handlers) deleteSupplierAccess(c *gin.Context) {
	p, root, ok := supplierScope(c)
	if !ok {
		return
	}
	h.deleteEstablishmentAccess(c, p, root.establishment())
}
