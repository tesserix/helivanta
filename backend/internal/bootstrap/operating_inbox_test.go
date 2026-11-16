package bootstrap_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type nativeInboxCall func(method, path, actor string, input any, status int) string
type nativeInboxPage struct {
	Data []iam.OperatingDuty `json:"data"`
	Page struct {
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
	} `json:"page"`
}

func prepareRetainedOperatingDuty(t *testing.T, router *gin.Engine, kind string, employer, location, staff, link uuid.UUID, template string, now time.Time, call nativeInboxCall) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	path := "/v1/iam/operating-governance-employers/" + kind + "/" + employer.String() + "/duties/" + id.String()
	input := iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: kind, EmployerID: employer, LocationID: location, ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}, Subject: "user-auditor", TemplateKey: template, Reason: "Synthetic retained duty whose Staff revision will change", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	call("PUT", path, "owner2", input, 201)
	call("PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "admin", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "A distinct current owner reviews the Staff duty before its revision changes"}, 201)
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		response := governanceRequest(router, "GET", path, "owner2", "")
		if !assert.Equal(check, 200, response.Code, response.Body.String()) {
			return
		}
		var duty iam.OperatingDuty
		if !assert.NoError(check, json.Unmarshal(response.Body.Bytes(), &duty)) {
			return
		}
		assert.Equal(check, id, duty.ID)
		assert.Equal(check, "active", duty.Status)
	}, 10*time.Second, 20*time.Millisecond, "the retained case must activate before its Staff source changes")
	return id
}

func exerciseNativeOperatingInbox(t *testing.T, router *gin.Engine, db *tenantdb.DB, prefix, kind string, hidden, current, historical, extraLocation uuid.UUID, extraLocationVersion int64, input iam.OperatingDutyInput, call nativeInboxCall) {
	t.Helper()
	extra := uuid.Must(uuid.NewV7())
	call("PUT", prefix+"/duties/"+extra.String(), "owner2", input, 409)
	extraInput := input
	extraInput.Scope.LocationID = extraLocation
	extraInput.Scope.ExpectedLocationVersion = extraLocationVersion
	call("PUT", prefix+"/duties/"+extra.String(), "owner2", extraInput, 201)
	path := prefix + "/duty-inbox"
	var first nativeInboxPage
	require.NoError(t, json.Unmarshal([]byte(call("GET", path+"?limit=1", "auditor", nil, 200)), &first))
	require.Len(t, first.Data, 1)
	require.Equal(t, extra, first.Data[0].ID)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	cursor := url.QueryEscape(*first.Page.NextCursor)
	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest("GET", path+"?limit=20", nil))
	require.Equal(t, 401, anonymous.Code)
	call("GET", path+"?limit=20", "ordinary", nil, 403)
	for _, test := range []struct {
		query  string
		status int
	}{{"?limit=1&cursor=" + cursor, 400}, {"?limit=20", 404}} {
		request := httptest.NewRequest("GET", path+test.query, nil)
		request.Header.Set("Authorization", "Bearer foreign")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-foreign")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), current.String())
	}

	call("GET", path+"?limit=1&cursor="+cursor, "owner2", nil, 400)
	call("GET", "/v1/iam/operating-governance-employers/"+kind+"/"+hidden.String()+"/duty-inbox?limit=1&cursor="+cursor, "auditor", nil, 400)
	other := "pharmacy"
	if kind == other {
		other = "supplier"
	}
	call("GET", "/v1/iam/operating-governance-employers/"+other+"/"+input.Scope.EmployerID.String()+"/duty-inbox?limit=1&cursor="+cursor, "auditor", nil, 400)
	seen := map[uuid.UUID]bool{extra: true}
	next := first.Page.NextCursor
	for count := 0; next != nil; count++ {
		require.Less(t, count, 20, "fixture paging must stay bounded")
		var page nativeInboxPage
		require.NoError(t, json.Unmarshal([]byte(call("GET", path+"?limit=1&cursor="+url.QueryEscape(*next), "auditor", nil, 200)), &page))
		require.Len(t, page.Data, 1)
		row := page.Data[0]
		require.False(t, seen[row.ID])
		require.Equal(t, input.Scope.EmployerKind, row.Input.Scope.EmployerKind)
		require.Equal(t, input.Scope.EmployerID, row.Input.Scope.EmployerID)
		seen[row.ID] = true
		next = page.Page.NextCursor
	}
	require.True(t, seen[current])
	require.True(t, seen[historical], "the changed Staff revision must not erase its retained duty")
	for _, query := range []string{"?limit=21", "?limit=1&limit=20", "?staff=other", "?status=active", "?status=all&status=all"} {
		call("GET", path+query, "auditor", nil, 400)
	}
	call("GET", "/v1/iam/operating-governance-employers/"+kind+"/"+hidden.String()+"/duty-inbox?limit=20", "auditor", nil, 404)
	raw := call("GET", path+"?limit=20", "owner2", nil, 200)
	for _, private := range []string{"source_grant_id", "owner_membership_id", "source_proof", "native_administration"} {
		require.NotContains(t, raw, private)
	}
	countAudit := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("audit_events").Where("tenant_id=? AND action='iam.operating.employer_inbox.read'", testutil.TenantA).Count(&count).Error
		}))
		return count
	}
	before := countAudit()
	require.Positive(t, before)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_native_inbox_audit", SQL: `ALTER TABLE audit_events ADD CONSTRAINT synthetic_native_inbox_audit CHECK(action<>'iam.operating.employer_inbox.read')NOT VALID;`}}))
	func() {
		defer func() {
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_native_inbox_audit_restore", SQL: `ALTER TABLE audit_events DROP CONSTRAINT synthetic_native_inbox_audit;`}}))
		}()
		denied := call("GET", path+"?limit=20", "owner2", nil, 503)
		require.NotContains(t, denied, current.String())
		require.Equal(t, before, countAudit())
	}()
}

func exerciseIneligibleOperatingInbox(t *testing.T, prefix string, id uuid.UUID, call nativeInboxCall) {
	t.Helper()
	var page nativeInboxPage
	require.NoError(t, json.Unmarshal([]byte(call("GET", prefix+"/duty-inbox?limit=20", "owner2", nil, 200)), &page))
	var selected *iam.OperatingDuty
	for i := range page.Data {
		if page.Data[i].ID == id {
			selected = &page.Data[i]
			break
		}
	}
	require.NotNil(t, selected)
	require.Equal(t, "active", selected.Status)
	response := call("PUT", prefix+"/duties/"+id.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner2", iam.OperatingDutyDecisionInput{ExpectedVersion: selected.Version, Decision: "revoke", Reason: "Withdraw retained operating authority after the reviewed Staff revision changed"}, 201)
	var receipt struct {
		Result iam.OperatingDuty `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(response), &receipt))
	require.Equal(t, id, receipt.Result.ID)
	require.Equal(t, "revoked", receipt.Result.Status)
	require.Equal(t, selected.Version+1, receipt.Result.Version)
}
