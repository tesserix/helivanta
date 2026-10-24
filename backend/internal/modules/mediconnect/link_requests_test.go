package mediconnect_test

import (
	"encoding/json"
	"gorm.io/gorm"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestLinkRequestIsUnverifiedAndImmutable(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA, "peer": testutil.TenantA, "b": testutil.TenantB}, Modules: []platform.Module{mediconnect.New()}})
	for _, token := range []string{"a", "peer", "b"} {
		require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", token, "{}").Code)
	}
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + id
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"self"}`
	created := testutil.Do(r, "PUT", path, "a", body)
	require.Equal(t, 202, created.Code, created.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &row))
	require.Equal(t, "pending_verification", row["status"])
	require.Equal(t, float64(1), row["version"])
	require.NotContains(t, row, "grants")
	replay := testutil.Do(r, "PUT", path, "a", body)
	require.Equal(t, 200, replay.Code)
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	changed := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"33333333-3333-4333-8333-333333333333","relationship":"self"}`
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "a", changed).Code)
	require.Equal(t, 404, testutil.Do(r, "PUT", path, "peer", body).Code)
	// The same opaque ID in a different tenant is an independent claim.
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "b", body).Code)
}

func TestLinkRequestsPageAndWithdrawalStayWithinTheAccount(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA, "peer": testutil.TenantA, "b": testutil.TenantB}, Modules: []platform.Module{mediconnect.New()}})
	const base = "/v1/mediconnect/account/link-requests"
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"child"}`
	for _, token := range []string{"a", "peer", "b"} {
		require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", token, "{}").Code)
	}
	ids := []string{}
	for range 3 {
		id := uuid.Must(uuid.NewV7()).String()
		ids = append(ids, id)
		require.Equal(t, 202, testutil.Do(r, "PUT", base+"/"+id, "a", body).Code)
	}
	page := testutil.Do(r, "GET", base+"?limit=2", "a", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	require.Equal(t, "no-store", page.Header().Get("Cache-Control"))
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		}
		Page struct {
			HasMore bool   `json:"has_more"`
			Cursor  string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data, 2)
	require.True(t, envelope.Page.HasMore)
	next := testutil.Do(r, "GET", base+"?limit=2&cursor="+envelope.Page.Cursor, "a", "")
	require.Equal(t, 200, next.Code)
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data, 1)
	require.False(t, envelope.Page.HasMore)
	path := base + "/" + ids[0]
	own := testutil.Do(r, "GET", path, "a", "")
	require.Equal(t, 200, own.Code)
	for _, token := range []string{"peer", "b"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path, token, "").Code)
		require.Equal(t, 404, testutil.Do(r, "POST", path+"/withdraw", token, `{"expected_version":1}`).Code)
		empty := testutil.Do(r, "GET", base, token, "")
		require.Equal(t, 200, empty.Code)
		require.Contains(t, empty.Body.String(), `"data":[]`)
	}
	require.Equal(t, 409, testutil.Do(r, "POST", path+"/withdraw", "a", `{"expected_version":2}`).Code)
	withdrawn := testutil.Do(r, "POST", path+"/withdraw", "a", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"status":"withdrawn"`)
	require.Contains(t, withdrawn.Body.String(), `"version":2`)
	retry := testutil.Do(r, "POST", path+"/withdraw", "a", `{"expected_version":1}`)
	require.Equal(t, 200, retry.Code)
	require.JSONEq(t, withdrawn.Body.String(), retry.Body.String())
	replay := testutil.Do(r, "PUT", path, "a", body)
	require.Equal(t, 200, replay.Code)
	require.JSONEq(t, withdrawn.Body.String(), replay.Body.String())
	require.Equal(t, 400, testutil.Do(r, "GET", base+"?limit=201", "a", "").Code)
}

func TestLinkRequestValidationAndStoredHistory(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	const base = "/v1/mediconnect/account/link-requests"
	id := uuid.Must(uuid.NewV7()).String()
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"self"}`
	require.Equal(t, 404, testutil.Do(r, "PUT", base+"/"+id, "a", body).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", base, "a", "").Code)
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}").Code)
	for _, invalid := range []string{`null`, `{}`, body + ` {}`, `{"status":"approved"}`, `{"hospital_id":"invalid"}`, `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"doctor"}`} {
		require.Equal(t, 400, testutil.Do(r, "PUT", base+"/"+id, "a", invalid).Code)
	}
	require.Equal(t, 404, testutil.Do(r, "PUT", base+"/"+uuid.NewString(), "a", body).Code)
	require.Equal(t, 202, testutil.Do(r, "PUT", base+"/"+id, "a", body).Code)
	require.Equal(t, 400, testutil.Do(r, "POST", base+"/"+id+"/withdraw", "a", `{}`).Code)
	for _, sql := range []string{
		`UPDATE mediconnect_link_requests SET patient_id = '33333333-3333-4333-8333-333333333333' WHERE id = ?`,
		`UPDATE mediconnect_link_requests SET status = 'active' WHERE id = ?`,
		`DELETE FROM mediconnect_link_requests WHERE id = ?`,
	} {
		require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject', 'user-a', true)`).Error; err != nil {
				return err
			}
			return tx.Exec(sql, id).Error
		}))
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM mediconnect_link_requests`).Scan(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "missing subject context denies request reads")
		return nil
	}))
}

func TestConcurrentLinkRequestAndWithdrawalAreIdempotent(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "a", "{}").Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + id
	body := `{"hospital_id":"11111111-1111-4111-8111-111111111111","patient_id":"22222222-2222-4222-8222-222222222222","relationship":"self"}`
	for _, phase := range []struct{ method, path, body string }{
		{"PUT", path, body}, {"POST", path + "/withdraw", `{"expected_version":1}`},
	} {
		responses := make(chan *httptest.ResponseRecorder, 6)
		var workers sync.WaitGroup
		for range 6 {
			workers.Go(func() { responses <- testutil.Do(r, phase.method, phase.path, "a", phase.body) })
		}
		workers.Wait()
		close(responses)
		first := ""
		for response := range responses {
			require.Contains(t, []int{200, 202}, response.Code, response.Body.String())
			if first == "" {
				first = response.Body.String()
			}
			require.JSONEq(t, first, response.Body.String())
		}
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var counts []struct {
			Action string
			N      int64
		}
		if err := tx.Raw(`SELECT action, count(*) AS n FROM audit_events WHERE resource_type='mediconnect_link_requests' AND resource_id=? AND action IN ('insert','update') GROUP BY action ORDER BY action`, id).Scan(&counts).Error; err != nil {
			return err
		}
		require.Len(t, counts, 2)
		require.Equal(t, "insert", counts[0].Action)
		require.Equal(t, int64(1), counts[0].N)
		require.Equal(t, "update", counts[1].Action)
		require.Equal(t, int64(1), counts[1].N)
		return nil
	}))
}
