package workforce_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHospitalStaffRequiresExplicitAccessAndRevocationIsImmediate(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "staff": testutil.TenantA, "other": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	path := "/v1/workforce/hospitals/" + hospital
	w := testutil.Do(r, http.MethodPut, path, "admin", `{"name":"Synthetic Hospital"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	staffPath := path + "/staff/" + uuid.NewString()
	body := `{"employee_code":"SYN-01","display_name":"Synthetic Staff","job_category":"nurse","employment_status":"active"}`
	w = testutil.Do(r, http.MethodPut, staffPath, "admin", body)
	require.Equal(t, http.StatusNotFound, w.Code, "tenant admin must not inherit staff access")
	w = testutil.Do(r, http.MethodPut, path+"/access/user-staff", "admin", `{"level":"manager"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = testutil.Do(r, http.MethodPut, staffPath, "staff", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.Equal(t, "SYN-01", created["employee_code"])
	for _, token := range []string{"admin", "other", ""} {
		w = testutil.Do(r, http.MethodGet, staffPath, token, "")
		if token == "" {
			require.Equal(t, http.StatusUnauthorized, w.Code)
		} else {
			require.Equal(t, http.StatusNotFound, w.Code)
		}
	}
	w = testutil.Do(r, http.MethodGet, staffPath, "staff", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = testutil.Do(r, http.MethodDelete, path+"/access/user-staff", "admin", "")
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	w = testutil.Do(r, http.MethodGet, staffPath, "staff", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPWithSignedSessionsAndLiveOpenFGAMembership(t *testing.T) {
	ctx := context.Background()
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, client.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
	require.NoError(t, client.GrantRole(ctx, testutil.TenantA, "user-admin", authz.RoleTenantAdmin))
	signer, verifier := testutil.NewSessionSignerForTest(t)
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Verifier: authn.NewSessionVerifier(verifier), Membership: client,
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	token, err := signer.Mint("user-admin", testutil.TenantA, time.Now(), time.Now().Add(time.Minute))
	require.NoError(t, err)
	call := func(method, path, credential, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		return res.StatusCode, string(data)
	}
	path := "/v1/workforce/hospitals/" + uuid.NewString()
	status, response := call("PUT", path, token, `{"name":"Synthetic HTTP Hospital"}`)
	require.Equal(t, 200, status, response)
	status, response = call("PUT", path+"/access/user-admin", token, `{"level":"manager"}`)
	require.Equal(t, 200, status, response)
	staffPath := path + "/staff/" + uuid.NewString()
	status, response = call("PUT", staffPath, token, `{"employee_code":"HTTP-01","display_name":"Synthetic","job_category":"doctor","employment_status":"active"}`)
	require.Equal(t, 200, status, response)
	status, response = call("GET", staffPath, token, "")
	require.Equal(t, 200, status, response)
	status, _ = call("GET", staffPath, "forged", "")
	require.Equal(t, 401, status)
	require.NoError(t, client.RevokeRole(ctx, testutil.TenantA, "user-admin", authz.RoleTenantAdmin))
	status, response = call("GET", staffPath, token, "")
	require.Equal(t, 403, status, "signed session and hospital grant cannot bypass revoked tenant membership: "+response)
}

func TestWorkforceIsolationDatabaseConstraintsAndIdempotency(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "writer": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}, "other": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	h1, h2, foreign := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/"
	for _, hospital := range []string{h1, h2} {
		w := testutil.Do(r, "PUT", base+hospital, "admin", `{"name":"Synthetic Hospital"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		w = testutil.Do(r, "PUT", base+hospital+"/access/user-writer", "admin", `{"level":"manager"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w := testutil.Do(r, "PUT", base+foreign, "other", `{"name":"Synthetic Other Hospital"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "PUT", base+h1+"/access/user-reader", "admin", `{"level":"reader"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	id := uuid.NewString()
	path := base + h1 + "/staff/" + id
	body := `{"employee_code":"SYN-01","display_name":"Synthetic Staff","job_category":"nurse","employment_status":"active"}`
	w = testutil.Do(r, "PUT", path, "writer", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	first := w.Body.String()
	w = testutil.Do(r, "PUT", path, "writer", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, first, w.Body.String(), "unchanged retry must preserve timestamps")
	w = testutil.Do(r, "GET", path, "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "PUT", path, "reader", body)
	require.Equal(t, 404, w.Code)
	w = testutil.Do(r, "GET", base+h2+"/staff/"+id, "writer", "")
	require.Equal(t, 404, w.Code, "even a manager of both hospitals must select the owning hospital")
	w = testutil.Do(r, "PUT", base+h2+"/staff/"+uuid.NewString(), "reader", body)
	require.Equal(t, 404, w.Code)
	w = testutil.Do(r, "PUT", base+foreign+"/access/user-writer", "admin", `{"level":"manager"}`)
	require.Equal(t, 404, w.Code, "composite hospital FK must block cross-tenant access grants")
	w = testutil.Do(r, "PUT", base+h1+"/staff/"+uuid.NewString(), "writer", body)
	require.Equal(t, 409, w.Code, "employee code unique within hospital")
	w = testutil.Do(r, "PUT", base+h2+"/staff/"+uuid.NewString(), "writer", body)
	require.Equal(t, 200, w.Code, "same employee code may exist at isolated sibling hospital")
	for _, invalid := range []string{
		strings.Replace(body, `"employee_code"`, `"tenant_id":"`+testutil.TenantB+`","employee_code"`, 1),
		strings.Replace(body, "Synthetic Staff", "   ", 1),
		strings.Replace(body, "Synthetic Staff", strings.Repeat("x", 9000), 1),
		body + body,
	} {
		w = testutil.Do(r, "PUT", path, "writer", invalid)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, token := range []string{"other", "admin", "reader"} {
		w = testutil.Do(r, "GET", base+h2+"/staff", token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, w.Body.String())
	}
	var seen int
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM workforce_staff`).Scan(&seen).Error
	}))
	require.Zero(t, seen, "tenant alone must not expose staff or inherit a pooled hospital context")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject', 'user-writer', true), set_config('app.workforce_hospital', ?, true)`, h1).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM workforce_staff`).Scan(&seen).Error
	}))
	require.Equal(t, 1, seen, "raw query without WHERE must remain hospital scoped")
	for _, statement := range []string{
		`UPDATE workforce_audit SET action = 'forged'`,
		`DELETE FROM workforce_audit`,
		`TRUNCATE workforce_audit`,
		`DELETE FROM workforce_staff`,
		`TRUNCATE workforce_staff`,
	} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(statement).Error })
		require.Error(t, err, statement)
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM workforce_audit WHERE action = 'workforce_staff.insert' AND hospital_id = ?`, h1).Scan(&seen).Error
	}))
	require.Equal(t, 1, seen, "retry must not append duplicate mutation audit")
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject', 'user-writer', true), set_config('app.workforce_hospital', ?, true)`, h1).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workforce_staff (tenant_id,hospital_id,id,employee_code,display_name,job_category,employment_status) VALUES (?, ?, ?, 'RAW', 'Synthetic', 'nurse', 'active')`, testutil.TenantB, h1, uuid.New()).Error
		})
		require.Error(t, err, fmt.Sprintf("forged tenant/hospital linkage as %s", tenant))
	}
}

func TestStaffListsUseBoundedCursorPages(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	path := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", `{"name":"Synthetic"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for i := 0; i < 3; i++ {
		body := fmt.Sprintf(`{"employee_code":"SYN-%d","display_name":"Synthetic","job_category":"other","employment_status":"active"}`, i)
		w := testutil.Do(r, "PUT", path+"/staff/"+uuid.NewString(), "admin", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w := testutil.Do(r, "GET", path+"/staff?limit=2", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Page struct {
			Cursor string `json:"next_cursor"`
			More   bool   `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.True(t, page.Page.More)
	firstIDs := []string{page.Data[0].ID, page.Data[1].ID}
	w = testutil.Do(r, "GET", path+"/staff?limit=2&cursor="+page.Page.Cursor, "admin", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.False(t, page.Page.More)
	require.NotContains(t, firstIDs, page.Data[0].ID)
	w = testutil.Do(r, "GET", path+"/staff?limit=201", "admin", "")
	require.Equal(t, 400, w.Code)
}

func TestHospitalDiscoveryOnlyReturnsExplicitGrants(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms:  map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}}, Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	path := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", `{"name":"YES Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path+"/access/user-reader", "admin", `{"level":"reader"}`).Code)
	for _, token := range []string{"admin", "other"} {
		w := testutil.Do(r, "GET", "/v1/workforce/hospitals", token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), hospital)
	}
	w := testutil.Do(r, "GET", "/v1/workforce/hospitals", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"YES Hospital"`)
	require.Contains(t, w.Body.String(), `"access_level":"reader"`)
	require.Equal(t, 401, testutil.Do(r, "GET", "/v1/workforce/hospitals", "", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", path+"/access/user-reader", "admin", "").Code)
	require.NotContains(t, testutil.Do(r, "GET", "/v1/workforce/hospitals", "reader", "").Body.String(), hospital)
}
