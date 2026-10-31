package iam_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMemberEnrollmentRejectsStaleWorkspaceBeforeWriting(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/iam/members", strings.NewReader(`{"subject":"synthetic-staff","role_key":"staff_member"}`))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"error":"session_changed"`)
	list := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, list.Body.String())
}

func TestMemberEnrollmentMatchingWorkspaceAndLegacyReplayReturnSameID(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	body := `{"subject":"synthetic-staff","role_key":"staff_member"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/iam/members", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Equal(t, http.StatusAccepted, res.Code, res.Body.String())
	var accepted iam.MemberGrantResult
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &accepted))
	require.NotEqual(t, uuid.Nil, accepted.ID)
	replay := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", body)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, res.Body.String(), replay.Body.String())
}

func TestMemberRemovalRejectsChangedOrAmbiguousWorkspaceWithoutRemovingMembership(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	created := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-removal-staff","role_key":"staff_member"}`)
	require.Equal(t, http.StatusAccepted, created.Code, created.Body.String())
	before := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	require.Contains(t, before.Body.String(), "synthetic-removal-staff")
	for _, tc := range []struct {
		name   string
		values []string
	}{
		{"changed", []string{testutil.TenantB}},
		{"empty", []string{""}},
		{"malformed", []string{"not-a-tenant"}},
		{"zero", []string{uuid.Nil.String()}},
		{"duplicate", []string{testutil.TenantA, testutil.TenantA}},
		{"comma_joined", []string{testutil.TenantA + "," + testutil.TenantA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/v1/iam/members/synthetic-removal-staff/staff_member", nil)
			req.Header.Set("Authorization", "Bearer admin")
			for _, value := range tc.values {
				req.Header.Add("X-Helivanta-Expected-Tenant", value)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), `"error":"session_changed"`)
			require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
			after := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
			require.Equal(t, http.StatusOK, after.Code, after.Body.String())
			require.JSONEq(t, before.Body.String(), after.Body.String())
		})
	}
}

func TestMemberRemovalMatchingWorkspaceAndLegacyReplayRequireCommand(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	created := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-removal-staff","role_key":"staff_member"}`)
	require.Equal(t, http.StatusAccepted, created.Code, created.Body.String())
	req := httptest.NewRequest(http.MethodDelete, "/v1/iam/members/synthetic-removal-staff/staff_member", nil)
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"error":"membership_command_required"`)
	list := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Contains(t, list.Body.String(), "synthetic-removal-staff")
	replay := testutil.Do(r, http.MethodDelete, "/v1/iam/members/synthetic-removal-staff/staff_member", "admin", "")
	require.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
	require.JSONEq(t, res.Body.String(), replay.Body.String())
}
