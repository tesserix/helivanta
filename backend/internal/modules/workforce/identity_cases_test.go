package workforce_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestIdentityCaseDraftRevisionsAndReplay(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-DRAFT")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	path := "/identity-staff/" + staff + "/checks/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{
		"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0,
		"kind": "identity", "policy_reference": "SYN-POLICY-1",
		"check_reference": "Synthetic manual identity check",
		"observed_on":     "2020-01-01", "valid_until": "2100-01-01T00:00:00Z",
		"identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"],
	}
	code, first := h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, first)
	require.Equal(t, "draft", first["status"])
	require.Equal(t, float64(1), first["version"])
	require.Equal(t, "SYN-POLICY-1", first["policy_reference"])
	require.Equal(t, "2020-01-01", first["observed_on"])
	require.Equal(t, "2100-01-01T00:00:00Z", first["valid_until"])
	require.Equal(t, source["identity_hash"], first["identity_hash"])
	code, replay := h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, replay)
	require.Equal(t, first, replay)

	edit := with(input, map[string]any{
		"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1,
		"policy_reference": "SYN-POLICY-2",
	})
	code, second := h.call("PUT", path, "hr-manager", edit)
	require.Equal(t, 200, code, second)
	require.Equal(t, float64(2), second["version"])
	require.Equal(t, "SYN-POLICY-2", second["policy_reference"])
	code, replay = h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, replay)
	require.Equal(t, first, replay, "an old command returns its immutable receipt")
	code, conflict := h.call("PUT", path, "hr-manager", with(input, map[string]any{"policy_reference": "different"}))
	require.Equal(t, 409, code, conflict)
	code, stale := h.call("PUT", path, "hr-manager", with(edit, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String()}))
	require.Equal(t, 409, code, stale)
	code, latest := h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, latest)
	require.Equal(t, second, latest)
}

func TestIdentityCaseSourceCapabilityProjection(t *testing.T) {
	h, authority, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-CAPABILITY")
	path := "/identity-staff/" + staff
	code, plain := h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, plain)
	require.NotContains(t, plain, "manage_checks_allowed", "the existing unprojected contract stays unchanged")
	code, manager := h.call("GET", path+"?projection=identity_drafts", "hr-manager", nil)
	require.Equal(t, 200, code, manager)
	require.Equal(t, true, manager["manage_checks_allowed"])
	authority.permission = "workforce.identity_checks.review"
	code, reviewer := h.call("GET", path+"?projection=identity_drafts", "hr-manager", nil)
	require.Equal(t, 200, code, reviewer)
	require.Equal(t, false, reviewer["manage_checks_allowed"])
	for _, query := range []string{"?projection=unknown", "?projection=identity_drafts&projection=identity_drafts", "?projection=identity_drafts&subject=someone"} {
		code, denied := h.call("GET", path+query, "hr-manager", nil)
		require.Equal(t, 400, code, denied)
		require.NotContains(t, denied, "identity_hash")
	}
}

func TestIdentityCaseDatabasePreservesPrivateHistoryAndDraftState(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-DATABASE")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/identity-staff/" + staff + "/checks/" + id
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "kind": "identity", "policy_reference": "SYN-POLICY", "check_reference": "Synthetic check", "observed_on": "2020-01-01", "valid_until": "2100-01-01T00:00:00Z", "identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"]}
	code, first := h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, first)
	scoped := func(tenant string, action func(*gorm.DB) error) error {
		return h.db.WithTenantCommand(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-hr-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_identity_case','manage',true)`, h.hospital, staff).Error; err != nil {
				return err
			}
			return action(tx)
		})
	}
	for _, table := range []string{"workforce_identity_cases", "workforce_identity_case_history"} {
		require.NoError(t, scoped(testutil.TenantB, func(tx *gorm.DB) error {
			var count int64
			if err := tx.Table(table).Where("id=?", id).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, "foreign tenant cannot read case or retained history")
			return nil
		}))
		require.NoError(t, scoped(testutil.TenantA, func(tx *gorm.DB) error {
			deleted := tx.Table(table).Where("id=?", id).Delete(map[string]any{})
			require.Zero(t, deleted.RowsAffected, "private history cannot be erased")
			return deleted.Error
		}))
	}
	for _, query := range []string{
		`UPDATE workforce_identity_case_history SET policy_reference='forged' WHERE id=?`,
		`INSERT INTO workforce_identity_case_history SELECT * FROM workforce_identity_cases WHERE id=?`,
	} {
		err := scoped(testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(query, id).Error })
		require.ErrorContains(t, err, "owner-maintained and immutable")
	}
	for _, query := range []string{
		`UPDATE workforce_identity_cases SET status='approved',version=version+1,revision_id=? WHERE id=?`,
		`UPDATE workforce_identity_cases SET version=99,revision_id=? WHERE id=?`,
		`UPDATE workforce_identity_cases SET employment_epoch=99,version=version+1,revision_id=? WHERE id=?`,
	} {
		require.Error(t, scoped(testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(query, uuid.Must(uuid.NewV7()), id).Error }))
	}
	code, latest := h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, latest)
	require.Equal(t, first, latest)
	require.NotContains(t, latest, "input_hash")
}

func TestIdentityCaseRejectsStaleWorkspacePreconditions(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-WORKSPACE")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	path := "/identity-staff/" + staff + "/checks/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "kind": "identity", "policy_reference": "SYN-PRIVATE-POLICY", "check_reference": "Synthetic private check", "observed_on": "2020-01-01", "valid_until": "2100-01-01T00:00:00Z", "identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"]}
	code, first := h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, first)
	input = with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1})
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	for _, header := range []struct {
		key    string
		values []string
	}{
		{"X-Helivanta-Expected-Tenant", nil},
		{"X-Helivanta-Expected-Subject", nil},
		{"X-Helivanta-Expected-Tenant", []string{testutil.TenantB}},
		{"X-Helivanta-Expected-Tenant", []string{"invalid"}},
		{"X-Helivanta-Expected-Tenant", []string{testutil.TenantA, testutil.TenantA}},
		{"X-Helivanta-Expected-Subject", []string{"another-subject"}},
		{"X-Helivanta-Expected-Subject", []string{"user-hr-manager", "user-hr-manager"}},
	} {
		for _, method := range []string{"GET", "PUT"} {
			request := httptest.NewRequest(method, h.base+path, strings.NewReader(string(raw)))
			request.Header.Set("Authorization", "Bearer hr-manager")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			request.Header.Set("X-Helivanta-Expected-Subject", "user-hr-manager")
			request.Header.Del(header.key)
			for _, value := range header.values {
				request.Header.Add(header.key, value)
			}
			response := httptest.NewRecorder()
			h.handler.ServeHTTP(response, request)
			require.Equal(t, 409, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "SYN-PRIVATE-POLICY")
		}
	}
	code, latest := h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, latest)
	require.Equal(t, first, latest)
}

func TestIdentityCaseAuthorityAndGuardRollback(t *testing.T) {
	h, authority, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-AUTHORITY")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	path := "/identity-staff/" + staff + "/checks/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "kind": "background", "policy_reference": "SYN-PRIVATE-POLICY", "check_reference": "Synthetic confidential check reference", "observed_on": "2020-01-01", "valid_until": "2100-01-01T00:00:00Z", "identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"]}
	code, first := h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 200, code, first)
	for _, user := range []string{"admin", "viewer", "foreign"} {
		for _, method := range []string{"GET", "PUT"} {
			code, denied := h.call(method, path, user, input)
			require.Equal(t, 403, code, denied)
			require.NotContains(t, denied, "policy_reference")
		}
	}
	update := with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "policy_reference": "SYN-NEXT-POLICY"})
	authority.permission = "workforce.identity_checks.review"
	code, read := h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, read)
	require.Equal(t, first, read)
	code, denied := h.call("PUT", path, "hr-manager", update)
	require.Equal(t, 403, code, denied)
	authority.failure = errors.New("synthetic authority outage")
	code, denied = h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 503, code, denied)
	require.NotContains(t, denied, "policy_reference")
	authority.failure = nil
	authority.permission = "workforce.identity_checks.manage"
	authority.after = authz.ErrScopedCommandDenied
	code, denied = h.call("PUT", path, "hr-manager", update)
	require.Equal(t, 403, code, denied)
	require.NotContains(t, denied, "SYN-NEXT-POLICY")
	authority.after = nil
	code, read = h.call("GET", path, "hr-manager", nil)
	require.Equal(t, 200, code, read)
	require.Equal(t, first, read, "a post-command guard denial rolls back the head and receipt")
	code, read = h.call("PUT", path, "hr-manager", update)
	require.Equal(t, 200, code, read, "the rolled-back command can be retried")
	require.Equal(t, float64(2), read["version"])
	code, read = h.call("PUT", path, "hr-manager", update)
	require.Equal(t, 200, code, read)
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var payloads []string
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.workforce.identity_case_changed.v1").Pluck("payload", &payloads).Error; err != nil {
			return err
		}
		require.Len(t, payloads, 2, "one committed event per revision, none for rollback or replay")
		for _, payload := range payloads {
			require.NotContains(t, payload, "SYN-PRIVATE-POLICY")
			require.NotContains(t, payload, "SYN-NEXT-POLICY")
			require.NotContains(t, payload, "confidential")
			require.NotContains(t, payload, "SYN-CASE-AUTHORITY")
		}
		return nil
	}))
}

func TestIdentityCaseDraftSourceAndRequiredFields(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-SOURCE")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	path := "/identity-staff/" + staff + "/checks/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "kind": "identity", "policy_reference": "SYN-POLICY", "check_reference": "Synthetic check", "observed_on": "2020-01-01", "valid_until": "2100-01-01T00:00:00Z", "identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"]}
	for _, change := range []map[string]any{
		{"expected_version": nil}, {"expected_version": -1}, {"command_id": uuid.NewString()},
		{"kind": "verified"}, {"policy_reference": " "}, {"policy_reference": strings.Repeat("x", 301)},
		{"check_reference": ""}, {"check_reference": "invalid\x00reference"}, {"observed_on": "2026-02-30"},
		{"observed_on": "2200-01-01"}, {"valid_until": "2019-01-01T00:00:00Z"}, {"identity_hash": strings.Repeat("z", 64)},
		{"employment_epoch": 0}, {"unexpected_source": "forged"},
	} {
		code, denied := h.call("PUT", path, "hr-manager", with(input, change))
		require.Equal(t, 400, code, denied)
	}
	for _, change := range []map[string]any{{"identity_hash": strings.Repeat("a", 64)}, {"employment_epoch": 2}} {
		code, denied := h.call("PUT", path, "hr-manager", with(input, change))
		require.Equal(t, 409, code, denied)
		require.Equal(t, "identity_source_changed", denied["error"])
	}
	code, person := h.call("GET", "/staff/"+staff, "admin", nil)
	require.Equal(t, 200, code, person)
	fields := map[string]any{"employee_code": person["employee_code"], "display_name": person["display_name"], "job_category": person["job_category"], "employment_status": "inactive"}
	code, out := h.call("PUT", "/staff/"+staff, "admin", fields)
	require.Equal(t, 200, code, out)
	code, out = h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 409, code, out)
	require.Equal(t, "staff_inactive", out["error"])
	fields["employment_status"] = "active"
	code, out = h.call("PUT", "/staff/"+staff, "admin", fields)
	require.Equal(t, 200, code, out)
	code, out = h.call("PUT", path, "hr-manager", input)
	require.Equal(t, 409, code, out)
	require.Equal(t, "identity_source_changed", out["error"])
	code, current := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, current)
	require.Equal(t, float64(3), current["employment_epoch"])
	code, out = h.call("PUT", path, "hr-manager", with(input, map[string]any{"employment_epoch": current["employment_epoch"], "identity_hash": current["identity_hash"]}))
	require.Equal(t, 200, code, out)
	require.Equal(t, float64(3), out["employment_epoch"])
}

func TestIdentityCaseListAndHistoryAreScopedAndBounded(t *testing.T) {
	h, _, _ := newIdentitySourceHarness(t)
	staff := h.staff("SYN-CASE-LIST")
	code, source := h.call("GET", "/identity-staff/"+staff, "hr-manager", nil)
	require.Equal(t, 200, code, source)
	base := "/identity-staff/" + staff + "/checks"
	ids := make(map[string]bool)
	var first string
	var input map[string]any
	for _, kind := range []string{"identity", "background", "photograph"} {
		id := uuid.Must(uuid.NewV7()).String()
		ids[id] = true
		input = map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "kind": kind, "policy_reference": "SYN-POLICY", "check_reference": "Synthetic check", "observed_on": "2020-01-01", "valid_until": "2100-01-01T00:00:00Z", "identity_hash": source["identity_hash"], "employment_epoch": source["employment_epoch"]}
		code, out := h.call("PUT", base+"/"+id, "hr-manager", input)
		require.Equal(t, 200, code, out)
		first = id
	}
	code, out := h.call("PUT", base+"/"+first, "hr-manager", with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "policy_reference": "SYN-POLICY-EDIT"}))
	require.Equal(t, 200, code, out)
	readPage := func(path string) ([]map[string]any, string, bool) {
		t.Helper()
		code, body := h.do("GET", h.base+path, "hr-manager", "")
		require.Equal(t, 200, code, body)
		var page struct {
			Data []map[string]any `json:"data"`
			Page struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &page), body)
		require.Len(t, page.Data, 1)
		return page.Data, page.Page.NextCursor, page.Page.HasMore
	}
	cursor := ""
	for i := range 3 {
		rows, next, more := readPage(base + "?limit=1&cursor=" + url.QueryEscape(cursor))
		id := rows[0]["id"].(string)
		require.True(t, ids[id], "each current case appears once")
		delete(ids, id)
		require.Equal(t, i < 2, more)
		cursor = next
	}
	require.Empty(t, ids)
	rows, cursor, more := readPage(base + "/" + first + "/history?limit=1")
	require.True(t, more)
	require.Equal(t, float64(2), rows[0]["version"])
	rows, _, more = readPage(base + "/" + first + "/history?limit=1&cursor=" + url.QueryEscape(cursor))
	require.False(t, more)
	require.Equal(t, float64(1), rows[0]["version"])
	code, body := h.do("GET", h.base+base, "admin", "")
	require.Equal(t, 403, code, body)
	require.NotContains(t, body, "SYN-POLICY")
	other := h.staff("SYN-CASE-OTHER")
	code, body = h.do("GET", h.base+"/identity-staff/"+other+"/checks/"+first, "hr-manager", "")
	require.Equal(t, 404, code, body)
}
