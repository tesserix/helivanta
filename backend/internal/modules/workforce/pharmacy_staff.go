package workforce

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (s PharmacyStaff) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

func (s PharmacyStaffSource) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.PharmacyID }

// listPharmacyStaffSources lists only pharmacies where the caller holds an access row; hospitals stay on their own list.
func (h *handlers) listPharmacyStaffSources(c *gin.Context, params pagination.Params) ([]PharmacyStaffSource, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []PharmacyStaffSource
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx.Table("workforce_hospitals"), params).Select(`id AS pharmacy_id, name, created_at,
		 (SELECT level FROM workforce_hospital_access a WHERE a.tenant_id = workforce_hospitals.tenant_id
		 AND a.hospital_id = workforce_hospitals.id AND a.subject = ?) AS access_level`, p.Subject).
			Where(`tenant_id = ? AND establishment_kind = ? AND EXISTS (SELECT 1 FROM workforce_hospital_access a
		 WHERE a.tenant_id = workforce_hospitals.tenant_id AND a.hospital_id = workforce_hospitals.id AND a.subject = ?)`, p.TenantID, pharmacyEstablishmentKind, p.Subject).
			Scan(&rows).Error
	})
	if err != nil {
		return nil, errors.New("pharmacy discovery failed")
	}
	return rows, nil
}

func pharmacyStaffView(row staff) PharmacyStaff {
	return PharmacyStaff{ID: row.ID, PharmacyID: row.HospitalID, EmployeeCode: row.EmployeeCode, DisplayName: row.DisplayName, JobCategory: row.JobCategory, EmploymentStatus: row.EmploymentStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func respondPharmacyStaff(c *gin.Context, row staff) {
	respond.OK(c, struct {
		PharmacyStaff
		staffDirectoryReadMetadata
	}{pharmacyStaffView(row), staffDirectoryReadMetadata{Version: row.Version, DirectoryContextVersion: staffDirectoryContextVersion}})
}

// pharmacyStaffScope refuses the HR intake view, whose IAM projection is hospital-only until pharmacy roots exist.
func pharmacyStaffScope(c *gin.Context) (authn.Principal, pharmacyEstablishment, bool) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return p, pharmacyEstablishment{}, false
	}
	if _, intake := c.Request.URL.Query()[hrIntakeQuery]; intake {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This Staff intake view is not supported. Refresh and retry.")
		return p, pharmacyEstablishment{}, false
	}
	return pharmacyScope(c)
}

// pharmacyDirectoryScope requires the versioned directory view, because the pharmacy family has no unversioned clients.
func pharmacyDirectoryScope(c *gin.Context) (authn.Principal, pharmacyEstablishment, bool) {
	p, pharmacy, ok := pharmacyStaffScope(c)
	if !ok {
		return p, pharmacy, false
	}
	projection, ok := directoryContext(c)
	if !ok {
		return p, pharmacy, false
	}
	if !projection {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "This Staff directory view is not supported. Refresh and retry.")
		return p, pharmacy, false
	}
	return p, pharmacy, true
}

func (h *handlers) putPharmacyStaff(c *gin.Context) {
	p, pharmacy, ok := pharmacyDirectoryScope(c)
	if !ok {
		return
	}
	if row, ok := h.putDirectoryStaff(c, p, pharmacy.establishment(), true); ok {
		respondPharmacyStaff(c, row)
	}
}

func (h *handlers) getPharmacyStaff(c *gin.Context) {
	p, pharmacy, ok := pharmacyDirectoryScope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "staff")
		return
	}
	var row staff
	err = h.transaction(c.Request.Context(), p, uuid.UUID(pharmacy), func(tx *gorm.DB) error {
		return readDirectoryStaff(tx, p.TenantID, pharmacy.establishment(), id, &row)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respondPharmacyStaff(c, row)
}

func (h *handlers) listPharmacyStaff(c *gin.Context, params pagination.Params) ([]PharmacyStaff, error) {
	p, pharmacy, ok := pharmacyStaffScope(c)
	if !ok {
		return nil, nil
	}
	var rows []staff
	err := h.transaction(c.Request.Context(), p, uuid.UUID(pharmacy), func(tx *gorm.DB) error {
		// Without an access row the page would be empty, which the Pharma screen must not mistake for an empty directory.
		if err := requireStaffAccess(tx, p.TenantID, uuid.UUID(pharmacy), false); err != nil {
			return err
		}
		return listDirectoryStaff(tx, p.TenantID, pharmacy.establishment(), params, &rows)
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	views := make([]PharmacyStaff, len(rows))
	for i, row := range rows {
		views[i] = pharmacyStaffView(row)
	}
	return views, nil
}

func (h *handlers) putPharmacyAccess(c *gin.Context) {
	p, pharmacy, ok := pharmacyScope(c)
	if !ok {
		return
	}
	if subject, level, ok := h.putEstablishmentAccess(c, p, pharmacy.establishment()); ok {
		respond.OK(c, PharmacyAccess{PharmacyID: uuid.UUID(pharmacy), Subject: subject, Level: level})
	}
}

func (h *handlers) deletePharmacyAccess(c *gin.Context) {
	p, pharmacy, ok := pharmacyScope(c)
	if !ok {
		return
	}
	h.deleteEstablishmentAccess(c, p, pharmacy.establishment())
}
