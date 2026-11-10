package customeronboarding_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"       //nolint:depguard // composition test exercises the owner boundary
	"github.com/tesserix/helivanta/internal/modules/pharmacy"  //nolint:depguard // composition test exercises the owner boundary
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // composition test exercises the owner boundary
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"testing"
)

func TestInitialWorkspaceCommandsFailClosedWithoutTheirOwnerGuard(t *testing.T) {
	tenant := uuid.NewString()
	in := customercontract.Workspace{TenantID: tenant, OrganizationID: "helivanta-" + tenant, OwnerSubject: "owner-" + tenant, ResourceID: uuid.Must(uuid.NewV7()).String(), Setup: customercontract.Setup{Name: "TEST Hospital", Kind: customercontract.Hospital, CountryCode: customercontract.India, StateCode: customercontract.Odisha}}
	called := false
	err := iam.NewCustomerWorkspaceAuthority(platform.Deps{})(t.Context(), in, func(context.Context) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called)
	require.Error(t, workforce.NewCustomerWorkspaceInitializer(platform.Deps{})(t.Context(), in))
	in.Kind = customercontract.Pharmacy
	require.Error(t, pharmacy.NewCustomerWorkspaceInitializer(platform.Deps{})(t.Context(), in))
}

func TestInitialCustomerWorkspaceCreatesOnlyOwnedResourcesAndNeverRestoresRevokedAccess(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(), pharmacy.New(), workforce.New()}})
	deps := platform.Deps{DB: db, PharmacyEmployers: pharmacy.New().PharmacyEmployers(db)}
	authority := iam.NewCustomerWorkspaceAuthority(deps)
	workforceInit := workforce.NewCustomerWorkspaceInitializer(deps)
	pharmacyInit := pharmacy.NewCustomerWorkspaceInitializer(deps)
	for _, kind := range []customercontract.Kind{customercontract.Hospital, customercontract.Pharmacy} {
		tenant := testutil.TenantA
		if kind == customercontract.Pharmacy {
			tenant = testutil.TenantB
		}
		subject := "owner-" + tenant
		in := customercontract.Workspace{TenantID: tenant, OrganizationID: "helivanta-" + tenant, OwnerSubject: subject, ResourceID: uuid.Must(uuid.NewV7()).String(), Setup: customercontract.Setup{Name: "TEST Workspace", Kind: kind, CountryCode: customercontract.India, StateCode: customercontract.Odisha}}
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,'tenant_admin')`, tenant, subject).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'system:serviceaccount:onboarding:onboarding-api','membership_verified','hospital_onboarding',?)`, tenant, tenant).Error
		}))
		initialize := func(current context.Context) error {
			if kind == customercontract.Pharmacy {
				if err := pharmacyInit(current, in); err != nil {
					return err
				}
			}
			return workforceInit(current, in)
		}
		require.NoError(t, authority(ctx, in, initialize))
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("workforce_hospitals").Where("tenant_id=? AND id=? AND establishment_kind=?", tenant, in.ResourceID, kind).Count(&count).Error; err != nil {
				return err
			}
			require.Equal(t, int64(1), count, "initial workspace is not registered by its owner")
			if err := tx.Table("workforce_hospital_access").Where("tenant_id=? AND hospital_id=? AND subject=? AND level='manager'", tenant, in.ResourceID, subject).Count(&count).Error; err != nil {
				return err
			}
			require.Equal(t, int64(1), count, "initial HR governance access missing")
			if kind == customercontract.Pharmacy {
				if err := tx.Table("pharmacy_organizations").Where("tenant_id=? AND id=? AND ownership_type='independent' AND onboarding_status='draft'", tenant, in.ResourceID).Count(&count).Error; err != nil {
					return err
				}
				require.Equal(t, int64(1), count, "pharmacy owner did not create an independent draft business")
				if err := tx.Table("workforce_hospitals").Where("tenant_id=? AND establishment_kind='hospital'", tenant).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count, "independent signup invented a hospital")
			}
			return tx.Exec(`DELETE FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=?`, tenant, in.ResourceID, subject).Error
		}))
		require.NoError(t, authority(ctx, in, initialize))
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table("workforce_hospital_access").Where("tenant_id=? AND hospital_id=? AND subject=?", tenant, in.ResourceID, subject).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, "replayed signup restored deliberately revoked access")
			return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=?`, tenant, subject).Error
		}))
		require.Error(t, authority(ctx, in, initialize), "revoked tenant owner still provisioned resources")
	}
}
