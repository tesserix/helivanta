package pharmacy_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"testing"
)

func TestPharmacyBusinessProfileReadIsManagerAndTenantScoped(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacy.New()}, Tokens: map[string]string{"admin": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {pharmacy.PermAccessManage}, "foreign": {pharmacy.PermAccessManage}}})
	id := uuid.Must(uuid.NewV7()).String()
	root := "/v1/pharmacy/pharmacies/" + id
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "admin", `{"code":"TEST-PROFILE","name":"TEST Pharmacy","ownership_type":"independent"}`).Code)
	result := testutil.Do(r, "GET", root+"/business-profile", "admin", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	require.JSONEq(t, `{"profile":null}`, result.Body.String())

	address := customercontract.BusinessAddress{Line1: "10 Headquarters Road", Line2: "", City: "Pune", District: "Pune", PostalCode: "411001", StateCode: customercontract.Maharashtra, CountryCode: customercontract.India}
	profile := customercontract.OrganizationProfile{Contact: customercontract.BusinessContact{Phone: "+919876543210", Email: "office@example.test"}, HeadOffice: address, MainBranch: customercontract.MainBranch{Name: "Test Main Branch", Address: address}}
	profile.MainBranch.Address.Line1 = "20 Branch Road"
	raw, err := json.Marshal(profile)
	require.NoError(t, err)
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "synthetic-profile-fixture"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE pharmacy_organizations SET onboarding_profile=?::jsonb WHERE tenant_id=? AND id=?", string(raw), testutil.TenantA, id).Error
	}))
	result = testutil.Do(r, "GET", root+"/business-profile", "admin", "")
	require.Equal(t, 200, result.Code, result.Body.String())
	var view customercontract.BusinessProfileView
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &view))
	require.NotNil(t, view.Profile)
	require.Equal(t, profile, *view.Profile)
	require.Equal(t, 404, testutil.Do(r, "GET", root+"/business-profile", "foreign", "").Code)
	require.Equal(t, 403, testutil.Do(r, "GET", root+"/business-profile", "denied", "").Code)
}
