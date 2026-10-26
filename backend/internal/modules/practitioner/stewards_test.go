package practitioner_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStewardAppointmentRequiresIndependentNominationAcceptanceAndRetainsRevocation(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}, "foreign": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	management := "/v1/practitioners/steward-appointments/" + id
	own := "/v1/practitioners/my/steward-appointments/" + id
	reference := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, reference)
	nominated := testutil.Do(r, "PUT", management, "admin", body)
	require.Equal(t, 201, nominated.Code, nominated.Body.String())
	require.Contains(t, nominated.Body.String(), `"status":"nominated"`)
	require.Contains(t, nominated.Body.String(), `"effective":false`)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "admin", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "POST", own+"/accept", "admin", `{"expected_version":1}`).Code)
	accepted := testutil.Do(r, "POST", own+"/accept", "reviewer", `{"expected_version":1}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"status":"active"`)
	require.Contains(t, accepted.Body.String(), `"effective":true`)
	current := testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.JSONEq(t, accepted.Body.String(), current.Body.String())
	require.Equal(t, 404, testutil.Do(r, "GET", own, "peer", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", management, "foreign", "").Code)
	require.Equal(t, 401, testutil.Do(r, "GET", own, "unknown", "").Code)
	end := fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	revoked := testutil.Do(r, "POST", management+"/revoke", "admin", end)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"version":3`)
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.Contains(t, revoked.Body.String(), `"effective":false`)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "").Code)
	for _, op := range []struct{ method, path, token, body string }{
		{"POST", management + "/revoke", "admin", end},
		{"POST", own + "/accept", "reviewer", `{"expected_version":1}`},
		{"PUT", management, "admin", body},
	} {
		replay := testutil.Do(r, op.method, op.path, op.token, op.body)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.JSONEq(t, revoked.Body.String(), replay.Body.String())
	}
	history := testutil.Do(r, "GET", own+"/history", "reviewer", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var page struct {
		Data []struct {
			Version int64  `json:"version"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page.Data, 3)
	require.ElementsMatch(t, []int64{1, 2, 3}, []int64{page.Data[0].Version, page.Data[1].Version, page.Data[2].Version})
}

func TestStewardExpiryFailsClosedAndReplacementDoesNotImplicitlySupersedeLiveAuthority(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	nominate := func(until string) (string, string, string) {
		id := uuid.Must(uuid.NewV7()).String()
		management := "/v1/practitioners/steward-appointments/" + id
		own := "/v1/practitioners/my/steward-appointments/" + id
		body := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":%q}`, uuid.Must(uuid.NewV7()).String(), until)
		w := testutil.Do(r, "PUT", management, "admin", body)
		require.Equal(t, 201, w.Code, w.Body.String())
		return management, own, body
	}
	management, old, body := nominate("2098-01-01T00:00:00Z")
	require.Equal(t, 200, testutil.Do(r, "POST", old+"/accept", "reviewer", `{"expected_version":1}`).Code)
	_, pending, _ := nominate("2098-01-01T00:00:00Z")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_registry_clock_2100", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT '2100-01-01T00:00:00Z'::timestamptz $$;`}}))
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "").Code)
	expiredView := testutil.Do(r, "GET", old, "reviewer", "")
	require.Equal(t, 200, expiredView.Code, expiredView.Body.String())
	require.Contains(t, expiredView.Body.String(), `"effective":false`)
	require.Equal(t, 409, testutil.Do(r, "POST", pending+"/accept", "reviewer", `{"expected_version":1}`).Code)
	_, replacement, _ := nominate("2200-01-01T00:00:00Z")
	active := testutil.Do(r, "POST", replacement+"/accept", "reviewer", `{"expected_version":1}`)
	require.Equal(t, 200, active.Code, active.Body.String())
	require.Contains(t, active.Body.String(), `"effective":true`)
	retained := testutil.Do(r, "PUT", management, "admin", body)
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.Contains(t, retained.Body.String(), `"status":"expired"`)
	require.Contains(t, retained.Body.String(), `"version":3`)
	require.Contains(t, retained.Body.String(), `"effective":false`)
	replay := testutil.Do(r, "POST", old+"/accept", "reviewer", `{"expected_version":1}`)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, retained.Body.String(), replay.Body.String())
	_, overlap, _ := nominate("2300-01-01T00:00:00Z")
	require.Equal(t, 409, testutil.Do(r, "POST", overlap+"/accept", "reviewer", `{"expected_version":1}`).Code)
	end := fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 200, testutil.Do(r, "POST", replacement+"/withdraw", "reviewer", end).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", overlap+"/accept", "reviewer", `{"expected_version":1}`).Code)
	oldHistory := testutil.Do(r, "GET", old+"/history", "reviewer", "")
	require.Equal(t, 200, oldHistory.Code, oldHistory.Body.String())
	var history struct {
		Data []struct {
			Version int64  `json:"version"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(oldHistory.Body.Bytes(), &history))
	require.Len(t, history.Data, 3)
	require.ElementsMatch(t, []string{"nominated", "active", "expired"}, []string{history.Data[0].Status, history.Data[1].Status, history.Data[2].Status})
}

func TestConcurrentStewardAcceptanceCreatesOneTransitionAndTerminalReplayCannotChangeIntent(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	management := "/v1/practitioners/steward-appointments/" + id
	own := "/v1/practitioners/my/steward-appointments/" + id
	body := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 201, testutil.Do(r, "PUT", management, "admin", body).Code)
	responses := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { responses <- testutil.Do(r, "POST", own+"/accept", "reviewer", `{"expected_version":1}`).Code })
	}
	wg.Wait()
	close(responses)
	for code := range responses {
		require.Equal(t, 200, code)
	}
	history := testutil.Do(r, "GET", own+"/history", "reviewer", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(history.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	end := fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	stopped := testutil.Do(r, "POST", own+"/withdraw", "reviewer", end)
	require.Equal(t, 200, stopped.Code, stopped.Body.String())
	repeat := testutil.Do(r, "POST", own+"/withdraw", "reviewer", end)
	require.Equal(t, 200, repeat.Code, repeat.Body.String())
	require.JSONEq(t, stopped.Body.String(), repeat.Body.String())
	require.Equal(t, 409, testutil.Do(r, "POST", own+"/withdraw", "reviewer", fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 409, testutil.Do(r, "POST", management+"/revoke", "admin", end).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "").Code)
}

func TestStewardNominationRejectsSelfAppointmentForgedStateAndInvalidInputs(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	ref := uuid.Must(uuid.NewV7()).String()
	for _, tc := range []struct {
		body string
		code int
	}{
		{`null`, 400},
		{fmt.Sprintf(`{"subject":"user-admin","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, ref), 403},
		{fmt.Sprintf(`{"subject":"service:worker","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z","status":"active"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2000-01-01T00:00:00Z"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00.1234567Z"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00.0000000007Z"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00+24:00"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-02-30T00:00:00Z"}`, ref), 400},
		{fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.NewString()), 400},
		{fmt.Sprintf(`{"subject":"%s","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, strings.Repeat("x", 513), ref), 400},
	} {
		w := testutil.Do(r, "PUT", "/v1/practitioners/steward-appointments/"+uuid.Must(uuid.NewV7()).String(), "admin", tc.body)
		require.Equal(t, tc.code, w.Code, w.Body.String())
	}
	all := testutil.Do(r, "GET", "/v1/practitioners/steward-appointments", "admin", "")
	require.Equal(t, 200, all.Code, all.Body.String())
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(all.Body.Bytes(), &page))
	require.Empty(t, page.Data)
}

type stewardMembership struct{ state atomic.Int32 }

func (m *stewardMembership) IsMember(context.Context, string, string) (bool, error) {
	switch m.state.Load() {
	case 1:
		return false, nil
	case 2:
		return false, errors.New("synthetic membership dependency unavailable")
	default:
		return true, nil
	}
}
func TestAcceptedStewardStillRequiresCurrentTenantMembership(t *testing.T) {
	membership := &stewardMembership{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Membership: membership, Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	management := "/v1/practitioners/steward-appointments/" + id
	own := "/v1/practitioners/my/steward-appointments/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", management, "admin", fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", own+"/accept", "reviewer", `{"expected_version":1}`).Code)
	for _, state := range []int32{1, 2} {
		membership.state.Store(state)
		expected := 403
		if state == 2 {
			expected = 503
		}
		for _, path := range []string{own, own + "/history", "/v1/practitioners/my/steward-appointments", "/v1/practitioners/my/steward-appointment"} {
			w := testutil.Do(r, "GET", path, "reviewer", "")
			require.Equal(t, expected, w.Code, w.Body.String())
		}
	}
	membership.state.Store(0)
	require.Equal(t, 200, testutil.Do(r, "GET", "/v1/practitioners/my/steward-appointment", "reviewer", "").Code)
}

func TestStewardOwnRoutesNeverEnableProfessionalMasterScope(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage, practitioner.PermRecordManage}}, Modules: []platform.Module{practitioner.New()}})
	require.Equal(t, 201, testutil.Do(r, "PUT", "/v1/practitioners/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"given_name":"Private professional","professional_type":"physician"}`).Code)
	id := uuid.Must(uuid.NewV7()).String()
	management := "/v1/practitioners/steward-appointments/" + id
	own := "/v1/practitioners/my/steward-appointments/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", management, "admin", fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_steward_scope_isolation", SQL: `CREATE FUNCTION check_steward_master_isolation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type LIKE 'practitioner_steward%' AND EXISTS(SELECT 1 FROM practitioner_records) THEN RAISE EXCEPTION 'steward scope exposes professional master'; END IF; RETURN NEW; END $$; CREATE TRIGGER check_steward_master_isolation BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION check_steward_master_isolation();`}}))
	accepted := testutil.Do(r, "POST", own+"/accept", "reviewer", `{"expected_version":1}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	for _, path := range []string{own, own + "/history", "/v1/practitioners/my/steward-appointments", "/v1/practitioners/my/steward-appointment"} {
		w := testutil.Do(r, "GET", path, "reviewer", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "Private professional")
	}
}

func TestStewardAuditOutageBlocksDisclosuresAndRollsBackTermination(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	management := "/v1/practitioners/steward-appointments/" + id
	own := "/v1/practitioners/my/steward-appointments/" + id
	body := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 201, testutil.Do(r, "PUT", management, "admin", body).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", own+"/accept", "reviewer", `{"expected_version":1}`).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_steward_read_audit_failure", SQL: `CREATE FUNCTION reject_steward_read_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type LIKE 'practitioner_steward%' THEN RAISE EXCEPTION 'synthetic steward audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_steward_read_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_steward_read_audit();`}}))
	end := fmt.Sprintf(`{"expected_version":2,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	for _, op := range []struct{ method, path, token, body string }{
		{"GET", management, "admin", ""}, {"GET", management + "/history", "admin", ""}, {"GET", "/v1/practitioners/steward-appointments", "admin", ""},
		{"GET", own, "reviewer", ""}, {"GET", own + "/history", "reviewer", ""}, {"GET", "/v1/practitioners/my/steward-appointments", "reviewer", ""}, {"GET", "/v1/practitioners/my/steward-appointment", "reviewer", ""},
		{"PUT", management, "admin", body}, {"POST", own + "/accept", "reviewer", `{"expected_version":1}`}, {"POST", own + "/withdraw", "reviewer", end}, {"POST", management + "/revoke", "admin", end},
	} {
		w := testutil.Do(r, op.method, op.path, op.token, op.body)
		require.Equal(t, 500, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "user-reviewer")
		require.NotContains(t, w.Body.String(), "synthetic steward audit outage")
		require.NotContains(t, w.Body.String(), `"data"`)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT set_config('app.actor_subject','user-admin',true),set_config('app.practitioner_steward_manage','true',true)").Error; e != nil {
			return e
		}
		var row struct {
			Status  string
			Version int64
		}
		if e := tx.Table("practitioner_steward_appointments").Where("id=?", id).Take(&row).Error; e != nil {
			return e
		}
		require.Equal(t, "active", row.Status)
		require.EqualValues(t, 2, row.Version)
		var events int64
		if e := tx.Table("practitioner_steward_events").Where("appointment_id=?", id).Count(&events).Error; e != nil {
			return e
		}
		require.EqualValues(t, 2, events)
		return nil
	}))
}

func TestStewardDatabaseGuardsPreventAdminAcceptanceAndHistoryRewrites(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/practitioners/steward-appointments/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "admin", fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())).Code)
	for _, sql := range []string{
		"UPDATE practitioner_steward_appointments SET status='active',version=2 WHERE tenant_id=? AND id=?",
		"UPDATE practitioner_steward_appointments SET subject='user-admin' WHERE tenant_id=? AND id=?",
		"DELETE FROM practitioner_steward_appointments WHERE tenant_id=? AND id=?",
		"DELETE FROM practitioner_steward_events WHERE tenant_id=? AND appointment_id=?",
		"UPDATE practitioner_steward_events SET status='active' WHERE tenant_id=? AND appointment_id=?",
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if e := tx.Exec("SELECT set_config('app.actor_subject','user-admin',true),set_config('app.practitioner_steward_manage','true',true)").Error; e != nil {
				return e
			}
			return tx.Exec(sql, testutil.TenantA, id).Error
		})
		require.Error(t, err)
	}
	retained := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.Contains(t, retained.Body.String(), `"status":"nominated"`)
	require.Contains(t, retained.Body.String(), `"effective":false`)
}

func TestEveryPractitionerRouteRejectsStaleWorkspaceBeforeDomainWork(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage, practitioner.PermRecordManage, practitioner.PermRecordRead, practitioner.PermEvidenceManage, practitioner.PermEvidenceRead, practitioner.PermRegistrationReview, practitioner.PermQualificationReview, practitioner.PermSpecialtyManage}}, Modules: []platform.Module{practitioner.New()}})
	countAudit := func() int64 {
		var n int64
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Table("audit_events").Count(&n).Error }))
		return n
	}
	before := countAudit()
	checked := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/practitioners") {
			continue
		}
		path := route.Path
		for _, param := range []string{":practitioner", ":registration", ":revision", ":appointment", ":file", ":revocation", ":retry", ":review", ":specialty", ":qualification", ":retirement"} {
			path = strings.ReplaceAll(path, param, uuid.Must(uuid.NewV7()).String())
		}
		req := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 409, w.Code, route.Method+" "+path+" "+w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		checked++
	}
	require.Equal(t, 50, checked)
	require.Equal(t, before, countAudit())
}

func TestStewardAcceptsJavaScriptISOTimestampsAndReplaysEquivalentInstants(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {practitioner.PermStewardManage}}, Modules: []platform.Module{practitioner.New()}})
	path := "/v1/practitioners/steward-appointments/" + uuid.Must(uuid.NewV7()).String()
	ref := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T00:00:00.000Z"}`, ref)
	saved := testutil.Do(r, "PUT", path, "admin", body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	equivalent := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":%q,"valid_until":"2098-01-01T05:30:00+05:30"}`, ref)
	replay := testutil.Do(r, "PUT", path, "admin", equivalent)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	require.Contains(t, replay.Body.String(), `"valid_until":"2098-01-01T00:00:00Z"`)
}
