package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises the module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMembershipRemovalRetryCannotRemoveReplacementIncarnation(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil).SetUserStateChecker(membershipIdentity{})},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error
	}))
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
	legacy := testutil.Do(r, http.MethodDelete, path, "admin", "")
	require.Equal(t, http.StatusConflict, legacy.Code, legacy.Body.String())
	require.Contains(t, legacy.Body.String(), `"error":"membership_command_required"`)
	removeID := uuid.Must(uuid.NewV7())
	removeBody := fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":"synthetic-recovered-member","role_key":"staff_member","expected_membership_epoch":1,"reason":"Remove original membership before recovery"}`, original.ID.String())
	removed := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removeID.String(), "admin", removeBody)
	require.Equal(t, http.StatusAccepted, removed.Code, removed.Body.String())
	recoverBody := fmt.Sprintf(`{"operation":"recover","basis_id":%q,"subject":"synthetic-recovered-member","role_key":"staff_member","expected_membership_epoch":2,"reason":"Recover fresh ordinary membership after review"}`, removeID.String())
	recovered := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), "admin", recoverBody)
	require.Equal(t, http.StatusAccepted, recovered.Code, recovered.Body.String())
	var replacement iam.MembershipCommand
	require.NoError(t, json.Unmarshal(recovered.Body.Bytes(), &replacement))
	require.NotEqual(t, original.ID, replacement.ResultMembershipID)
	retry := testutil.Do(r, http.MethodDelete, path, "admin", "")
	require.Equal(t, http.StatusConflict, retry.Code, retry.Body.String())
	require.Contains(t, retry.Body.String(), `"error":"membership_command_required"`)
	commandReplay := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removeID.String(), "admin", removeBody)
	require.Equal(t, http.StatusAccepted, commandReplay.Code, commandReplay.Body.String())
	require.JSONEq(t, removed.Body.String(), commandReplay.Body.String())
	listed := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), replacement.ResultMembershipID.String(), "a delayed removal retry must preserve the newly granted membership")
}
