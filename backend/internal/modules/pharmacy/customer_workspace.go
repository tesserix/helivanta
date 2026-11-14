package pharmacy

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"reflect"
	"strings"
)

// NewCustomerWorkspaceInitializer creates only this owner's initial business resource.
func NewCustomerWorkspaceInitializer(deps platform.Deps) func(context.Context, customercontract.Workspace) error {
	return func(ctx context.Context, in customercontract.Workspace) error {
		if deps.DB == nil || !deps.DB.GuardedCommandActive(ctx) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if in.Validate() != nil || in.Kind != customercontract.Pharmacy {
			return customercontract.ErrInvalid
		}
		resource := uuid.MustParse(in.ResourceID)
		return deps.DB.WithTenantCommand(ctx, in.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "pharmacy-customer:"+in.TenantID+":"+in.ResourceID).Error; err != nil {
				return err
			}
			var initialized bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM audit_events WHERE tenant_id=? AND resource_type='pharmacy_workspace_onboarding' AND resource_id=? AND action='workspace_initialized')`, in.TenantID, resource).Scan(&initialized).Error; err != nil {
				return err
			}
			if initialized {
				var count int64
				if err := tx.Table("pharmacy_organizations").Where("tenant_id=? AND id=? AND ownership_type='independent'", in.TenantID, resource).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return gorm.ErrRecordNotFound
				}
				var saved pharmacyOrganization
				if err := tx.Select("id,onboarding_profile").Where("tenant_id=? AND id=?", in.TenantID, resource).Take(&saved).Error; err != nil {
					return err
				}
				if !reflect.DeepEqual(saved.OnboardingProfile, in.Profile) {
					return customercontract.ErrInvalid
				}
				if in.Profile != nil {
					var outlets int64
					if err := tx.Model(&pharmacyOutlet{}).Where("tenant_id=? AND pharmacy_id=? AND id=?", in.TenantID, resource, resource).Count(&outlets).Error; err != nil {
						return err
					}
					if outlets != 1 {
						return gorm.ErrRecordNotFound
					}
				}
				return nil
			}
			row := pharmacyOrganization{OnboardingProfile: in.Profile, ID: resource, TenantID: uuid.MustParse(in.TenantID), OrganizationFields: OrganizationFields{Code: "ONB_" + strings.ToUpper(strings.ReplaceAll(in.ResourceID, "-", "")), Name: in.Name, OwnershipType: "independent"}, OnboardingStatus: "draft"}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if in.Profile != nil {
				outlet := pharmacyOutlet{ID: resource, TenantID: uuid.MustParse(in.TenantID), PharmacyID: resource, OutletFields: OutletFields{Code: "MAIN", Name: in.Profile.MainBranch.Name, CountryCode: string(in.Profile.MainBranch.Address.CountryCode), Timezone: "Asia/Kolkata"}, RegulatoryStatus: "pending_review"}
				if err := tx.Create(&outlet).Error; err != nil {
					return err
				}
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'system:serviceaccount:onboarding:onboarding-api','workspace_initialized','pharmacy_workspace_onboarding',?)`, in.TenantID, resource).Error
		})
	}
}
