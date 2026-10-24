package mediconnect_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestAccountCreationReplayAndIsolation(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"a": testutil.TenantA, "peer": testutil.TenantA, "b": testutil.TenantB},
		Modules: []platform.Module{mediconnect.New()},
	})
	const path = "/v1/mediconnect/account"
	require.Equal(t, http.StatusNotFound, testutil.Do(r, "GET", path, "a", "").Code)
	created := testutil.Do(r, "POST", path, "a", "{}")
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var account map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &account))
	require.NotEmpty(t, account["id"])
	require.Equal(t, float64(1), account["version"])
	require.Equal(t, "none", account["preferred_channel"])
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	replay := testutil.Do(r, "POST", path, "a", "{}")
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	own := testutil.Do(r, "GET", path, "a", "")
	require.Equal(t, http.StatusOK, own.Code)
	require.JSONEq(t, created.Body.String(), own.Body.String())
	for _, token := range []string{"peer", "b"} {
		require.Equal(t, http.StatusNotFound, testutil.Do(r, "GET", path, token, "").Code)
	}
	require.Equal(t, http.StatusUnauthorized, testutil.Do(r, "GET", path, "unknown", "").Code)
}

func TestPreferencesRejectStaleAndInvalidWrites(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	const path = "/v1/mediconnect/account"
	require.Equal(t, 201, testutil.Do(r, "POST", path, "a", "{}").Code)
	body := `{"expected_version":1,"locale":"hi-IN","preferred_channel":"email","low_data_mode":true}`
	updated := testutil.Do(r, "PATCH", path+"/preferences", "a", body)
	require.Equal(t, 200, updated.Code, updated.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &row))
	require.Equal(t, "hi-IN", row["locale"])
	require.Equal(t, "email", row["preferred_channel"])
	require.Equal(t, true, row["low_data_mode"])
	require.Equal(t, float64(2), row["version"])
	require.Equal(t, 409, testutil.Do(r, "PATCH", path+"/preferences", "a", body).Code)
	replay := testutil.Do(r, "POST", path, "a", "{}")
	require.JSONEq(t, updated.Body.String(), replay.Body.String())
	for _, invalid := range []string{
		`{}`, `null`, `{"expected_version":2,"locale":"en-IN","preferred_channel":"sms","low_data_mode":true}`,
		`{"expected_version":2,"locale":"invalid locale!","preferred_channel":"none","low_data_mode":true}`,
		`{"expected_version":2,"locale":"hi-IN","preferred_channel":"email","low_data_mode":true,"subject":"other"}`,
		body + ` {}`,
	} {
		response := testutil.Do(r, "PATCH", path+"/preferences", "a", invalid)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	for _, invalid := range []string{`null`, `[]`, `{"tenant_id":"other"}`, `{} {}`} {
		require.Equal(t, 400, testutil.Do(r, "POST", path, "a", invalid).Code)
	}
}

type accountVerifier struct{}

func (accountVerifier) Verify(ctx context.Context, token string) (authn.Principal, error) {
	p, err := (testutil.StaticVerifier{"a": testutil.TenantA, "b": testutil.TenantB}).Verify(ctx, token)
	p.Subject = "same-identity"
	return p, err
}

func TestSameIdentityHasIndependentTenantAccounts(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Verifier: accountVerifier{}, Modules: []platform.Module{mediconnect.New()}})
	a := testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}")
	require.Equal(t, 201, a.Code, a.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/mediconnect/account", "b", "").Code)
	b := testutil.Do(r, "POST", "/v1/mediconnect/account", "b", "{}")
	require.Equal(t, 201, b.Code, b.Body.String())
	require.NotEqual(t, a.Body.String(), b.Body.String())
}

func TestConcurrentCreationCommitsOneAccountAndEvent(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	responses := make(chan *httptest.ResponseRecorder, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { responses <- testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}") })
	}
	workers.Wait()
	close(responses)
	creates := 0
	var first string
	for response := range responses {
		require.Contains(t, []int{200, 201}, response.Code, response.Body.String())
		if response.Code == 201 {
			creates++
		}
		if first == "" {
			first = response.Body.String()
		}
		require.JSONEq(t, first, response.Body.String())
	}
	require.Equal(t, 1, creates)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = 'helivanta.in.mediconnect.account_created.v1'`).Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count)
		if err := tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type = 'mediconnect_accounts' AND action = 'insert'`).Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count)
		if err := tx.Raw(`SELECT count(*) FROM mediconnect_accounts`).Scan(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "missing subject context must deny even within the tenant")
		if err := tx.Exec(`SELECT set_config('app.actor_subject', 'user-a', true)`).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT count(*) FROM mediconnect_accounts`).Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count)
		return nil
	}))
}

type membershipResult struct {
	allowed bool
	err     error
}

func (m membershipResult) IsMember(context.Context, string, string) (bool, error) {
	return m.allowed, m.err
}

func TestMembershipDenialAndOutageFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		check  membershipResult
		status int
	}{
		{"denied", membershipResult{}, 403},
		{"unavailable", membershipResult{err: errors.New("synthetic outage")}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Membership: tc.check, Modules: []platform.Module{mediconnect.New()}})
			for _, method := range []string{"GET", "POST"} {
				response := testutil.Do(r, method, "/v1/mediconnect/account", "a", "{}")
				require.Equal(t, tc.status, response.Code, response.Body.String())
			}
		})
	}
}

func TestGlobalAPIToMediConnectToPostgres(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA, "peer": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"mediconnect": upstream.URL}})
	require.NoError(t, err)
	request := httptest.NewRequest("POST", "/v1/mediconnect/account", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer a")
	request.Header.Set("X-Tenant-ID", testutil.TenantB)
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	require.Equal(t, 201, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	own := testutil.Do(r, "GET", "/v1/mediconnect/account", "a", "")
	require.JSONEq(t, response.Body.String(), own.Body.String())
	request = httptest.NewRequest("GET", "/v1/mediconnect/account", nil)
	request.Header.Set("Authorization", "Bearer peer")
	request.Header.Set("X-User-ID", "user-a")
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)
	path := "/v1/mediconnect/account/link-requests/" + uuid.Must(uuid.NewV7()).String()
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"self"}`
	request = httptest.NewRequest("PUT", path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer a")
	request.Header.Set("X-Tenant-ID", testutil.TenantB)
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Contains(t, response.Body.String(), "pending_verification")
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "peer", "{}").Code)
	request = httptest.NewRequest("GET", path, nil)
	request.Header.Set("Authorization", "Bearer peer")
	request.Header.Set("X-User-ID", "user-a")
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)
}

func TestOutboxFailureRollsBackAccountAndAudit(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_reject_account_event", SQL: `
 CREATE FUNCTION reject_account_event() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic outbox failure'; END $$;
 CREATE TRIGGER reject_account_event BEFORE INSERT ON outbox_events
 FOR EACH ROW EXECUTE FUNCTION reject_account_event();`}}))
	response := testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}")
	require.Equal(t, 500, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "synthetic")
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/mediconnect/account", "a", "").Code)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type = 'mediconnect_accounts'`).Scan(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}

func TestAuditFailurePreventsAccountReadDisclosure(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	created := testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}")
	require.Equal(t, 201, created.Code, created.Body.String())
	path := "/v1/mediconnect/account/link-requests/" + uuid.Must(uuid.NewV7()).String()
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"self"}`
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "a", body).Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_reject_account_read_audit", SQL: `
 CREATE FUNCTION reject_account_read_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NEW.action = 'read' THEN RAISE EXCEPTION 'synthetic audit outage'; END IF;
 RETURN NEW;
 END $$;
 CREATE TRIGGER reject_account_read_audit BEFORE INSERT ON audit_events
 FOR EACH ROW EXECUTE FUNCTION reject_account_read_audit();`}}))
	for _, route := range []struct{ method, path, body string }{
		{"GET", path, ""},
		{"GET", "/v1/mediconnect/account/link-requests", ""},
		{"PUT", path, body},
	} {
		response := testutil.Do(r, route.method, route.path, "a", route.body)
		require.Equal(t, 500, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "patient_id")
		require.NotContains(t, response.Body.String(), "synthetic")
	}
	for _, method := range []string{"GET", "POST"} {
		response := testutil.Do(r, method, "/v1/mediconnect/account", "a", "{}")
		require.Equal(t, 500, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "locale")
		require.NotContains(t, response.Body.String(), "synthetic")
	}
}
