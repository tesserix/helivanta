package workforce_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// grantPharmacyAccess simulates a later slice lifting the access guard, so only the route admission keeps the hospital family closed.
func (h *establishmentHarness) grantPharmacyAccess(t *testing.T, pharmacy string) {
	t.Helper()
	require.NoError(t, h.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_lifted_access_" + pharmacy, SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true),set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital','` + pharmacy + `',true);
 ALTER TABLE workforce_hospital_access DISABLE TRIGGER workforce_hospital_establishment;
 INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES('` + testutil.TenantA + `','` + pharmacy + `','user-admin','manager');
 ALTER TABLE workforce_hospital_access ENABLE TRIGGER workforce_hospital_establishment;`}}))
}

func (h *establishmentHarness) accessRows(t *testing.T, establishment string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, h.tx(t, establishment, func(tx *gorm.DB) error {
		return tx.Table("workforce_hospital_access").Where("tenant_id=? AND hospital_id=?", testutil.TenantA, establishment).Count(&n).Error
	}))
	return n
}

func TestHospitalRoutesRefuseAPharmacyEstablishment(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := h.pharmacy(t, "independent")
	h.grantPharmacyAccess(t, pharmacy)
	for _, call := range []struct{ method, path, body string }{
		{"PUT", "/v1/workforce/hospitals/" + pharmacy, `{"name":"Synthetic Employer Pharmacy"}`},
		{"PUT", "/v1/workforce/hospitals/" + pharmacy + "/access/user-other", `{"level":"reader"}`},
		{"DELETE", "/v1/workforce/hospitals/" + pharmacy + "/access/user-admin", ""},
		{"GET", "/v1/workforce/hospitals/" + pharmacy + "/staff", ""},
	} {
		t.Run(call.method+" "+call.path, func(t *testing.T) {
			response := testutil.Do(h.router, call.method, call.path, "admin", call.body)
			require.Equal(t, 404, response.Code, response.Body.String())
			require.JSONEq(t, `{"error":"not_found","message":"hospital not found"}`, response.Body.String())
		})
	}
	require.EqualValues(t, 1, h.accessRows(t, pharmacy), "a refused hospital command must not change the pharmacy's grants")
}

func TestHospitalListExcludesPharmacyEstablishments(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	h.grantPharmacyAccess(t, h.pharmacy(t, "chain"))
	response := testutil.Do(h.router, "GET", "/v1/workforce/hospitals", "admin", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, h.hospital, page.Items[0].ID)
}

func TestPharmacyRoutesRefuseAHospitalEstablishment(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	for _, method := range []string{"PUT", "GET"} {
		response := testutil.Do(h.router, method, "/v1/workforce/pharmacies/"+h.hospital, "admin", "{}")
		require.Equal(t, 404, response.Code, response.Body.String())
		require.JSONEq(t, `{"error":"not_found","message":"pharmacy not found"}`, response.Body.String())
	}
}

func TestPharmacyRegistrationFailsClosedWithoutTheEmployerOwner(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	pharmacy := uuid.Must(uuid.NewV7()).String()
	response := testutil.Do(h.router, "PUT", "/v1/workforce/pharmacies/"+pharmacy, "admin", "{}")
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var n int64
	require.NoError(t, h.tx(t, pharmacy, func(tx *gorm.DB) error {
		return tx.Table("workforce_hospitals").Where("tenant_id=? AND id=?", testutil.TenantA, pharmacy).Count(&n).Error
	}))
	require.Zero(t, n)
}

func TestEstablishmentAdmissionFailsClosedWhenTheRegistryIsUnavailable(t *testing.T) {
	h := newEstablishmentHarness(t, workforce.New())
	require.NoError(t, h.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registry_unavailable", SQL: `ALTER TABLE workforce_hospitals RENAME COLUMN establishment_kind TO establishment_kind_unavailable`}}))
	for _, path := range []string{"/v1/workforce/hospitals/" + h.hospital + "/staff", "/v1/workforce/pharmacies/" + uuid.Must(uuid.NewV7()).String()} {
		response := testutil.Do(h.router, "GET", path, "admin", "")
		require.Equal(t, 503, response.Code, response.Body.String())
		require.JSONEq(t, `{"error":"establishment_unavailable","message":"The establishment registry is unavailable. Retry shortly."}`, response.Body.String())
	}
}
