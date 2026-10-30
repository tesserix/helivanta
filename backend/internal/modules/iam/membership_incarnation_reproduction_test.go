package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises the module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMembershipRemovalRetryCannotRemoveReplacementIncarnation(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	body := `{"subject":"synthetic-recovered-member","role_key":"staff_member"}`
	grant := func() iam.MemberGrantResult {
		t.Helper()
		res := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", body)
		require.Equal(t, http.StatusAccepted, res.Code, res.Body.String())
		var result iam.MemberGrantResult
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result))
		return result
	}
	original := grant()
	path := "/v1/iam/members/synthetic-recovered-member/staff_member"
	removed := testutil.Do(r, http.MethodDelete, path, "admin", "")
	require.Equal(t, http.StatusAccepted, removed.Code, removed.Body.String())
	replacement := grant()
	require.NotEqual(t, original.ID, replacement.ID)
	retry := testutil.Do(r, http.MethodDelete, path, "admin", "")
	t.Logf("delayed original DELETE response: %d %s", retry.Code, retry.Body.String())
	listed := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), replacement.ID.String(), "a delayed removal retry must preserve the newly granted membership")
}
