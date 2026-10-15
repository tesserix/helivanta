package pharmacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndependentPharmacyNeedsNoHospital(t *testing.T) {
	r, _, _, _ := catalogueSetup(t)
	root := "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010"
	w := do(r, "PUT", root, "editor", `{"code":"TEST-INDEPENDENT","name":"TEST Independent Pharmacy","ownership_type":"independent"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"ownership_type":"independent"`)
	require.NotContains(t, w.Body.String(), "hospital_id")
}

func TestPharmacyOutletsAndHospitalAffiliationsDoNotShareAccess(t *testing.T) {
	r, db := catalogueHarness(t)
	root := "/v1/pharmacy/pharmacies/"
	chain := "0199e000-0000-7000-8000-000000000011"
	hospitalOwned := "0199e000-0000-7000-8000-000000000012"
	secondHospitalOwned := "0199e000-0000-7000-8000-000000000013"
	siblingOutlet := "0199e000-0000-7000-8000-000000000002"
	for _, entry := range []struct{ id, code, ownership string }{{chain, "TEST-CHAIN", "chain"}, {hospitalOwned, "TEST-HOSPITAL-PHARMACY", "hospital_owned"}, {secondHospitalOwned, "TEST-HOSPITAL-PHARMACY-2", "hospital_owned"}} {
		body := fmt.Sprintf(`{"code":%q,"name":%q,"ownership_type":%q}`, entry.code, entry.code, entry.ownership)
		w := do(r, "PUT", root+entry.id, "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.JSONEq(t, w.Body.String(), do(r, "PUT", root+entry.id, "editor", body).Body.String())
	}
	outletBody := `{"code":"TEST-BRANCH","name":"TEST Branch","country_code":"IN","timezone":"Asia/Kolkata"}`
	w := do(r, "PUT", root+chain+"/outlets/"+siblingOutlet, "editor", outletBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, w.Body.String(), do(r, "PUT", root+chain+"/outlets/"+siblingOutlet, "editor", outletBody).Body.String())
	w = do(r, "GET", root+chain+"/outlets", "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), siblingOutlet)
	require.Equal(t, 403, do(r, "PUT", root+chain+"/outlets/"+siblingOutlet, "reader", outletBody).Code)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", productPath(id), "editor", productBody(0, "TEST Independent")).Code)
	wrongParent := strings.Replace(productPath(id), pharmaOrganization, chain, 1)
	require.Equal(t, 404, do(r, "GET", wrongParent, "editor", "").Code)
	require.Equal(t, 404, do(r, "PUT", root+chain+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", `{"level":"editor"}`).Code)
	require.Equal(t, 404, do(r, "GET", root+chain+"/outlets/"+siblingOutlet+"/access", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", root+chain+"/outlets", "foreign", "").Code)

	hospital := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, owner := range []string{hospitalOwned, secondHospitalOwned} {
		link := root + owner + "/hospital-affiliations/" + uuid.Must(uuid.NewV7()).String()
		body := fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, hospital)
		w = do(r, "PUT", link, "editor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"verification_status":"pending_validation"`)
		require.JSONEq(t, w.Body.String(), do(r, "PUT", link, "editor", body).Body.String())
		require.Equal(t, 403, do(r, "PUT", link, "reader", body).Code)
		require.Equal(t, 400, do(r, "PUT", link, "editor", strings.Replace(body, `"relationship":`, `"verification_status":"active","relationship":`, 1)).Code)
		w = do(r, "GET", root+owner+"/hospital-affiliations", "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), hospital)
		require.Equal(t, 404, do(r, "GET", root+owner+"/hospital-affiliations", "reader", "").Code)
	}
	// A reader with a grant in one independent outlet sees only that pharmacy.
	w = do(r, "GET", root[:len(root)-1], "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), pharmaOrganization)
	require.NotContains(t, w.Body.String(), hospitalOwned)
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true)`, chain).Error; err != nil {
			return err
		}
		var n int64
		err := tx.Table("pharmacy_catalogue_products").Count(&n).Error
		require.Zero(t, n)
		return err
	}))
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_outlets SET pharmacy_id=? WHERE id=?`, chain, pharmaOutlet).Error
	}))
}

func TestRegistryRejectsDuplicateIdentityInvalidTimezoneAndImplicitSharing(t *testing.T) {
	r, _ := catalogueHarness(t)
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization
	duplicate := `{"code":"test-pharmacy","name":"TEST Other Pharmacy","ownership_type":"independent"}`
	require.Equal(t, 409, do(r, "PUT", "/v1/pharmacy/pharmacies/"+uuid.Must(uuid.NewV7()).String(), "editor", duplicate).Code)
	require.Equal(t, 409, do(r, "PUT", root, "editor", duplicate).Code)
	for _, zone := range []string{"invalid/zone", "Local"} {
		body := fmt.Sprintf(`{"code":"TEST-NEW","name":"TEST New","country_code":"IN","timezone":%q}`, zone)
		require.Equal(t, 400, do(r, "PUT", root+"/outlets/"+uuid.Must(uuid.NewV7()).String(), "editor", body).Code)
	}
	require.Equal(t, 400, do(r, "PUT", root, "editor", `{"code":"TEST-X","name":"TEST","ownership_type":"independent","share_patients":true}`).Code)
	w := do(r, "GET", "/v1/pharmacy/pharmacies?limit=1", "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 404, do(r, "GET", "/v1/pharmacy/pharmacies/"+uuid.Must(uuid.NewV7()).String()+"/outlets", "editor", "").Code)
}
