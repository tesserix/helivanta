package emergency_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func receivingFacilityBody(version int, enabled bool) string {
	return fmt.Sprintf(`{"version":%d,"name":"Synthetic Receiving Campus","enabled":%t,"policy_reference":"SYN-RECEIVING-2026","expires_at":%q}`, version, enabled, time.Now().UTC().Add(24*time.Hour).Truncate(time.Second).Format(time.RFC3339))
}
func TestReceivingFacilityRetainsActorBoundConfigurationRevisions(t *testing.T) {
	f := setup(t)
	id := newID()
	path := "/v1/emergency/receiving/facilities/" + id
	body := receivingFacilityBody(0, true)
	w := testutil.Do(f.r, "PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, float64(1), decodeBody(t, w.Body.Bytes())["version"])
	original := w.Body.String()
	require.Equal(t, original, testutil.Do(f.r, "PUT", path, "admin", body).Body.String())
	require.Equal(t, 409, testutil.Do(f.r, "PUT", path, "verifier", body).Code)
	w = testutil.Do(f.r, "PUT", path, "admin", receivingFacilityBody(1, false))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, float64(2), decodeBody(t, w.Body.Bytes())["version"])
	require.Equal(t, original, testutil.Do(f.r, "PUT", path, "admin", body).Body.String())
	require.Equal(t, 404, testutil.Do(f.r, "GET", path, "other", "").Code)
	require.Equal(t, 403, testutil.Do(f.r, "PUT", path, "driver", body).Code)
}

func TestReceivingAuthorityRequiresIndependentExpiringFacilityGrant(t *testing.T) {
	f := setup(t)
	id := newID()
	path := "/v1/emergency/receiving/facilities/" + id
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "admin", receivingFacilityBody(0, true)).Code)
	grant := fmt.Sprintf(`{"version":0,"enabled":true,"level":"manager","policy_reference":"SYN-RECEIVING-DELEGATION","expires_at":%q}`, time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339))
	w := testutil.Do(f.r, "PUT", path+"/access/user-verifier", "admin", grant)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, testutil.Do(f.r, "GET", path, "verifier", "").Code)
	require.Equal(t, 400, testutil.Do(f.r, "PUT", path+"/access/user-admin", "admin", grant).Code)
	require.Equal(t, 404, testutil.Do(f.r, "GET", path+"/access/user-verifier", "other", "").Code)
	disable := fmt.Sprintf(`{"version":1,"enabled":false,"level":"manager","policy_reference":"SYN-REVOKED","expires_at":%q}`, time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/access/user-verifier", "admin", disable).Code)
	// Keep the verifier's administrative permission in this fixture: explicit current authority is a separate fact.
	w = testutil.Do(f.r, "GET", path+"/access/user-verifier", "verifier", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, false, decodeBody(t, w.Body.Bytes())["enabled"])
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/access/user-verifier", "admin", grant).Code, "replay returns the old receipt without re-enabling authority")
}

func prepareReceivingFacility(t *testing.T, f *fixture) string {
	t.Helper()
	id := newID()
	path := "/v1/emergency/receiving/facilities/" + id
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "badmin", receivingFacilityBody(0, true)).Code)
	for _, subject := range []string{"bmanager", "breceiver"} {
		level := "receiver"
		if subject == "bmanager" {
			level = "manager"
		}
		grant := fmt.Sprintf(`{"version":0,"enabled":true,"level":%q,"policy_reference":"SYN-RECEIVING-DELEGATION","expires_at":%q}`, level, time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339))
		w := testutil.Do(f.r, "PUT", path+"/access/user-"+subject, "badmin", grant)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	return id
}
func TestReceivingLinkRequiresBothDirectedAuthorities(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	facility := prepareReceivingFacility(t, f)
	link := newID()
	providerPath := f.path() + "/receiving-links/" + link
	body := fmt.Sprintf(`{"destination_tenant_id":%q,"facility_id":%q,"policy_reference":"SYN-PROVIDER-RECEIVING","valid_until":%q}`, testutil.TenantB, facility, time.Now().UTC().Add(24*time.Hour).Truncate(time.Second).Format(time.RFC3339))
	w := testutil.Do(f.r, "PUT", providerPath, "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "proposed", decodeBody(t, w.Body.Bytes())["status"])
	recipientPath := "/v1/emergency/receiving/facilities/" + facility + "/links/" + testutil.TenantA + "/" + link
	require.Equal(t, 404, testutil.Do(f.r, "GET", recipientPath, "badmin", "").Code, "administration does not confer receiving authority")
	require.Equal(t, 200, testutil.Do(f.r, "GET", recipientPath, "bmanager", "").Code)
	command := recipientPath + "/commands/" + newID()
	approve := `{"version":1,"operation":"approve","evidence_reference":"SYN-AGREEMENT-SIGNED"}`
	require.Equal(t, 404, testutil.Do(f.r, "PUT", command, "breceiver", approve).Code)
	w = testutil.Do(f.r, "PUT", command, "bmanager", approve)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "approved", decodeBody(t, w.Body.Bytes())["status"])
	require.Equal(t, w.Body.String(), testutil.Do(f.r, "PUT", command, "bmanager", approve).Body.String())
	w = testutil.Do(f.r, "GET", providerPath, "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "approved", decodeBody(t, w.Body.Bytes())["status"])
	revoke := `{"version":2,"operation":"revoke","evidence_reference":"SYN-LINK-REVOKED"}`
	w = testutil.Do(f.r, "PUT", providerPath+"/commands/"+newID(), "supervisor", revoke)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "revoked", decodeBody(t, w.Body.Bytes())["status"])
	require.Equal(t, 409, testutil.Do(f.r, "PUT", recipientPath+"/commands/"+newID(), "bmanager", `{"version":3,"operation":"approve","evidence_reference":"SYN-REAPPROVE"}`).Code)
}

func prepareReceivingLink(t *testing.T, f *fixture, facility string) string {
	t.Helper()
	id := newID()
	body := fmt.Sprintf(`{"destination_tenant_id":%q,"facility_id":%q,"policy_reference":"SYN-PROVIDER-RECEIVING","valid_until":%q}`, testutil.TenantB, facility, time.Now().UTC().Add(24*time.Hour).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/receiving-links/"+id, "supervisor", body).Code)
	path := "/v1/emergency/receiving/facilities/" + facility + "/links/" + testutil.TenantA + "/" + id + "/commands/" + newID()
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "bmanager", `{"version":1,"operation":"approve","evidence_reference":"SYN-BILATERAL-AGREEMENT"}`).Code)
	return id
}
func prepareScene(t *testing.T, f *fixture, existing ...bool) string {
	t.Helper()
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	if len(existing) > 0 && existing[0] {
		// Recreate a previously committed trip through the real SQL guards before testing suspended entry.
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO emergency_dispatches(tenant_id,base_id,id,case_id,case_version,assessment_id,vehicle_id,duty_id,acknowledgement_seconds,requested_by,driver_subject,clinician_subject)VALUES(?,?,?,?,1,?,?,?,120,'user-dispatcher','user-driver','user-medic')`, testutil.TenantA, f.base, id, c, a, v, d).Error
		}))
	} else {
		require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	}
	_, err := dispatchProcessor(t, f).Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	for i, step := range []struct{ actor, operation string }{{"driver", "acknowledge"}, {"medic", "acknowledge"}, {"driver", "en_route"}, {"driver", "on_scene"}} {
		body := fmt.Sprintf(`{"version":%d,"operation":%q,"evidence_reference":"SYN-OPERATION"}`, i+2, step.operation)
		w := testutil.Do(f.r, "PUT", "/v1/emergency/crew/bases/"+f.base+"/dispatches/"+id+"/commands/"+newID(), step.actor, body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	return id
}
func TestHospitalAcceptanceIsDirectedLeasedAndDistinctFromPreference(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	dispatch := prepareScene(t, f)
	facility := prepareReceivingFacility(t, f)
	link := prepareReceivingLink(t, f, facility)
	request := newID()
	path := f.path() + "/dispatches/" + dispatch + "/receiving/" + request
	body := fmt.Sprintf(`{"version":6,"link_id":%q,"response_seconds":120,"evidence_reference":"SYN-PREALERT-VOICE"}`, link)
	w := testutil.Do(f.r, "PUT", path, "dispatcher", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "pending", decodeBody(t, w.Body.Bytes())["status"])
	require.NotContains(t, w.Body.String(), "Synthetic Gate C")
	require.NotContains(t, w.Body.String(), "+910000000000")
	recipient := "/v1/emergency/receiving/facilities/" + facility + "/requests/" + testutil.TenantA + "/" + request
	require.Equal(t, 404, testutil.Do(f.r, "GET", recipient, "badmin", "").Code)
	require.Equal(t, 200, testutil.Do(f.r, "GET", recipient, "breceiver", "").Code)
	command := recipient + "/commands/" + newID()
	accept := fmt.Sprintf(`{"version":1,"operation":"accept","evidence_reference":"SYN-ACCEPTANCE-VOICE","valid_until":%q}`, time.Now().UTC().Add(30*time.Minute).Truncate(time.Second).Format(time.RFC3339))
	w = testutil.Do(f.r, "PUT", command, "breceiver", accept)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "accepted", decodeBody(t, w.Body.Bytes())["status"])
	require.Equal(t, w.Body.String(), testutil.Do(f.r, "PUT", command, "breceiver", accept).Body.String())
	require.Equal(t, 409, testutil.Do(f.r, "PUT", command, "bmanager", accept).Code)
	w = testutil.Do(f.r, "GET", path, "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "accepted", decodeBody(t, w.Body.Bytes())["status"])
	require.Equal(t, true, decodeBody(t, w.Body.Bytes())["usable"])
	w = testutil.Do(f.r, "PUT", path, "dispatcher", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "pending", decodeBody(t, w.Body.Bytes())["status"], "lost response returns the original request receipt")
}

func tripCommand(t *testing.T, f *fixture, dispatch, actor, operation string, version int, targets ...string) int {
	t.Helper()
	path := "/v1/emergency/crew/bases/" + f.base + "/dispatches/" + dispatch + "/commands/" + newID()
	if actor == "supervisor" {
		path = f.path() + "/dispatches/" + dispatch + "/commands/" + newID()
	}
	input := map[string]any{"version": version, "operation": operation, "evidence_reference": "SYN-TRIP-EVIDENCE"}
	if operation == "transporting" || operation == "arrived" || operation == "handover_complete" {
		target := ""
		if len(targets) > 0 {
			target = targets[0]
		} else {
			list := testutil.Do(f.r, "GET", "/v1/emergency/crew/bases/"+f.base+"/dispatches/"+dispatch+"/receiving", actor, "")
			if list.Code == 200 {
				rows := decodeBody(t, list.Body.Bytes())["data"].([]any)
				var maximum float64
				for _, row := range rows {
					r := row.(map[string]any)
					if r["sequence"].(float64) > maximum {
						maximum = r["sequence"].(float64)
						target = r["id"].(string)
					}
				}
			}
		}
		if target != "" {
			input["receiving_request_id"] = target
		}
	}
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	body := string(encoded)
	w := testutil.Do(f.r, "PUT", path, actor, body)
	if w.Code != 200 && w.Code != 409 && w.Code != 403 {
		t.Log(w.Body.String())
	}
	return w.Code
}
func TestTransportWaitsForAcceptanceAndTwoHandoverSubjects(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	dispatch := prepareScene(t, f)
	require.Equal(t, 409, tripCommand(t, f, dispatch, "driver", "transporting", 6), "preference alone is not hospital acceptance")
	facility := prepareReceivingFacility(t, f)
	link := prepareReceivingLink(t, f, facility)
	request := newID()
	path := f.path() + "/dispatches/" + dispatch + "/receiving/" + request
	body := fmt.Sprintf(`{"version":6,"link_id":%q,"response_seconds":120,"evidence_reference":"SYN-PREALERT"}`, link)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "dispatcher", body).Code)
	recipient := "/v1/emergency/receiving/facilities/" + facility + "/requests/" + testutil.TenantA + "/" + request + "/commands/"
	accept := fmt.Sprintf(`{"version":1,"operation":"accept","evidence_reference":"SYN-VOICE-ACCEPT","valid_until":%q}`, time.Now().UTC().Add(30*time.Minute).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", recipient+newID(), "breceiver", accept).Code)
	require.Equal(t, 200, tripCommand(t, f, dispatch, "driver", "transporting", 6))
	require.Equal(t, 409, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+dispatch+"/commands/"+newID(), "dispatcher", `{"version":7,"operation":"request_cancel","evidence_reference":"SYN-UNSAFE-CANCEL"}`).Code)
	handover := `{"version":2,"operation":"handover_received","evidence_reference":"SYN-PATIENT-RECEIVED"}`
	require.Equal(t, 409, testutil.Do(f.r, "PUT", recipient+newID(), "breceiver", handover).Code, "physical handover cannot precede arrival")
	require.Equal(t, 200, tripCommand(t, f, dispatch, "driver", "arrived", 7))
	require.Equal(t, 409, tripCommand(t, f, dispatch, "driver", "handover_complete", 8), "crew cannot invent a receiving signature")
	require.Equal(t, 200, testutil.Do(f.r, "PUT", recipient+newID(), "breceiver", handover).Code)
	require.Equal(t, 200, tripCommand(t, f, dispatch, "driver", "handover_complete", 8))
	require.Equal(t, 403, tripCommand(t, f, dispatch, "driver", "clear", 9))
	require.Equal(t, 409, tripCommand(t, f, dispatch, "supervisor", "clear", 9), "a pre-trip readiness check cannot clear a completed trip")
	trip := decodeBody(t, testutil.Do(f.r, "GET", f.path()+"/dispatches/"+dispatch, "dispatcher", "").Body.Bytes())
	vehicle, duty := trip["vehicle_id"].(string), trip["duty_id"].(string)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", readinessBody(vehicle, duty, 150, true)).Code)
	require.Equal(t, 200, tripCommand(t, f, dispatch, "supervisor", "clear", 9))
	require.Equal(t, 404, testutil.Do(f.r, "GET", "/v1/emergency/crew/bases/"+f.base+"/dispatches/"+dispatch+"/incident", "driver", "").Code)
	rows := fleetRows(t, f)
	require.Len(t, rows, 1)
	require.Equal(t, vehicle, rows[0]["id"])
	require.Equal(t, true, rows[0]["dispatchable"])
	reserve := fmt.Sprintf(`{"case_id":%q,"case_version":1,"assessment_id":%q,"vehicle_id":%q,"duty_id":%q,"acknowledgement_seconds":120}`, trip["case_id"], trip["assessment_id"], vehicle, duty)
	require.Equal(t, 409, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+newID(), "dispatcher", reserve).Code, "a completed citizen journey needs a new case for another transport")
}

type acceptedReceivingTrip struct{ dispatch, facility, link, request, providerPath, recipientPath string }

func prepareAcceptedReceivingTrip(t *testing.T, f *fixture) acceptedReceivingTrip {
	t.Helper()
	a := acceptedReceivingTrip{dispatch: prepareScene(t, f), facility: prepareReceivingFacility(t, f), request: newID()}
	a.link = prepareReceivingLink(t, f, a.facility)
	a.providerPath = f.path() + "/dispatches/" + a.dispatch + "/receiving/" + a.request
	body := fmt.Sprintf(`{"version":6,"link_id":%q,"response_seconds":120,"evidence_reference":"SYN-PREALERT"}`, a.link)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", a.providerPath, "dispatcher", body).Code)
	a.recipientPath = "/v1/emergency/receiving/facilities/" + a.facility + "/requests/" + testutil.TenantA + "/" + a.request
	accept := fmt.Sprintf(`{"version":1,"operation":"accept","evidence_reference":"SYN-CLINICAL-VOICE","valid_until":%q}`, time.Now().UTC().Add(30*time.Minute).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", a.recipientPath+"/commands/"+newID(), "breceiver", accept).Code)
	return a
}
func TestHospitalRevocationHoldsCapacityUntilNewDestinationAcceptance(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	require.Equal(t, 200, tripCommand(t, f, a.dispatch, "driver", "transporting", 6))
	w := testutil.Do(f.r, "PUT", a.recipientPath+"/commands/"+newID(), "breceiver", `{"version":2,"operation":"revoke","evidence_reference":"SYN-CAPACITY-CHANGED"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "arrived", 7))
	rows := fleetRows(t, f)
	require.Equal(t, false, rows[0]["dispatchable"])
	request := newID()
	path := f.path() + "/dispatches/" + a.dispatch + "/receiving/" + request
	missingPrevious := fmt.Sprintf(`{"version":7,"link_id":%q,"response_seconds":120,"evidence_reference":"SYN-DIVERSION"}`, a.link)
	require.Equal(t, 409, testutil.Do(f.r, "PUT", path, "dispatcher", missingPrevious).Code)
	body := fmt.Sprintf(`{"version":7,"link_id":%q,"previous_request_id":%q,"response_seconds":120,"evidence_reference":"SYN-DIVERSION"}`, a.link, a.request)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "dispatcher", body).Code)
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "arrived", 7))
	recipient := "/v1/emergency/receiving/facilities/" + a.facility + "/requests/" + testutil.TenantA + "/" + request
	accept := fmt.Sprintf(`{"version":1,"operation":"accept","evidence_reference":"SYN-NEW-ACCEPTANCE","valid_until":%q}`, time.Now().UTC().Add(30*time.Minute).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", recipient+"/commands/"+newID(), "bmanager", accept).Code)
	require.Equal(t, 200, tripCommand(t, f, a.dispatch, "driver", "arrived", 7))
	old := decodeBody(t, testutil.Do(f.r, "GET", a.providerPath, "dispatcher", "").Body.Bytes())
	require.Equal(t, true, old["superseded"])
	require.Equal(t, "revoked", old["status"])
}
func TestRevokedReceivingDelegationCannotSustainAnEarlierAcceptance(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	body := fmt.Sprintf(`{"version":1,"enabled":false,"level":"receiver","policy_reference":"SYN-AUTHORITY-REVOKED","expires_at":%q}`, time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", "/v1/emergency/receiving/facilities/"+a.facility+"/access/user-breceiver", "badmin", body).Code)
	require.Equal(t, 404, testutil.Do(f.r, "GET", a.recipientPath, "breceiver", "").Code)
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "transporting", 6))
}
func TestExpiredHospitalAcceptanceCannotStartTransport(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	clock := time.Now().UTC().Add(31 * time.Minute).Format(time.RFC3339Nano)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_receiving_clock", SQL: fmt.Sprintf(`CREATE OR REPLACE FUNCTION emergency_dispatch_clock() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$SELECT '%s'::timestamptz$$;`, clock)}}))
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "transporting", 6))
}

func TestReceivingDiscoveryListsOnlyExplicitlyGrantedCampuses(t *testing.T) {
	f := setup(t)
	facility := prepareReceivingFacility(t, f)
	other := newID()
	require.Equal(t, 200, testutil.Do(f.r, "PUT", "/v1/emergency/receiving/facilities/"+other, "badmin", receivingFacilityBody(0, true)).Code)
	path := "/v1/emergency/receiving/facilities"
	w := testutil.Do(f.r, "GET", path, "breceiver", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	rows := decodeBody(t, w.Body.Bytes())["data"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, facility, rows[0].(map[string]any)["id"])
	require.Equal(t, "receiver", rows[0].(map[string]any)["access_level"])
	w = testutil.Do(f.r, "GET", path, "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Empty(t, decodeBody(t, w.Body.Bytes())["data"])
	w = testutil.Do(f.r, "GET", path, "badmin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, decodeBody(t, w.Body.Bytes())["data"], 2)
}

func TestWorkerSnapshotIncludesTheCurrentReceivingDeadline(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: a.dispatch}
	snapshot, err := dispatchProcessor(t, f).Snapshot(t.Context(), in)
	require.NoError(t, err)
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	result := decodeBody(t, raw)
	require.Equal(t, a.request, result["receiving_request_id"])
	require.NotNil(t, result["receiving_deadline"])
	require.Equal(t, false, result["receiving_timed_out"])
	require.NotContains(t, string(raw), "user-breceiver")
	require.NotContains(t, string(raw), "SYN-CLINICAL-VOICE")
}

func TestReceivingExpiryIsIdempotentAndNeverReleasesOccupiedCapacity(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	processor := dispatchProcessor(t, f)
	in := emergency.DispatchReceivingExpiryInput{DispatchWorkflowInput: emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: a.dispatch}, RequestID: a.request, Kind: "acceptance"}
	setDispatchClock(t, f, time.Now().UTC().Add(31*time.Minute))
	first, err := processor.ExpireReceiving(t.Context(), in)
	require.NoError(t, err)
	require.True(t, first.ReceivingTimedOut)
	require.Equal(t, "on_scene", first.State)
	require.Equal(t, int64(6), first.Version)
	again, err := processor.ExpireReceiving(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, false, fleetRows(t, f)[0]["dispatchable"])
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error)
		var count int64
		require.NoError(t, tx.Table("emergency_receiving_timeouts").Where("tenant_id=? AND base_id=? AND request_id=?", testutil.TenantA, f.base, a.request).Count(&count).Error)
		require.Equal(t, int64(1), count)
		return nil
	}))
}

func TestReceivingDecisionCommitsOnlyDirectedOperationalNotification(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	var notifications []struct{ Payload string }
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Select("payload").Where("subject=?", "helivanta.in.emergency.receiving_changed.v1").Find(&notifications).Error
	}))
	require.Len(t, notifications, 1)
	var event events.Event
	require.NoError(t, json.Unmarshal([]byte(notifications[0].Payload), &event))
	require.Equal(t, testutil.TenantB, event.TenantID)
	require.Equal(t, testutil.TenantA, event.DestinationTenantID)
	data := decodeBody(t, event.Data)
	require.Len(t, data, 4)
	require.Equal(t, a.request, data["request_id"])
	require.Equal(t, a.dispatch, data["dispatch_id"])
	require.NotContains(t, notifications[0].Payload, "SYN-CLINICAL-VOICE")
	require.NotContains(t, notifications[0].Payload, "user-breceiver")
	require.NotContains(t, notifications[0].Payload, "Synthetic Gate C")
	var count int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.emergency.dispatch_changed.v1").Count(&count).Error
	}))
	require.Equal(t, int64(7), count)
}

func TestRevokedClinicalDelegationCannotStartHospitalTransport(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false)).Code)
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "transporting", 6))
}

func TestCrewMovementCannotSilentlyUseAChangedDestination(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	newer := newID()
	path := f.path() + "/dispatches/" + a.dispatch + "/receiving/" + newer
	body := fmt.Sprintf(`{"version":6,"link_id":%q,"previous_request_id":%q,"response_seconds":120,"evidence_reference":"SYN-NEW-DESTINATION"}`, a.link, a.request)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "dispatcher", body).Code)
	recipient := "/v1/emergency/receiving/facilities/" + a.facility + "/requests/" + testutil.TenantA + "/" + newer + "/commands/" + newID()
	accept := fmt.Sprintf(`{"version":1,"operation":"accept","evidence_reference":"SYN-NEW-ACCEPTANCE","valid_until":%q}`, time.Now().UTC().Add(30*time.Minute).Truncate(time.Second).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", recipient, "breceiver", accept).Code)
	require.Equal(t, 409, tripCommand(t, f, a.dispatch, "driver", "transporting", 6, a.request))
	command := "/v1/emergency/crew/bases/" + f.base + "/dispatches/" + a.dispatch + "/commands/" + newID()
	movement := fmt.Sprintf(`{"version":6,"operation":"transporting","receiving_request_id":%q,"evidence_reference":"SYN-TARGET-ACKNOWLEDGED"}`, newer)
	w := testutil.Do(f.r, "PUT", command, "driver", movement)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, newer, decodeBody(t, w.Body.Bytes())["receiving_request_id"])
	require.Equal(t, w.Body.String(), testutil.Do(f.r, "PUT", command, "driver", movement).Body.String())
}

func TestSuspendingNewEntryDoesNotStrandAnAlreadyCommittedTrip(t *testing.T) {
	f := setupWithSubjectAliases(t, vault(t), nil, false)
	dispatch := prepareScene(t, f, true)
	facility := prepareReceivingFacility(t, f)
	link := prepareReceivingLink(t, f, facility)
	path := f.path() + "/dispatches/" + dispatch + "/receiving/" + newID()
	body := fmt.Sprintf(`{"version":6,"link_id":%q,"response_seconds":120,"evidence_reference":"SYN-EXISTING-TRIP"}`, link)
	w := testutil.Do(f.r, "PUT", path, "dispatcher", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	trip := decodeBody(t, testutil.Do(f.r, "GET", f.path()+"/dispatches/"+dispatch, "dispatcher", "").Body.Bytes())
	reserve := fmt.Sprintf(`{"case_id":%q,"case_version":1,"assessment_id":%q,"vehicle_id":%q,"duty_id":%q,"acknowledgement_seconds":120}`, trip["case_id"], trip["assessment_id"], trip["vehicle_id"], trip["duty_id"])
	require.Equal(t, 503, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+newID(), "dispatcher", reserve).Code)
}
