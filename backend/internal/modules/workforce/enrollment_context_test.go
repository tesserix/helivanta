package workforce_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestProtectedEnrollmentRequiresTheOpeningAccountAndWorkspace(t *testing.T) {
	h := newSupervisionHarness(t)
	id, path, input := h.person(t, "SYN-PROFILE-CONTEXT")
	input["request_context_version"] = "v1"
	input["profile"] = map[string]any{"given_name": "Synthetic protected name"}
	var auditBefore, outboxBefore int64
	require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
		if err := tx.Table("workforce_audit").Count(&auditBefore).Error; err != nil {
			return err
		}
		return tx.Table("outbox_events").Count(&outboxBefore).Error
	}))
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, tc := range []struct {
			name            string
			tenant, subject []string
		}{
			{"missing", nil, nil},
			{"empty_tenant", []string{""}, []string{"user-admin"}},
			{"empty_account", []string{testutil.TenantA}, []string{""}},
			{"missing_tenant", nil, []string{"user-admin"}},
			{"missing_account", []string{testutil.TenantA}, nil},
			{"duplicate_tenant", []string{testutil.TenantA, testutil.TenantA}, []string{"user-admin"}},
			{"joined_tenant", []string{testutil.TenantA + ", " + testutil.TenantA}, []string{"user-admin"}},
			{"joined_account", []string{testutil.TenantA}, []string{"user-admin, user-admin"}},
			{"wrong_tenant", []string{testutil.TenantB}, []string{"user-admin"}},
			{"wrong_account", []string{testutil.TenantA}, []string{"different-admin"}},
			{"duplicate_account", []string{testutil.TenantA}, []string{"user-admin", "user-admin"}},
		} {
			t.Run(method+"_"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest(method, path+"?request_context=v1", strings.NewReader(string(mustJSON(t, input))))
				req.Header.Set("Authorization", "Bearer admin")
				req.Header.Set("Content-Type", "application/json")
				for _, v := range tc.tenant {
					req.Header.Add("X-Helivanta-Expected-Tenant", v)
				}
				for _, v := range tc.subject {
					req.Header.Add("X-Helivanta-Expected-Subject", v)
				}
				res := httptest.NewRecorder()
				h.router.ServeHTTP(res, req)
				require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
				require.Contains(t, res.Body.String(), `"error":"session_changed"`)
				require.NotContains(t, res.Body.String(), "Synthetic protected name")
				require.NoError(t, h.scoped(t, func(tx *gorm.DB) error {
					var profile struct{ GivenName string }
					var version, auditAfter, outboxAfter int64
					if err := tx.Table("workforce_enrollment_profile").Select("given_name").Where("staff_id=?", id).Take(&profile).Error; err != nil {
						return err
					}
					if err := tx.Table("workforce_staff").Select("version").Where("id=?", id).Scan(&version).Error; err != nil {
						return err
					}
					if err := tx.Table("workforce_audit").Count(&auditAfter).Error; err != nil {
						return err
					}
					if err := tx.Table("outbox_events").Count(&outboxAfter).Error; err != nil {
						return err
					}
					require.Empty(t, profile.GivenName)
					require.EqualValues(t, 1, version)
					require.Equal(t, auditBefore, auditAfter)
					require.Equal(t, outboxBefore, outboxAfter)
					return nil
				}))
			})
		}
	}
}

func TestEnrollmentContextProjectionIsExplicitAndLegacyCompatible(t *testing.T) {
	h := newSupervisionHarness(t)
	id, path, input := h.person(t, "SYN-CONTEXT-COMPATIBILITY")
	request := func(method, query string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path+query, strings.NewReader(string(mustJSON(t, body))))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
		res := httptest.NewRecorder()
		h.router.ServeHTTP(res, req)
		return res
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, query := range []string{"?request_context=", "?request_context=v2", "?request_context=v1&request_context=v1"} {
			res := request(method, query, input)
			require.Equal(t, 400, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), `"error":"invalid_request"`)
		}
	}
	for _, tc := range []struct {
		query  string
		marker any
	}{
		{"?request_context=v1", ""}, {"?request_context=v1", "v2"}, {"", "v1"}, {"", ""}, {"", nil}, {"?request_context=v1", nil},
	} {
		input["request_context_version"] = tc.marker
		res := request(http.MethodPut, tc.query, input)
		require.Equal(t, 400, res.Code, res.Body.String())
	}
	delete(input, "request_context_version")
	res := request(http.MethodGet, "", nil)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.NotContains(t, res.Body.String(), "request_context_version")
	var legacy struct {
		Version int64 `json:"version"`
		Staff   struct {
			ID string `json:"id"`
		} `json:"staff"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &legacy))
	require.EqualValues(t, 1, legacy.Version)
	require.Equal(t, id, legacy.Staff.ID)
	res = request(http.MethodGet, "?request_context=v1", nil)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"request_context_version":"v1"`)
	input["request_context_version"] = "v1"
	input["profile"] = map[string]any{"given_name": "Synthetic current context"}
	res = request(http.MethodPut, "?request_context=v1", input)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"request_context_version":"v1"`)
	require.Contains(t, res.Body.String(), `"version":2`)
	res = request(http.MethodGet, "", nil)
	require.Equal(t, 200, res.Code, res.Body.String())
	require.NotContains(t, res.Body.String(), "request_context_version")
	require.Contains(t, res.Body.String(), "Synthetic current context")
}
