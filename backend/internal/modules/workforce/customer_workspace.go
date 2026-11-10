package workforce

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
)

// NewCustomerWorkspaceInitializer creates only this owner's initial business resource.
func NewCustomerWorkspaceInitializer(deps platform.Deps) func(context.Context, customercontract.Workspace) error {
	return func(ctx context.Context, in customercontract.Workspace) error {
		if deps.DB == nil || !deps.DB.GuardedCommandActive(ctx) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if in.Validate() != nil {
			return customercontract.ErrInvalid
		}
		resource := uuid.MustParse(in.ResourceID)
		return deps.DB.WithTenantCommand(ctx, in.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "workforce-customer:"+in.TenantID+":"+in.ResourceID).Error; err != nil {
				return err
			}
			var initialized bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM audit_events WHERE tenant_id=? AND resource_type='workforce_workspace_onboarding' AND resource_id=? AND action='workspace_initialized')`, in.TenantID, resource).Scan(&initialized).Error; err != nil {
				return err
			}
			if initialized {
				var count int64
				if err := tx.Table("workforce_hospitals").Where("tenant_id=? AND id=? AND establishment_kind=?", in.TenantID, resource, in.Kind).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return gorm.ErrRecordNotFound
				}
				return nil
			}
			name, ownership := in.Name, ""
			if in.Kind == customercontract.Pharmacy {
				if deps.PharmacyEmployers == nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				source, err := deps.PharmacyEmployers.CurrentPharmacyEmployer(ctx, in.TenantID, resource)
				if err != nil {
					return err
				}
				if source.PharmacyID != resource || source.OwnershipType != "independent" {
					return authz.ErrPharmacyEmployerHidden
				}
				name, ownership = source.Name, source.OwnershipType
			}
			if err := tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,?,?,NULLIF(?,''))`, in.TenantID, resource, name, in.Kind, ownership).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,?,'manager')`, in.TenantID, resource, in.OwnerSubject).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'system:serviceaccount:onboarding:onboarding-api','workspace_initialized','workforce_workspace_onboarding',?)`, in.TenantID, resource).Error
		})
	}
}
