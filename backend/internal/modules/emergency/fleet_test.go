package emergency_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func newID() string { return uuid.Must(uuid.NewV7()).String() }
func fleetVehicleBody() string {
	return fmt.Sprintf(`{"version":0,"code":"SYN-ALS-1","vehicle_class":"als","certified_until":%q,"policy_reference":"SYN-APPROVED-FLEET-1","oxygen_min_bar":100,"readiness_minutes":480,"retired":false}`, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339))
}

func TestConcurrentReadinessRetryRecordsOneObservation(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	path := f.path() + "/readiness/" + newID()
	body := readinessBody(vehicle, duty, 150, true)
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(f.r, "PUT", path, "dispatcher", body).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 200, code)
	}
}

func TestFleetMutationsPublishOnlyMetadataOnce(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	path := f.path() + "/readiness/" + newID()
	body := readinessBody(vehicle, duty, 150, true)
	for range 2 {
		w := testutil.Do(f.r, "PUT", path, "dispatcher", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	var events []struct{ Data string }
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload->'data' AS data FROM outbox_events WHERE subject='helivanta.in.emergency.fleet_changed.v1' ORDER BY created_at`).Scan(&events).Error
	}))
	require.Len(t, events, 5)
	for _, event := range events {
		require.NotContains(t, event.Data, "user-driver")
		require.NotContains(t, event.Data, "SYN-CREDENTIAL")
		require.NotContains(t, event.Data, "oxygen_bar")
		require.Contains(t, event.Data, "base_id")
	}
}

func TestVehiclePolicyChangesAreVersionedAndReplayDoesNotReapply(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	path := f.path() + "/vehicles/" + newID()
	body := fleetVehicleBody()
	w := testutil.Do(f.r, "PUT", path, "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path, "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	update := strings.Replace(strings.Replace(body, `"version":0`, `"version":1`, 1), `"oxygen_min_bar":100`, `"oxygen_min_bar":110`, 1)
	for range 2 {
		w = testutil.Do(f.r, "PUT", path, "supervisor", update)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, float64(2), decodeBody(t, w.Body.Bytes())["version"])
	}
	w = testutil.Do(f.r, "PUT", path, "supervisor", strings.Replace(update, `110`, `120`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestFleetRequiresScopedManagementAndDoesNotInferReadiness(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	id := newID()
	path := f.path() + "/vehicles/" + id
	w := testutil.Do(f.r, "PUT", path, "supervisor", fleetVehicleBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, float64(1), decodeBody(t, w.Body.Bytes())["version"])
	w = testutil.Do(f.r, "GET", f.path()+"/vehicles", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []struct {
			ID           string   `json:"id"`
			Dispatchable bool     `json:"dispatchable"`
			Blockers     []string `json:"blockers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, id, page.Data[0].ID)
	require.False(t, page.Data[0].Dispatchable)
	require.Contains(t, page.Data[0].Blockers, "no_current_duty")
	require.Contains(t, page.Data[0].Blockers, "readiness_missing_or_expired")
	for _, who := range []string{"admin", "other"} {
		w = testutil.Do(f.r, "PUT", path, who, fleetVehicleBody())
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	w = testutil.Do(f.r, "PUT", path, "dispatcher", fleetVehicleBody())
	require.Equal(t, 403, w.Code, w.Body.String())
}

func crewBody(subject, qualification string) string {
	return fmt.Sprintf(`{"version":0,"subject":%q,"qualification":%q,"certified_until":%q,"evidence_reference":"SYN-CREDENTIAL-CHECK-1","suspended":false}`, subject, qualification, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339))
}
func prepareDuty(t *testing.T, f *fixture, clinical string) (string, string) {
	t.Helper()
	vehicle, duty := newID(), newID()
	w := testutil.Do(f.r, "PUT", f.path()+"/vehicles/"+vehicle, "supervisor", fleetVehicleBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, v := range []struct{ subject, qualification string }{{"user-driver", "driver"}, {"user-medic", clinical}} {
		w = testutil.Do(f.r, "PUT", f.path()+"/crew/"+newID(), "supervisor", crewBody(v.subject, v.qualification))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	body := fmt.Sprintf(`{"vehicle_id":%q,"driver_subject":"user-driver","clinician_subject":"user-medic","ends_at":%q}`, vehicle, time.Now().UTC().Add(8*time.Hour).Format(time.RFC3339))
	w = testutil.Do(f.r, "PUT", f.path()+"/duties/"+duty, "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	return vehicle, duty
}
func readinessBody(vehicle, duty string, oxygen int, defib bool) string {
	return fmt.Sprintf(`{"vehicle_id":%q,"vehicle_version":1,"duty_id":%q,"oxygen_bar":%d,"defibrillator_passed":%t,"equipment_complete":true,"roadworthy":true}`, vehicle, duty, oxygen, defib)
}
func fleetRows(t *testing.T, f *fixture) []map[string]any {
	t.Helper()
	w := testutil.Do(f.r, "GET", f.path()+"/vehicles", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var p struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	return p.Data
}
func TestReadinessUsesVerifiedDutyAndDowngradesALSForEMT(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "emt")
	path := f.path() + "/readiness/" + newID()
	body := readinessBody(vehicle, duty, 150, true)
	for range 2 {
		w := testutil.Do(f.r, "PUT", path, "supervisor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	rows := fleetRows(t, f)
	require.Len(t, rows, 1)
	require.Equal(t, true, rows[0]["dispatchable"])
	require.Equal(t, "bls", rows[0]["effective_capability"])
	require.Empty(t, rows[0]["blockers"])
	w := testutil.Do(f.r, "POST", f.path()+"/duties/"+duty+"/end", "supervisor", `{}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	rows = fleetRows(t, f)
	require.Equal(t, false, rows[0]["dispatchable"])
	require.Contains(t, rows[0]["blockers"], "no_current_duty")
}

func TestFailedEquipmentRemainsBlockedAfterLaterPassUntilScopedClearance(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	failed := newID()
	w := testutil.Do(f.r, "PUT", f.path()+"/readiness/"+failed, "dispatcher", readinessBody(vehicle, duty, 150, false))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", readinessBody(vehicle, duty, 150, true))
	require.Equal(t, 200, w.Code, w.Body.String())
	rows := fleetRows(t, f)
	require.Equal(t, false, rows[0]["dispatchable"])
	require.Contains(t, rows[0]["blockers"], "defibrillator_failed")
	path := f.path() + "/constraints/" + failed + "/defibrillator_failed/clear"
	for _, who := range []string{"dispatcher", "reader"} {
		w = testutil.Do(f.r, "POST", path, who, `{"evidence_reference":"SYN-REPAIR-1"}`)
		require.Equal(t, 403, w.Code, w.Body.String())
	}
	for _, who := range []string{"admin", "other"} {
		w = testutil.Do(f.r, "POST", path, who, `{"evidence_reference":"SYN-REPAIR-1"}`)
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	for range 2 {
		w = testutil.Do(f.r, "POST", path, "supervisor", `{"evidence_reference":"SYN-REPAIR-1"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	rows = fleetRows(t, f)
	require.Equal(t, true, rows[0]["dispatchable"])
	w = testutil.Do(f.r, "POST", path, "supervisor", `{"evidence_reference":"SYN-REPAIR-OTHER"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestDatabaseRejectsConstraintClearanceWithoutEvidence(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	id := newID()
	w := testutil.Do(f.r, "PUT", f.path()+"/readiness/"+id, "dispatcher", readinessBody(vehicle, duty, 150, false))
	require.Equal(t, 200, w.Code, w.Body.String())
	err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE emergency_fleet_constraints SET cleared_at=now() WHERE tenant_id=? AND base_id=? AND id=?`, testutil.TenantA, f.base, id).Error
	})
	require.Error(t, err, "direct scoped SQL cannot clear a safety constraint without evidence or actor")
}

func TestExpiredCredentialAndPolicyChangeInvalidateReadinessImmediately(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	w := testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", readinessBody(vehicle, duty, 150, true))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, true, fleetRows(t, f)[0]["dispatchable"])
	var page struct {
		Data []map[string]any `json:"data"`
	}
	w = testutil.Do(f.r, "GET", f.path()+"/crew", "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	for _, person := range page.Data {
		if person["qualification"] != "paramedic" {
			continue
		}
		person["version"] = 1
		person["certified_until"] = "2020-01-01T00:00:00Z"
		delete(person, "created_at")
		delete(person, "updated_at")
		id := person["id"].(string)
		delete(person, "id")
		body, err := json.Marshal(person)
		require.NoError(t, err)
		w = testutil.Do(f.r, "PUT", f.path()+"/crew/"+id, "supervisor", string(body))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	rows := fleetRows(t, f)
	require.Equal(t, false, rows[0]["dispatchable"])
	require.Contains(t, rows[0]["blockers"], "clinician_not_qualified")
	configuration := strings.Replace(fleetVehicleBody(), `"version":0`, `"version":1`, 1)
	w = testutil.Do(f.r, "PUT", f.path()+"/vehicles/"+vehicle, "supervisor", configuration)
	require.Equal(t, 200, w.Code, w.Body.String())
	rows = fleetRows(t, f)
	require.Contains(t, rows[0]["blockers"], "readiness_missing_or_expired")
}

func TestDutyCannotReuseCrewAndEndedDutyCannotReceiveNewChecks(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	otherVehicle := newID()
	w := testutil.Do(f.r, "PUT", f.path()+"/vehicles/"+otherVehicle, "supervisor", strings.Replace(fleetVehicleBody(), "SYN-ALS-1", "SYN-ALS-2", 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	body := fmt.Sprintf(`{"vehicle_id":%q,"driver_subject":"user-driver","clinician_subject":"user-medic","ends_at":%q}`, otherVehicle, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	w = testutil.Do(f.r, "PUT", f.path()+"/duties/"+newID(), "supervisor", body)
	require.Equal(t, 409, w.Code, w.Body.String())
	for range 2 {
		w = testutil.Do(f.r, "POST", f.path()+"/duties/"+duty+"/end", "supervisor", `{}`)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w = testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", readinessBody(vehicle, duty, 150, true))
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/duties/"+newID(), "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestFleetEndpointsDenyOtherBaseAndTenantAndValidateMeasurementPresence(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	for _, token := range []string{"other", "admin"} {
		for _, resource := range []string{"vehicles", "crew", "duties"} {
			w := testutil.Do(f.r, "GET", f.path()+"/"+resource, token, "")
			require.Equal(t, 404, w.Code, w.Body.String())
		}
	}
	original := f.base
	f.base = newID()
	w := testutil.Do(f.r, "PUT", f.path(), "admin", baseBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "supervisor", readinessBody(vehicle, duty, 150, true))
	require.Equal(t, 404, w.Code, w.Body.String())
	f.base = original
	for _, body := range []string{strings.Replace(readinessBody(vehicle, duty, 150, true), `,"roadworthy":true`, "", 1), readinessBody(vehicle, duty, 301, true), strings.Replace(readinessBody(vehicle, duty, 150, true), `"vehicle_version":1`, `"vehicle_version":2`, 1)} {
		w = testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", body)
		require.Contains(t, []int{400, 409}, w.Code, w.Body.String())
	}
	var count int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-supervisor',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Table("emergency_readiness").Where("tenant_id=? AND base_id=?", testutil.TenantA, f.base).Count(&count).Error
	}))
	require.Zero(t, count)
}
