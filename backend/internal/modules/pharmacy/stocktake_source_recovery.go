package pharmacy

import (
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func (u stocktakeWorkUse) requireOriginalHospitalStocktakeSource(tx *gorm.DB, take uuid.UUID) error {
	if u.hospital == nil {
		return nil
	}
	var matches int64
	if err := tx.Table("pharmacy_stocktake_hospital_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='start' AND hospital_id=? AND department_id=? AND binding_id=? AND binding_version=? AND department_revision_id=?", u.tenant, u.outlet, take, take, u.hospital.hospital, u.hospital.department, u.hospital.binding, u.hospital.bindingVersion, u.hospital.revision).Count(&matches).Error; err != nil {
		return err
	}
	if matches != 1 {
		return authz.ErrDepartmentReferenceChanged
	}
	var invalidated int64
	if err := tx.Table("pharmacy_stocktake_source_invalidations").Where("tenant_id=? AND outlet_id=? AND stocktake_id=?", u.tenant, u.outlet, take).Count(&invalidated).Error; err != nil {
		return err
	}
	if invalidated != 0 {
		return authz.ErrDepartmentReferenceChanged
	}
	return nil
}

func (h *catalogueHandlers) invalidatePriorHospitalStocktake(tx *gorm.DB, p authn.Principal, use stocktakeWorkUse, replacement uuid.UUID) error {
	if use.hospital == nil {
		return nil
	}
	var active []struct{ ID uuid.UUID }
	if err := tx.Raw(`SELECT s.id FROM pharmacy_stocktakes s WHERE s.tenant_id=? AND s.outlet_id=? AND s.sealed AND s.authority_kind='hospital'
 AND NOT EXISTS(SELECT 1 FROM pharmacy_stocktake_source_invalidations i WHERE i.tenant_id=s.tenant_id AND i.outlet_id=s.outlet_id AND i.stocktake_id=s.id)
 AND COALESCE((SELECT c.state FROM pharmacy_stocktake_commands c WHERE c.tenant_id=s.tenant_id AND c.outlet_id=s.outlet_id AND c.stocktake_id=s.id ORDER BY c.version DESC LIMIT 1),'counting')IN('counting','submitted')
 ORDER BY s.id LIMIT 2 FOR UPDATE`, p.TenantID, use.outlet).Scan(&active).Error; err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}
	if len(active) != 1 {
		return errStocktakeChanged
	}
	var original struct{ BindingID uuid.UUID }
	if err := tx.Table("pharmacy_stocktake_hospital_evidence").Select("binding_id").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='start'", p.TenantID, use.outlet, active[0].ID, active[0].ID).Take(&original).Error; err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if original.BindingID == use.hospital.binding {
		return nil
	}
	if err := tx.Exec(`INSERT INTO pharmacy_stocktake_source_invalidations(tenant_id,outlet_id,stocktake_id,replacement_stocktake_id,original_binding_id,replacement_binding_id,actor_subject)VALUES(?,?,?,?,?,?,?)`, p.TenantID, use.outlet, active[0].ID, replacement, original.BindingID, use.hospital.binding, p.Subject).Error; err != nil {
		return err
	}
	return h.audit(tx, p, "pharmacy.stocktake.source_invalidated", "pharmacy_stocktakes", active[0].ID,
		map[string]any{"binding_id": original.BindingID}, map[string]any{"replacement_binding_id": use.hospital.binding, "replacement_stocktake_id": replacement})
}
