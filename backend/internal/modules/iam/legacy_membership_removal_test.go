package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestLegacyMembershipRemovalRequiresIncarnationCommandWithoutSideEffects(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "restricted": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	created := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-legacy-target","role_key":"staff_member"}`)
	require.Equal(t, http.StatusAccepted, created.Code, created.Body.String())
	before := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	var beforeEvents int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Count(&beforeEvents).Error
	}))
	for _, subject := range []string{"synthetic-legacy-target", "synthetic-absent-target"} {
		path := "/v1/iam/members/" + subject + "/staff_member"
		for range 2 {
			res := testutil.Do(r, http.MethodDelete, path, "admin", "")
			require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
			require.JSONEq(t, `{"error":"membership_command_required","message":"Use PUT /v1/iam/membership-commands/:id with the current membership ID, epoch and reason"}`, res.Body.String())
			require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
		}
		restricted := testutil.Do(r, http.MethodDelete, path, "restricted", "")
		require.Equal(t, http.StatusForbidden, restricted.Code, restricted.Body.String())
	}
	after := testutil.Do(r, http.MethodGet, "/v1/iam/members", "admin", "")
	require.Equal(t, http.StatusOK, after.Code, after.Body.String())
	require.JSONEq(t, before.Body.String(), after.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Equal(t, beforeEvents, count, "legacy removal must not publish a reconciliation event")
		require.NoError(t, tx.Table("iam_membership_commands").Count(&count).Error)
		require.Zero(t, count, "migration refusal cannot fabricate a removal receipt")
		return nil
	}))
}

func currentMembershipRemoval(t *testing.T, r http.Handler, subject, role string) (string, string) {
	t.Helper()
	res := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-candidates?limit=200", "admin", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct{ Data []iam.MembershipCandidate }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	for _, row := range page.Data {
		if row.Subject == subject && row.RoleKey == role {
			body, err := json.Marshal(iam.MembershipCommandInput{Operation: "remove", BasisID: row.ID, Subject: subject, RoleKey: role, ExpectedMembershipEpoch: row.MembershipEpoch, Reason: "Synthetic independently requested role removal"})
			require.NoError(t, err)
			return "/v1/iam/membership-commands/" + uuid.Must(uuid.NewV7()).String(), string(body)
		}
	}
	t.Fatalf("current membership candidate missing: %s/%s", subject, role)
	return "", ""
}
