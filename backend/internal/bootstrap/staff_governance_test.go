package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

type governanceNativeIdentity struct{}

func (governanceNativeIdentity) MembershipEligible(_ context.Context, tenant, subject string) (bool, error) {
	return tenant == testutil.TenantA && subject == "user-reviewer", nil
}

func governanceRequest(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}

func TestGovernanceSingleOwnerGrantRetainsIntentAndNotifiesEveryOwner(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {"iam.governance.manage", "iam.governance.read", workforce.PermHospitalManage}},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(governanceNativeIdentity{}), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-reviewer','staff_member')`, testutil.TenantA, testutil.TenantA).Error
	}))
	hospital := uuid.Must(uuid.NewV7())
	created := governanceRequest(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital.String(), "admin", `{"name":"Synthetic governance hospital"}`)
	require.Equal(t, 200, created.Code, created.Body.String())
	id := uuid.Must(uuid.NewV7())
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	input := iam.GovernanceGrantInput{Subject: "user-reviewer", ExpectedMembershipEpoch: 1, TemplateKey: "workforce.account_link_reviewer.v1", ScopeKind: "hospital", ScopeID: &hospital, Reason: "Delegate independent Staff link review", ValidFrom: start, ExpiresAt: start.Add(24 * time.Hour)}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	path := "/v1/iam/governance-grants/" + id.String()
	first := governanceRequest(r, http.MethodPut, path, "admin", string(raw))
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var receipt iam.GovernanceGrantReceipt
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &receipt))
	require.Equal(t, id, receipt.ID)
	require.Equal(t, testutil.TenantA, receipt.TenantID.String())
	require.Equal(t, "provisional_active", receipt.Status)
	require.Equal(t, "single_owner_provisional", receipt.AuthorizationMode)
	require.EqualValues(t, 1, receipt.Version)
	require.EqualValues(t, 1, receipt.OwnerCount)
	require.Equal(t, "user-admin", receipt.IssuedBy)
	require.Equal(t, input.Subject, receipt.Input.Subject)
	replay := governanceRequest(r, http.MethodPut, path, "admin", string(raw))
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	changed := strings.Replace(string(raw), input.Reason, "A different delegation intent", 1)
	conflict := governanceRequest(r, http.MethodPut, path, "admin", changed)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	notices := governanceRequest(r, http.MethodGet, "/v1/iam/governance-notices", "admin", "")
	require.Equal(t, http.StatusOK, notices.Code, notices.Body.String())
	var inbox struct {
		Data []iam.GovernanceOwnerNotice `json:"data"`
	}
	require.NoError(t, json.Unmarshal(notices.Body.Bytes(), &inbox))
	require.Len(t, inbox.Data, 1)
	require.Equal(t, id, inbox.Data[0].GrantID)
	require.Equal(t, "user-admin", inbox.Data[0].RecipientSubject)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", "helivanta.in.iam.member_granted.v1").Count(&count).Error)
		require.EqualValues(t, 1, count, "routing projection intent commits with source, notice and audit")
		require.NoError(t, tx.Table("audit_events").Where("resource_type='iam_governance_grants' AND resource_id=? AND action=?", id, "iam.governance.granted").Count(&count).Error)
		require.EqualValues(t, 1, count, "replay cannot produce a second grant or grant audit")
		return nil
	}))
	t.Logf("Retained grant %s and owner notice %s; recipient login and operating access are separate acceptance gates", id, inbox.Data[0].ID)
}
