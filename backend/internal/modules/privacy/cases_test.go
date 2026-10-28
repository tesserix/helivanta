package privacy_test

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOfficerMustAcceptBeforeReceivingCaseAuthority(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "patient": testutil.TenantA, "peer": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage"}}, Modules: []platform.Module{privacy.New()}})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	routed := gin.New()
	routed.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		c.Request.Header.Set("X-User-ID", "forged")
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	r = routed
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/officers/" + id
	body := fmt.Sprintf(`{"subject":"user-officer","display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 403, testutil.Do(r, "PUT", path, "patient", body).Code)
	nominated := testutil.Do(r, "PUT", path, "admin", body)
	require.Equal(t, 201, nominated.Code, nominated.Body.String())
	require.Contains(t, nominated.Body.String(), `"status":"nominated"`)
	require.Contains(t, nominated.Body.String(), `"valid_until":"2099-01-01T00:00:00Z"`)
	req := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 503, testutil.Do(r, "PUT", req, "patient", `{"kind":"access","description":"Synthetic account access request"}`).Code)
	require.Equal(t, 404, testutil.Do(r, "POST", path+"/accept", "patient", `{}`).Code)
	accepted := testutil.Do(r, "POST", path+"/accept", "officer", `{}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"status":"active"`)
	created := testutil.Do(r, "PUT", req, "patient", `{"kind":"access","description":"Synthetic account access request"}`)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"received"`)
	for _, token := range []string{"peer", "other", "admin"} {
		require.Equal(t, 404, testutil.Do(r, "GET", req, token, "").Code)
	}
	require.Equal(t, 200, testutil.Do(r, "GET", req, "officer", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", req, "patient", "").Code)
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
}

func TestCaseCommandsRetainHistoryAndDoNotClaimRightsFulfilment(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage"}}, Modules: []platform.Module{privacy.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	op := "/v1/privacy/officers/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", op, "admin", fmt.Sprintf(`{"subject":"user-officer","display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/accept", "officer", `{}`).Code)
	path := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"erasure","description":"Please review this synthetic request"}`).Code)
	command := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	body := `{"expected_version":1,"action":"start_review","message":"Review started"}`
	require.Equal(t, 403, testutil.Do(r, "PUT", command, "patient", body).Code)
	result := testutil.Do(r, "PUT", command, "officer", body)
	require.Equal(t, 201, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), `"resulting_version":2`)
	require.JSONEq(t, result.Body.String(), testutil.Do(r, "PUT", command, "officer", body).Body.String())
	response := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, testutil.Do(r, "PUT", response, "officer", `{"expected_version":1,"action":"respond","message":"Reasoned test response"}`).Code)
	require.Equal(t, 201, testutil.Do(r, "PUT", response, "officer", `{"expected_version":2,"action":"respond","message":"Request reviewed. No record has been erased by this response."}`).Code)
	current := testutil.Do(r, "GET", path, "patient", "")
	require.Contains(t, current.Body.String(), `"status":"responded"`)
	require.NotContains(t, current.Body.String(), `"fulfilled"`)
	history := testutil.Do(r, "GET", path+"/commands", "patient", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), "No record has been erased")
	require.Equal(t, 409, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "patient", `{"expected_version":3,"action":"withdraw","message":"Withdraw this test"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "GET", "/v1/privacy/requests", "patient", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", "/v1/privacy/queue", "officer", "").Code)
	require.Equal(t, 403, testutil.Do(r, "GET", "/v1/privacy/queue", "admin", "").Code)
}

func TestRevokedAndSupersededOfficersLoseCaseAccess(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "first": testutil.TenantA, "next": testutil.TenantA, "patient": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage"}}, Modules: []platform.Module{privacy.New()}})
	nominate := func(subject string) string {
		t.Helper()
		path := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
		result := testutil.Do(r, "PUT", path, "admin", fmt.Sprintf(`{"subject":%q,"display_name":"Test Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, "user-"+subject, uuid.Must(uuid.NewV7()).String()))
		require.Equal(t, 201, result.Code, result.Body.String())
		return path
	}
	stale := nominate("first")
	first := nominate("first")
	require.Equal(t, 409, testutil.Do(r, "POST", stale+"/accept", "first", `{}`).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", first+"/accept", "first", `{}`).Code)
	path := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"grievance","description":"Synthetic grievance"}`).Code)
	next := nominate("next")
	require.Equal(t, 200, testutil.Do(r, "GET", path, "first", "").Code)
	require.Equal(t, 200, testutil.Do(r, "POST", next+"/accept", "next", `{}`).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "first", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "next", "").Code)
	pub := testutil.Do(r, "GET", "/v1/privacy/officer", "patient", "")
	require.Equal(t, 200, pub.Code, pub.Body.String())
	require.NotContains(t, pub.Body.String(), "user-next")
	require.NotContains(t, pub.Body.String(), "review_reference")
	body := fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 403, testutil.Do(r, "POST", next+"/revoke", "patient", body).Code)
	revoked := testutil.Do(r, "POST", next+"/revoke", "admin", body)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.JSONEq(t, revoked.Body.String(), testutil.Do(r, "POST", next+"/revoke", "admin", body).Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", path, "next", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "patient", "").Code)
	require.Equal(t, 503, testutil.Do(r, "GET", "/v1/privacy/officer", "patient", "").Code)
}

func caseFixture(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context, string, string) {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "patient": testutil.TenantA, "peer": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {"privacy.officer.manage"}}, Modules: []platform.Module{privacy.New()}})
	op := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", op, "admin", fmt.Sprintf(`{"subject":"user-officer","display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/accept", "officer", `{}`).Code)
	path := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic access request"}`).Code)
	return r, db, ctx, op, path
}
func TestDatabaseRejectsInventedCaseTransitions(t *testing.T) {
	r, db, ctx, _, path := caseFixture(t)
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject','user-patient',true)").Error; e != nil {
			return e
		}
		return tx.Exec("UPDATE privacy_requests SET status='responded',version=2 WHERE tenant_id=? AND id=?", testutil.TenantA, path[len("/v1/privacy/requests/"):]).Error
	})
	require.Error(t, err, "a raw application transaction must not manufacture an officer response")
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"received"`)
}

func TestCommandIDsCannotBeReusedAcrossCases(t *testing.T) {
	r, _, _, _, path := caseFixture(t)
	other := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(r, "PUT", other, "patient", `{"kind":"correction","description":"Another test request"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	body := `{"expected_version":1,"action":"start_review","message":"Review"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/commands/"+id, "officer", body).Code)
	conflict := testutil.Do(r, "PUT", other+"/commands/"+id, "officer", body)
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	require.NotContains(t, conflict.Body.String(), "duplicate")
	require.Contains(t, testutil.Do(r, "GET", other, "patient", "").Body.String(), `"version":1`)
}
func TestResponseAuditFailureRollsBackCommandAndState(t *testing.T) {
	r, db, ctx, _, path := caseFixture(t)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_privacy_audit_outage", SQL: `CREATE FUNCTION fail_privacy_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='privacy_commands' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic private audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_privacy_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_privacy_read();`}}))
	result := testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "officer", `{"expected_version":1,"action":"start_review","message":"Review"}`)
	require.Equal(t, 500, result.Code, result.Body.String())
	require.NotContains(t, result.Body.String(), "synthetic")
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"version":1`)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject','user-patient',true)").Error; e != nil {
			return e
		}
		var n int64
		e := tx.Table("privacy_commands").Count(&n).Error
		require.Zero(t, n)
		return e
	}))
}

func TestPrivacyInvalidAndConcurrentCommands(t *testing.T) {
	r, _, _, op, path := caseFixture(t)
	for _, body := range []string{`null`, `{}`, `{"kind":"unknown","description":"test"}`, `{"kind":"access","description":"test","tenant_id":"forged"}`, `{"kind":"access","description":"test"}{}`, fmt.Sprintf(`{"kind":"access","description":%q}`, strings.Repeat("x", 4097)), strings.Repeat(" ", 16385)} {
		result := testutil.Do(r, "PUT", "/v1/privacy/requests/"+uuid.Must(uuid.NewV7()).String(), "patient", body)
		require.Equal(t, 400, result.Code, result.Body.String())
	}
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/privacy/requests/not-an-id", "patient", "").Code)
	require.Equal(t, 400, testutil.Do(r, "POST", op+"/accept", "officer", `null`).Code)
	require.Equal(t, 400, testutil.Do(r, "POST", op+"/revoke", "admin", `{"expected_version":0,"review_reference":"bad"}`).Code)
	for _, body := range []string{`null`, `{}`, `{"expected_version":1,"action":"erase","message":"test"}`, `{"expected_version":1,"action":"start_review","message":""}`, `{"expected_version":1,"action":"start_review","message":"test","actor":"forged"}`} {
		require.Equal(t, 400, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "officer", body).Code)
	}
	command := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	body := `{"expected_version":1,"action":"start_review","message":"Review"}`
	statuses := make(chan int, 8)
	for range 8 {
		go func() { statuses <- testutil.Do(r, "PUT", command, "officer", body).Code }()
	}
	created := 0
	for range 8 {
		status := <-statuses
		if status == 201 {
			created++
		} else {
			require.Equal(t, 200, status)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 409, testutil.Do(r, "PUT", command, "officer", strings.Replace(body, "Review", "Changed", 1)).Code)
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "officer", `{"expected_version":2,"action":"request_information","message":"Clarify scope"}`).Code)
	require.Equal(t, 403, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "officer", `{"expected_version":3,"action":"reply","message":"Cannot speak for patient"}`).Code)
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "patient", `{"expected_version":3,"action":"reply","message":"Only my account data"}`).Code)
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "patient", `{"expected_version":4,"action":"withdraw","message":"Withdraw this test request"}`).Code)
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"withdrawn"`)
}

func TestOwnSubmissionReplaySurvivesOfficerRevocation(t *testing.T) {
	r, _, _, op, path := caseFixture(t)
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/revoke", "admin", fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())).Code)
	replay := testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic access request"}`)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"status":"received"`)
}

func TestOfficerInputAndReplayAreBounded(t *testing.T) {
	r, _, _, op, path := caseFixture(t)
	body := fmt.Sprintf(`{"subject":"user-officer","display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	for _, bad := range []string{`null`, `{}`, strings.Replace(body, "privacy@example.invalid", "not-an-email", 1), strings.Replace(body, "2099-01-01", "not-a-date", 1), strings.Replace(body, "user-officer", "", 1), strings.Replace(body, "00:00:00Z", "00:00:00.0000001Z", 1)} {
		result := testutil.Do(r, "PUT", "/v1/privacy/officers/"+uuid.Must(uuid.NewV7()).String(), "admin", bad)
		require.Equal(t, 400, result.Code, result.Body.String())
	}
	require.Equal(t, 409, testutil.Do(r, "PUT", "/v1/privacy/officers/"+uuid.Must(uuid.NewV7()).String(), "admin", strings.Replace(body, "2099-01-01", "2000-01-01", 1)).Code)
	fresh := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
	created := testutil.Do(r, "PUT", fresh, "admin", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.JSONEq(t, created.Body.String(), testutil.Do(r, "PUT", fresh, "admin", body).Body.String())
	require.Equal(t, 409, testutil.Do(r, "PUT", fresh, "admin", strings.Replace(body, "Synthetic Officer", "Changed Officer", 1)).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", op+"/accept", "officer", `{}`).Code)
	require.Equal(t, 404, testutil.Do(r, "POST", op+"/accept", "other", `{}`).Code)
	require.Equal(t, 404, testutil.Do(r, "POST", "/v1/privacy/officers/bad/accept", "officer", `{}`).Code)
	require.Equal(t, 400, testutil.Do(r, "POST", op+"/accept", "officer", `{"bad":true}`).Code)
	require.Equal(t, 403, testutil.Do(r, "GET", "/v1/privacy/queue", "peer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/commands", "peer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path+"/commands", "other", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/privacy/requests/bad/commands", "patient", "").Code)
	require.Equal(t, 409, testutil.Do(r, "POST", op+"/revoke", "admin", fmt.Sprintf(`{"expected_version":1,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 503, testutil.Do(r, "GET", "/v1/privacy/officer", "other", "").Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Changed"}`).Code)
}

func TestExpiredOfficerCannotReadExistingCases(t *testing.T) {
	r, db, ctx, _, path := caseFixture(t)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_privacy_elapsed_appointment", SQL: `SELECT set_config('app.tenant_id','11111111-1111-1111-1111-111111111111',true),set_config('app.actor_subject','synthetic-fixture',true),set_config('app.privacy_manage','true',true); ALTER TABLE privacy_officers DISABLE TRIGGER privacy_officer_guard; UPDATE privacy_officers SET created_at=clock_timestamp()-interval '3 days',valid_until=clock_timestamp()-interval '1 day' WHERE status='active'; ALTER TABLE privacy_officers ENABLE TRIGGER privacy_officer_guard;`}}))
	require.Equal(t, 404, testutil.Do(r, "GET", path, "officer", "").Code)
	require.Equal(t, 403, testutil.Do(r, "GET", "/v1/privacy/queue", "officer", "").Code)
	require.Equal(t, 503, testutil.Do(r, "GET", "/v1/privacy/officer", "patient", "").Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "patient", "").Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic access request"}`).Code)
	require.Equal(t, 503, testutil.Do(r, "PUT", "/v1/privacy/requests/"+uuid.Must(uuid.NewV7()).String(), "patient", `{"kind":"access","description":"New request"}`).Code)
}

func TestDatabaseRequiresManagementAuthorityForOfficerRevocation(t *testing.T) {
	r, db, ctx, op, path := caseFixture(t)
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject','user-patient',true)").Error; e != nil {
			return e
		}
		return tx.Exec("UPDATE privacy_officers SET status='revoked',version=3,ended_reason='revoked',ended_reference=? WHERE tenant_id=? AND id=?", uuid.Must(uuid.NewV7()), testutil.TenantA, op[len("/v1/privacy/officers/"):]).Error
	})
	require.Error(t, err)
	require.Equal(t, 200, testutil.Do(r, "GET", path, "officer", "").Code)
}
