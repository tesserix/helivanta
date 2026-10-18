package emergency_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func prepareDispatch(t *testing.T, f *fixture) (string, string, string, string) {
	t.Helper()
	createBase(t, f)
	vehicle, duty := prepareDuty(t, f, "paramedic")
	w := testutil.Do(f.r, "PUT", f.path()+"/readiness/"+newID(), "dispatcher", readinessBody(vehicle, duty, 150, true))
	require.Equal(t, 200, w.Code, w.Body.String())
	caseID, assessment := newID(), newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/cases/"+caseID, "dispatcher", intakeBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/access/user-doctor", "admin", `{"level":"reader"}`).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(0, true)).Code)
	w = testutil.Do(f.r, "PUT", f.path()+"/cases/"+caseID+"/assessments/"+assessment, "doctor", transportReviewBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	return caseID, assessment, vehicle, duty
}

func dispatchBody(caseID, assessment, vehicle, duty string) string {
	return fmt.Sprintf(`{"case_id":%q,"case_version":1,"assessment_id":%q,"vehicle_id":%q,"duty_id":%q,"acknowledgement_seconds":120}`, caseID, assessment, vehicle, duty)
}

func TestReservationCommitsCapacityButDoesNotClaimCrewAcknowledgement(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	path := f.path() + "/dispatches/" + newID()
	body := dispatchBody(c, a, v, d)
	for range 2 {
		w := testutil.Do(f.r, "PUT", path, "dispatcher", body)
		require.Equal(t, 202, w.Code, w.Body.String())
		row := decodeBody(t, w.Body.Bytes())
		require.Equal(t, "workflow_pending", row["state"])
		require.Equal(t, float64(1), row["version"])
		require.Equal(t, c, row["case_id"])
		require.Equal(t, v, row["vehicle_id"])
		require.NotContains(t, w.Body.String(), "Synthetic Caller")
	}
	rows := fleetRows(t, f)
	require.False(t, rows[0]["dispatchable"].(bool))
	require.Contains(t, rows[0]["blockers"], "active_dispatch")
}

func TestDispatchCannotReleaseCapacityWithoutAnAttributedCommand(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	w := testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d))
	require.Equal(t, 202, w.Code, w.Body.String())
	err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-dispatcher',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE emergency_dispatches SET state='cancelled',version=version+1 WHERE id=?`, id).Error
	})
	require.Error(t, err, "a mutable state field must not release capacity outside the command ledger")
}

func TestActiveReservationPreventsUncoordinatedCaseClosureAndDutyEnd(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	w := testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+newID(), "dispatcher", dispatchBody(c, a, v, d))
	require.Equal(t, 202, w.Code, w.Body.String())
	w = testutil.Do(f.r, "POST", f.path()+"/cases/"+c+"/close", "supervisor", `{"version":1,"reason":"caller_withdrew","explanation":"Synthetic cancellation; dispatch requires reconciliation"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(f.r, "POST", f.path()+"/duties/"+d+"/end", "supervisor", `{}`)
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestCancellationRetainsCapacityUntilExplicitSupervisorReconciliation(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	path := f.path() + "/dispatches/" + id
	require.Equal(t, 202, testutil.Do(f.r, "PUT", path, "dispatcher", dispatchBody(c, a, v, d)).Code)
	body := `{"version":1,"operation":"request_cancel","evidence_reference":"SYN-CANCEL-REQUEST"}`
	command := path + "/commands/" + newID()
	for range 2 {
		w := testutil.Do(f.r, "PUT", command, "dispatcher", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		row := decodeBody(t, w.Body.Bytes())
		require.Equal(t, "cancel_requested", row["state"])
		require.Equal(t, float64(2), row["version"])
	}
	require.False(t, fleetRows(t, f)[0]["dispatchable"].(bool))
	reconcile := path + "/commands/" + newID()
	body = `{"version":2,"operation":"reconcile_cancelled","evidence_reference":"SYN-CREW-CONTACT-VERIFIED"}`
	w := testutil.Do(f.r, "PUT", reconcile, "dispatcher", body)
	require.Equal(t, 404, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", reconcile, "supervisor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	row := decodeBody(t, w.Body.Bytes())
	require.Equal(t, "cancelled", row["state"])
	require.Equal(t, float64(3), row["version"])
	require.True(t, fleetRows(t, f)[0]["dispatchable"].(bool))
}

func TestCompetingReservationsCommitExactlyOneVehicleAndCrew(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	c2, a2 := newID(), newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/cases/"+c2, "dispatcher", intakeBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/cases/"+c2+"/assessments/"+a2, "doctor", transportReviewBody()).Code)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, body := range []string{dispatchBody(c, a, v, d), dispatchBody(c2, a2, v, d)} {
		wg.Go(func() {
			<-start
			codes <- testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+newID(), "dispatcher", body).Code
		})
	}
	close(start)
	wg.Wait()
	close(codes)
	var success, conflict int
	for code := range codes {
		if code == 202 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
}

func TestReservationDeniesOtherTenantScopeActorAndStaleClinicalDelegation(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	path := f.path() + "/dispatches/" + newID()
	body := dispatchBody(c, a, v, d)
	require.Equal(t, 404, testutil.Do(f.r, "PUT", path, "other", body).Code)
	require.Equal(t, 404, testutil.Do(f.r, "PUT", path, "admin", body).Code)
	w := testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, testutil.Do(f.r, "PUT", path, "dispatcher", body).Code)
}

func TestReservationReplayCannotBeReboundToAnotherActorOrBody(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	path := f.path() + "/dispatches/" + newID()
	body := dispatchBody(c, a, v, d)
	require.Equal(t, 202, testutil.Do(f.r, "PUT", path, "dispatcher", body).Code)
	require.Equal(t, 409, testutil.Do(f.r, "PUT", path, "supervisor", body).Code)
	require.Equal(t, 409, testutil.Do(f.r, "PUT", path, "dispatcher", strings.Replace(body, ":120", ":180", 1)).Code)
}

func TestReservationEligibilityUsesCurrentClinicalDelegationRatherThanHistory(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, _, _ := prepareDispatch(t, f)
	path := f.path() + "/cases/" + c + "/dispatch-eligibility"
	w := testutil.Do(f.r, "GET", path, "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	row := decodeBody(t, w.Body.Bytes())
	require.Equal(t, true, row["eligible"])
	require.Equal(t, a, row["assessment_id"])
	require.Equal(t, 404, testutil.Do(f.r, "GET", path, "other", "").Code)
	require.Equal(t, 404, testutil.Do(f.r, "GET", path, "admin", "").Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false)).Code)
	w = testutil.Do(f.r, "GET", path, "dispatcher", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, false, decodeBody(t, w.Body.Bytes())["eligible"])
}

func TestAssignedCrewMustBothAcknowledgeBeforeRecordingProgress(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	input := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id}
	_, err := dispatchProcessor(t, f).Offer(t.Context(), input)
	require.NoError(t, err)
	path := "/v1/emergency/crew/bases/" + f.base + "/dispatches/" + id
	require.Equal(t, 200, testutil.Do(f.r, "GET", path, "driver", "").Code)
	require.Equal(t, 404, testutil.Do(f.r, "GET", path, "unassigned", "").Code)
	ack := path + "/commands/" + newID()
	body := `{"version":2,"operation":"acknowledge","evidence_reference":"SYN-CREW-DEVICE-ACK"}`
	for range 2 {
		w := testutil.Do(f.r, "PUT", ack, "driver", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		row := decodeBody(t, w.Body.Bytes())
		require.Equal(t, "offered", row["state"])
		require.Equal(t, float64(3), row["version"])
	}
	w := testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "driver", `{"version":3,"operation":"en_route","evidence_reference":"SYN-START-TRIP"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "medic", `{"version":3,"operation":"acknowledge","evidence_reference":"SYN-CREW-CLINICIAN-ACK"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "acknowledged", decodeBody(t, w.Body.Bytes())["state"])
	w = testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "driver", `{"version":4,"operation":"en_route","evidence_reference":"SYN-START-TRIP"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "en_route", decodeBody(t, w.Body.Bytes())["state"])
	w = testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "medic", `{"version":5,"operation":"on_scene","evidence_reference":"SYN-AT-SCENE"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "on_scene", decodeBody(t, w.Body.Bytes())["state"])
	require.Equal(t, 404, testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "unassigned", `{"version":6,"operation":"on_scene","evidence_reference":"SYN-AT-SCENE"}`).Code)
}

func TestSharedCrewIdentityCannotAcknowledgeTwoProvidersAtOnce(t *testing.T) {
	f := setupWithSubjectAliases(t, vault(t), map[string]string{"bdriver": "user-driver", "bmedic": "user-medic"})
	c, a, v, d := prepareDispatch(t, f)
	first := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+first, "dispatcher", dispatchBody(c, a, v, d)).Code)
	processor := dispatchProcessor(t, f)
	_, err := processor.Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: first})
	require.NoError(t, err)
	base := newID()
	path := "/v1/emergency/bases/" + base
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path, "badmin", baseBody()).Code)
	for subject, level := range map[string]string{"user-bdispatcher": "dispatcher", "user-bsupervisor": "supervisor", "user-bdoctor": "reader"} {
		w := testutil.Do(f.r, "PUT", path+"/access/"+subject, "badmin", fmt.Sprintf(`{"level":%q}`, level))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	vehicle, duty := newID(), newID()
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/vehicles/"+vehicle, "bsupervisor", fleetVehicleBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/crew/"+newID(), "bsupervisor", crewBody("user-driver", "driver")).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/crew/"+newID(), "bsupervisor", crewBody("user-medic", "paramedic")).Code)
	body := fmt.Sprintf(`{"vehicle_id":%q,"driver_subject":"user-driver","clinician_subject":"user-medic","ends_at":%q}`, vehicle, time.Now().UTC().Add(8*time.Hour).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/duties/"+duty, "bsupervisor", body).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/readiness/"+newID(), "bdispatcher", readinessBody(vehicle, duty, 150, true)).Code)
	secondCase, review := newID(), newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", path+"/cases/"+secondCase, "bdispatcher", intakeBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/clinical-reviewers/user-bdoctor", "badmin", reviewerBody(0, true)).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", path+"/cases/"+secondCase+"/assessments/"+review, "bdoctor", transportReviewBody()).Code)
	second := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", path+"/dispatches/"+second, "bdispatcher", dispatchBody(secondCase, review, vehicle, duty)).Code)
	_, err = processor.Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantB, BaseID: base, DispatchID: second})
	require.NoError(t, err)
	firstPath := "/v1/emergency/crew/bases/" + f.base + "/dispatches/" + first
	secondPath := "/v1/emergency/crew/bases/" + base + "/dispatches/" + second
	command := `{"version":2,"operation":"acknowledge","evidence_reference":"SYN-CREW-ACK"}`
	require.Equal(t, 200, testutil.Do(f.r, "PUT", firstPath+"/commands/"+newID(), "driver", command).Code)
	w := testutil.Do(f.r, "PUT", secondPath+"/commands/"+newID(), "bdriver", command)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), testutil.TenantA)
	require.NotContains(t, w.Body.String(), first)
	require.Equal(t, 404, testutil.Do(f.r, "GET", firstPath, "bdriver", "").Code)
}
