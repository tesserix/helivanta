package emergency_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestBaseValidationPaginationAndIdempotentAdministration(t *testing.T) {
	f := setup(t)
	for _, body := range []string{`null`, `{}`, `{"name":"x","timezone":"wrong","policy_reference":"SYN","contact_retention_days":30}`, `{"name":"x","timezone":"Asia/Kolkata","policy_reference":"bad policy","contact_retention_days":30}`} {
		w := testutil.Do(f.r, "PUT", f.path(), "admin", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for range 2 {
		w := testutil.Do(f.r, "PUT", f.path(), "admin", baseBody())
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w := testutil.Do(f.r, "GET", "/v1/emergency/bases?limit=1", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Synthetic Odisha Base")
	w = testutil.Do(f.r, "PUT", f.path()+"/access/user-reader", "admin", `{"level":"invalid"}`)
	require.Equal(t, 400, w.Code)
	for range 2 {
		w = testutil.Do(f.r, "PUT", f.path()+"/access/user-reader", "admin", `{"level":"reader"}`)
		require.Equal(t, 200, w.Code)
	}
	for range 2 {
		w = testutil.Do(f.r, "DELETE", f.path()+"/access/user-reader", "admin", "")
		require.Equal(t, 204, w.Code)
	}
	w = testutil.Do(f.r, "DELETE", "/v1/emergency/bases/"+uuid.Must(uuid.NewV7()).String()+"/access/user-reader", "admin", "")
	require.Equal(t, 404, w.Code)
	w = testutil.Do(f.r, "PUT", "/v1/emergency/bases/not-an-id", "admin", baseBody())
	require.Equal(t, 404, w.Code)
}

func TestIntakeRejectsUnsafeAndUnreviewedInputs(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(intakeBody()), &in))
	for _, change := range []func(){
		func() { in["channel"] = "web" },
		func() { in["requested_service"] = "mortuary" },
		func() { in["contact"].(map[string]any)["callback_phone"] = "+91invalid" },
		func() { in["contact"].(map[string]any)["caller_name"] = "bad\x00name" },
		func() { in["incident"].(map[string]any)["location_text"] = "bad\x00place" },
	} {
		require.NoError(t, json.Unmarshal([]byte(intakeBody()), &in))
		change()
		body, err := json.Marshal(in)
		require.NoError(t, err)
		w := testutil.Do(f.r, "PUT", f.path()+"/cases/"+uuid.Must(uuid.NewV7()).String(), "dispatcher", string(body))
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, path := range []string{f.path() + "/cases/invalid", f.path() + "/cases/invalid/timeline"} {
		w := testutil.Do(f.r, "GET", path, "dispatcher", "")
		require.Equal(t, 404, w.Code)
	}
	w := testutil.Do(f.r, "GET", f.path()+"/cases/"+uuid.Must(uuid.NewV7()).String()+"/timeline", "dispatcher", "")
	require.Equal(t, 404, w.Code)
}
