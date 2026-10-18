package emergency_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/transit"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func vault(t *testing.T) *transit.Client {
	t.Helper()
	addr, _ := testinfra.StartOpenBao(t)
	user, pass := testinfra.OpenBaoUser(t, time.Hour, "helivanta-api-transit")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, pass), nil)
	require.NoError(t, err)
	return c
}
func TestEmergencyTransitKeyScopeAndContext(t *testing.T) {
	c := vault(t)
	key := "helivanta-emergency-" + uuid.NewString()
	sealed, err := c.Encrypt(t.Context(), key, []byte("synthetic-context"), []byte("synthetic-sensitive-value"))
	require.NoError(t, err)
	plain, err := c.Decrypt(t.Context(), key, []byte("synthetic-context"), sealed)
	require.NoError(t, err)
	require.Equal(t, "synthetic-sensitive-value", string(plain))
	_, err = c.Decrypt(t.Context(), key, []byte("another-case"), sealed)
	require.ErrorIs(t, err, transit.ErrIntegrity)
	_, err = c.Encrypt(t.Context(), "unrelated-product-key", []byte("context"), []byte("data"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
}
func intakeBody() string {
	return `{"channel":"phone","requested_service":"unknown","processing_basis":"medical_emergency","contact":{"caller_name":"Synthetic Caller","callback_phone":"+910000000000","relationship":"bystander"},"incident":{"location_text":"Synthetic Gate C","patient_description":"Unknown person, dispatch review required"}}`
}
func createBase(t *testing.T, f *fixture) {
	t.Helper()
	w := testutil.Do(f.r, "PUT", f.path(), "admin", baseBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, x := range []struct{ s, l string }{{"dispatcher", "dispatcher"}, {"supervisor", "supervisor"}, {"reader", "reader"}} {
		w = testutil.Do(f.r, "PUT", f.path()+"/access/user-"+x.s, "admin", `{"level":"`+x.l+`"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
}
func TestCaseIntakeIsIdempotentEncryptedAndUnknownPatientSafe(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	id := uuid.Must(uuid.NewV7()).String()
	path := f.path() + "/cases/" + id
	w := testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	first := w.Body.String()
	row := decodeBody(t, w.Body.Bytes())
	require.Equal(t, "received", row["status"])
	require.Equal(t, "untriaged", row["priority"])
	require.Equal(t, id, row["id"])
	require.NotContains(t, first, "Synthetic Caller")
	w = testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	require.JSONEq(t, first, w.Body.String())
	w = testutil.Do(f.r, "PUT", path, "dispatcher", strings.Replace(intakeBody(), "Synthetic Gate C", "Synthetic Gate D", 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(f.r, "GET", path, "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	got := decodeBody(t, w.Body.Bytes())
	require.Equal(t, "Synthetic Caller", got["contact"].(map[string]any)["caller_name"])
	w = testutil.Do(f.r, "GET", f.path()+"/cases", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "Synthetic Caller")
	require.NotContains(t, w.Body.String(), "Synthetic Gate C")
	var stored string
	var events int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT row_to_json(c)::text FROM emergency_cases c WHERE id=?`, id).Scan(&stored).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.emergency.case_received.v1'`).Scan(&events).Error
	}))
	require.EqualValues(t, 1, events)
	require.NotContains(t, stored, "Synthetic Caller")
	require.NotContains(t, stored, "Synthetic Gate C")
	require.Contains(t, stored, "vault:v")
	w = testutil.Do(f.r, "GET", path+"/timeline", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var timeline struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &timeline))
	require.Len(t, timeline.Data, 1)
	require.Equal(t, "received", timeline.Data[0]["state"])
}
