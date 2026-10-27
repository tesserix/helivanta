package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

// #1377: existing registrations are announced again, one bounded,
// resumable, audited page at a time, to consumers that started after them.
func TestRegistrationReplayAnnouncesEachPatientOncePerPage(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"reg": testutil.TenantA, "admin": testutil.TenantA, "nogrant": testutil.TenantA, "adminB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"reg":     {patientmaster.PermPatientRead, patientmaster.PermPatientRegister},
			"admin":   {patientmaster.PermRegistrationReplay},
			"nogrant": {patientmaster.PermRegistrationReplay},
			"adminB":  {patientmaster.PermRegistrationReplay},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	for subject, tenant := range map[string]string{"user-reg": testutil.TenantA, "user-admin": testutil.TenantA, "user-adminB": testutil.TenantB} {
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, ?, 'reader')`, tenant, testHospital, subject).Error
		}))
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE patient_hospital_access SET level = 'registrar' WHERE subject = 'user-reg'`).Error
	}))
	var registered []string
	for i := range 3 {
		w := scopedDo(r, "POST", "/v1/patients", "reg", registerBody(fmt.Sprintf("Replay%c", 'A'+i), "Test", "1990-01-0"+fmt.Sprint(i+1), fmt.Sprintf("98765432%02d", i), false, ""))
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		registered = append(registered, registeredID(t, w))
	}
	events := func() []string {
		var ids []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT payload->'data'->>'patient_id' FROM outbox_events WHERE subject = 'helivanta.in.patientmaster.registered.v1'`).Scan(&ids).Error
		}))
		return ids
	}
	require.Len(t, events(), 3, "registration published once each")

	url := "/v1/patients/hospitals/" + testHospital + "/registration-replays?limit=2"
	type page struct {
		Data []struct {
			PatientID string `json:"patient_id"`
		} `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	var replayed []string
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 3)
		next := url
		if cursor != "" {
			next += "&cursor=" + cursor
		}
		w := do(r, "POST", next, "admin", `{}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got page
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		for _, row := range got.Data {
			replayed = append(replayed, row.PatientID)
		}
		if !got.Page.HasMore {
			break
		}
		cursor = *got.Page.NextCursor
	}
	sort.Strings(registered)
	sort.Strings(replayed)
	require.Equal(t, registered, replayed)
	// Each patient announced exactly once more: the page's probe row is not.
	announced := map[string]int{}
	for _, id := range events() {
		announced[id]++
	}
	want := map[string]int{}
	for _, id := range registered {
		want[id] = 2
	}
	require.Equal(t, want, announced)

	var audited []string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT actor_subject || ':' || jsonb_array_length(resource_ids) FROM patient_access_audit WHERE action = 'replay' ORDER BY created_at`).Scan(&audited).Error
	}))
	require.Equal(t, []string{"user-admin:2", "user-admin:1"}, audited)

	// No patient-registry grant, or another tenant: the same 404, nothing announced.
	require.Equal(t, http.StatusNotFound, do(r, "POST", url, "nogrant", `{}`).Code)
	w := do(r, "POST", url, "adminB", `{}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"data":[],"page":{"next_cursor":null,"has_more":false}}`, w.Body.String())
	// The registrar cannot replay: the permission is admin-only.
	require.Equal(t, http.StatusForbidden, do(r, "POST", url, "reg", `{}`).Code)
	require.Len(t, events(), 6)
}
