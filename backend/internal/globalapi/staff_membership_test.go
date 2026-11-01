package globalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestStaffMembershipThroughGlobalAPI(t *testing.T) {
	ctx := t.Context()
	fga, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(ctx, testutil.TenantA, authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "synthetic-admin", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantPermission(ctx, testutil.TenantA, iam.PermMemberManage, authz.RoleTenantAdmin))
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Verifier: authn.NewSessionVerifier(verifier), Membership: fga, Resolver: fga,
		Writer: fga, Roles: fga, Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(globalEnrollmentIdentity{})},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-admin','tenant_admin')`, testutil.TenantA).Error
	}))
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	tokens := map[string]string{}
	for _, subject := range []string{"synthetic-admin", "synthetic-staff", "synthetic-first-member"} {
		tokens[subject], err = signer.Mint(subject, testutil.TenantA, time.Now(), time.Now().Add(time.Minute))
		require.NoError(t, err)
	}
	call := func(method, path, subject, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tokens[subject])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", subject)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, response.Body.String())
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		return response
	}
	catalogue := call("GET", "/v1/iam/roles", "synthetic-admin", "", http.StatusOK)
	var roles struct{ Data []iam.SystemRole }
	require.NoError(t, json.Unmarshal(catalogue.Body.Bytes(), &roles))
	require.Contains(t, roles.Data, iam.SystemRole{Key: authz.RoleStaffMember, Label: "Staff member (membership only)"})
	const body = `{"subject":"synthetic-staff","role_key":"staff_member"}`
	first := call("POST", "/v1/iam/members", "synthetic-admin", body, http.StatusAccepted)
	second := call("POST", "/v1/iam/members", "synthetic-admin", body, http.StatusAccepted)
	require.JSONEq(t, first.Body.String(), second.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		member, err := fga.IsMember(ctx, "synthetic-staff", testutil.TenantA)
		require.NoError(c, err)
		require.True(c, member)
		// Finish both grant deliveries before exercising the ordered revoke journey.
		require.NoError(c, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			var count int64
			err := tx.Table("processed_events").Where("consumer=?", "iam-fga-sync").Count(&count).Error
			require.Equal(c, int64(2), count)
			return err
		}))
	}, 20*time.Second, 100*time.Millisecond)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("iam_members").Where("subject=? AND role_key=?", "synthetic-staff", "staff_member").Count(&count).Error
		require.Equal(t, int64(1), count)
		return err
	}))
	permissions := call("GET", "/v1/iam/me/permissions", "synthetic-staff", "", http.StatusOK)
	require.JSONEq(t, `{"data":[],"subject":"synthetic-staff","tenant_id":"11111111-1111-1111-1111-111111111111"}`, permissions.Body.String())
	call("GET", "/v1/iam/roles", "synthetic-staff", "", http.StatusForbidden)
	call("POST", "/v1/iam/members", "synthetic-staff", body, http.StatusForbidden)
	legacy := call("DELETE", "/v1/iam/members/synthetic-staff/staff_member", "synthetic-admin", "", http.StatusConflict)
	require.Contains(t, legacy.Body.String(), `"error":"membership_command_required"`)
	var membership iam.MemberGrantResult
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &membership))
	input, err := json.Marshal(iam.MembershipCommandInput{Operation: "remove", BasisID: membership.ID, Subject: "synthetic-staff", RoleKey: "staff_member", ExpectedMembershipEpoch: 1, Reason: "Remove synthetic member through the Global API"})
	require.NoError(t, err)
	commandPath := "/v1/iam/membership-commands/" + uuid.Must(uuid.NewV7()).String()
	removed := call("PUT", commandPath, "synthetic-admin", string(input), http.StatusAccepted)
	replayed := call("PUT", commandPath, "synthetic-admin", string(input), http.StatusAccepted)
	require.JSONEq(t, removed.Body.String(), replayed.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		member, err := fga.IsMember(ctx, "synthetic-staff", testutil.TenantA)
		require.NoError(c, err)
		require.False(c, member)
	}, 20*time.Second, 100*time.Millisecond)
	revoked := call("GET", "/v1/iam/roles", "synthetic-staff", "", http.StatusForbidden)
	require.Contains(t, revoked.Body.String(), "not a member of this tenant")
	enrollmentPath := "/v1/iam/membership-enrollments/" + uuid.Must(uuid.NewV7()).String()
	enrollmentBody := `{"subject":"synthetic-first-member","reason":"Verified first membership through Global API"}`
	enrolled := call("PUT", enrollmentPath, "synthetic-admin", enrollmentBody, http.StatusAccepted)
	exact := call("GET", enrollmentPath, "synthetic-admin", "", http.StatusOK)
	require.JSONEq(t, enrolled.Body.String(), exact.Body.String())
	repeated := call("PUT", enrollmentPath, "synthetic-admin", enrollmentBody, http.StatusAccepted)
	require.JSONEq(t, enrolled.Body.String(), repeated.Body.String())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ready, err := fga.IsMember(ctx, "synthetic-first-member", testutil.TenantA)
		require.NoError(c, err)
		require.True(c, ready)
	}, 20*time.Second, 100*time.Millisecond)
	noPermissions := call("GET", "/v1/iam/me/permissions", "synthetic-first-member", "", http.StatusOK)
	require.JSONEq(t, `{"data":[],"subject":"synthetic-first-member","tenant_id":"11111111-1111-1111-1111-111111111111"}`, noPermissions.Body.String())
	call("GET", enrollmentPath, "synthetic-first-member", "", http.StatusForbidden)
	call("PUT", "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), "synthetic-first-member", enrollmentBody, http.StatusForbidden)

}

type globalEnrollmentIdentity struct{}

func (globalEnrollmentIdentity) MembershipEligible(context.Context, string) (bool, error) {
	return true, nil
}
