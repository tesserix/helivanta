package mediconnect_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestAccessibilityPreferencesRoundTrip(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	const path = "/v1/mediconnect/account"
	require.Equal(t, 201, testutil.Do(r, "POST", path, "a", "{}").Code)
	body := `{"expected_version":1,"locale":"hi-IN","preferred_channel":"email","low_data_mode":true,"accessibility":{"text_size":"extra_large","contrast":"high","motion":"reduced"}}`
	updated := testutil.Do(r, "PATCH", path+"/preferences", "a", body)
	require.Equal(t, 200, updated.Code, updated.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &row))
	require.Equal(t, map[string]any{"text_size": "extra_large", "contrast": "high", "motion": "reduced"}, row["accessibility"])
	require.Equal(t, float64(2), row["version"])
	require.Equal(t, "no-store", updated.Header().Get("Cache-Control"))
	read := testutil.Do(r, "GET", path, "a", "")
	require.Equal(t, 200, read.Code)
	require.JSONEq(t, updated.Body.String(), read.Body.String())
}

func accountBody(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var row map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &row))
	return row
}

func TestAccessibilityLegacyPreservationValidationAndReset(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA, "peer": testutil.TenantA, "b": testutil.TenantB}, Modules: []platform.Module{mediconnect.New()}})
	const path = "/v1/mediconnect/account"
	defaults := map[string]any{"text_size": "system", "contrast": "system", "motion": "system"}
	chosen := map[string]any{"text_size": "large", "contrast": "high", "motion": "reduced"}
	created := testutil.Do(r, "POST", path, "a", "{}")
	require.Equal(t, 201, created.Code)
	require.Equal(t, defaults, accountBody(t, created)["accessibility"])
	const body = `{"expected_version":1,"locale":"en-IN","preferred_channel":"none","low_data_mode":false,"accessibility":{"text_size":"large","contrast":"high","motion":"reduced"}}`
	for _, token := range []string{"peer", "b"} {
		require.Equal(t, 404, testutil.Do(r, "PATCH", path+"/preferences", token, body).Code)
		require.Equal(t, 201, testutil.Do(r, "POST", path, token, "{}").Code)
	}
	require.Equal(t, 200, testutil.Do(r, "PATCH", path+"/preferences", "a", body).Code)
	for i, extra := range []string{"", `,"accessibility":null`} {
		legacy := fmt.Sprintf(`{"expected_version":%d,"locale":"ta-IN","preferred_channel":"email","low_data_mode":true%s}`, i+2, extra)
		updated := testutil.Do(r, "PATCH", path+"/preferences", "a", legacy)
		require.Equal(t, 200, updated.Code, updated.Body.String())
		require.Equal(t, chosen, accountBody(t, updated)["accessibility"])
	}
	before := testutil.Do(r, "GET", path, "a", "")
	for _, invalid := range []string{`{}`, `[]`, `true`, `"large"`, `{"text_size":"large"}`, `{"text_size":"small","contrast":"high","motion":"reduced"}`, `{"text_size":"large","contrast":"low","motion":"reduced"}`, `{"text_size":"large","contrast":"high","motion":"full"}`, `{"text_size":null,"contrast":"high","motion":"reduced"}`, `{"text_size":"large","contrast":"high","motion":"reduced","patient_id":"claim"}`} {
		candidate := fmt.Sprintf(`{"expected_version":4,"locale":"en-IN","preferred_channel":"none","low_data_mode":false,"accessibility":%s}`, invalid)
		response := testutil.Do(r, "PATCH", path+"/preferences", "a", candidate)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	require.Equal(t, 409, testutil.Do(r, "PATCH", path+"/preferences", "a", body).Code)
	require.JSONEq(t, before.Body.String(), testutil.Do(r, "GET", path, "a", "").Body.String())
	for _, token := range []string{"peer", "b"} {
		require.Equal(t, defaults, accountBody(t, testutil.Do(r, "GET", path, token, ""))["accessibility"])
	}
	reset := `{"expected_version":4,"locale":"en-IN","preferred_channel":"none","low_data_mode":false,"accessibility":{"text_size":"system","contrast":"system","motion":"system"}}`
	updated := testutil.Do(r, "PATCH", path+"/preferences", "a", reset)
	require.Equal(t, 200, updated.Code, updated.Body.String())
	require.Equal(t, defaults, accountBody(t, updated)["accessibility"])
	require.Equal(t, float64(5), accountBody(t, updated)["version"])
	require.JSONEq(t, updated.Body.String(), testutil.Do(r, "POST", path, "a", "{}").Body.String())
}

func TestAccessibilityCompetingWritesAreAtomic(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	const path = "/v1/mediconnect/account"
	require.Equal(t, 201, testutil.Do(r, "POST", path, "a", "{}").Code)
	bodies := []string{
		`{"expected_version":1,"locale":"en-IN","preferred_channel":"none","low_data_mode":false,"accessibility":{"text_size":"large","contrast":"system","motion":"system"}}`,
		`{"expected_version":1,"locale":"hi-IN","preferred_channel":"email","low_data_mode":true,"accessibility":{"text_size":"extra_large","contrast":"high","motion":"reduced"}}`,
	}
	responses := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() { responses <- testutil.Do(r, "PATCH", path+"/preferences", "a", bodies[i%2]) })
	}
	wg.Wait()
	close(responses)
	var winner *httptest.ResponseRecorder
	conflicts := 0
	for response := range responses {
		if response.Code == 200 {
			require.Nil(t, winner)
			winner = response
		} else {
			require.Equal(t, 409, response.Code, response.Body.String())
			conflicts++
		}
	}
	require.NotNil(t, winner)
	require.Equal(t, 7, conflicts)
	require.Equal(t, float64(2), accountBody(t, winner)["version"])
	require.JSONEq(t, winner.Body.String(), testutil.Do(r, "GET", path, "a", "").Body.String())
	row := accountBody(t, winner)
	if row["locale"] == "en-IN" {
		require.Equal(t, map[string]any{"text_size": "large", "contrast": "system", "motion": "system"}, row["accessibility"])
	} else {
		require.Equal(t, "hi-IN", row["locale"])
		require.Equal(t, map[string]any{"text_size": "extra_large", "contrast": "high", "motion": "reduced"}, row["accessibility"])
	}
}

func TestAccessibilityAuditFailureRollsBackWholeUpdate(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	const path = "/v1/mediconnect/account"
	before := testutil.Do(r, "POST", path, "a", "{}")
	require.Equal(t, 201, before.Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_reject_preference_audit", SQL: `
 CREATE FUNCTION reject_preference_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.action='update' AND NEW.resource_type='mediconnect_accounts' THEN RAISE EXCEPTION 'synthetic audit outage'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_preference_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_preference_audit();`}}))
	body := `{"expected_version":1,"locale":"hi-IN","preferred_channel":"email","low_data_mode":true,"accessibility":{"text_size":"large","contrast":"high","motion":"reduced"}}`
	response := testutil.Do(r, "PATCH", path+"/preferences", "a", body)
	require.Equal(t, 500, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "synthetic")
	require.NotContains(t, response.Body.String(), "accessibility")
	require.JSONEq(t, before.Body.String(), testutil.Do(r, "GET", path, "a", "").Body.String())
}

type legacyMediConnect struct{ *mediconnect.Module }

func (m legacyMediConnect) Migrations() []tenantdb.Migration { return m.Module.Migrations()[:2] }

func TestAccessibilityMigrationPreservesExistingAccount(t *testing.T) {
	module := mediconnect.New()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"a": testutil.TenantA}, Modules: []platform.Module{legacyMediConnect{module}}})
	id := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject', ?, true)", "user-a").Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO mediconnect_accounts(id,tenant_id,subject,locale,preferred_channel,low_data_mode,version) VALUES (?,?,'user-a','hi-IN','email',true,7)`, id, testutil.TenantA).Error
	}))
	require.NoError(t, db.Migrate(ctx, module.Migrations()))
	response := testutil.Do(r, "GET", "/v1/mediconnect/account", "a", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	row := accountBody(t, response)
	require.Equal(t, id.String(), row["id"])
	require.Equal(t, "hi-IN", row["locale"])
	require.Equal(t, "email", row["preferred_channel"])
	require.Equal(t, true, row["low_data_mode"])
	require.Equal(t, float64(7), row["version"])
	require.Equal(t, map[string]any{"text_size": "system", "contrast": "system", "motion": "system"}, row["accessibility"])
	for _, column := range []string{"accessibility_text_size", "accessibility_contrast", "accessibility_motion"} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.actor_subject', ?, true)", "user-a").Error; err != nil {
				return err
			}
			return tx.Table("mediconnect_accounts").Where("id = ?", id).Update(column, "invalid").Error
		})
		require.Error(t, err)
	}
	require.JSONEq(t, response.Body.String(), testutil.Do(r, "GET", "/v1/mediconnect/account", "a", "").Body.String())
}
