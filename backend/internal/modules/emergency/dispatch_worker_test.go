package emergency_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
	"time"
)

func dispatchProcessor(t *testing.T, f *fixture) *emergency.DispatchProcessor {
	t.Helper()
	var name string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&name).Error }))
	db, err := tenantdb.OpenRuntime(t.Context(), testinfra.EmergencyWorkerDSN(t, name))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	processor, err := emergency.NewDispatchProcessor(t.Context(), db)
	require.NoError(t, err)
	return processor
}
func TestDedicatedWorkerOffersACommittedReservationExactlyOnce(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	processor := dispatchProcessor(t, f)
	input := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id}
	for range 2 {
		snapshot, err := processor.Offer(t.Context(), input)
		require.NoError(t, err)
		require.Equal(t, "offered", snapshot.State)
		require.Equal(t, int64(2), snapshot.Version)
		require.False(t, snapshot.AcknowledgementDeadline.IsZero())
	}
}

func TestExpiredOfferRequiresReconciliationAndKeepsCapacity(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	processor := dispatchProcessor(t, f)
	input := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id}
	offered, err := processor.Offer(t.Context(), input)
	require.NoError(t, err)
	setDispatchClock(t, f, offered.AcknowledgementDeadline.Add(time.Second))
	snapshot, err := processor.Expire(t.Context(), input, uuid.MustParse(newID()))
	require.NoError(t, err)
	require.Equal(t, "reconciliation_required", snapshot.State)
	require.Equal(t, int64(3), snapshot.Version)
	require.False(t, fleetRows(t, f)[0]["dispatchable"].(bool))
}

func setDispatchClock(t *testing.T, f *fixture, at time.Time) {
	t.Helper()
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_dispatch_clock_" + newID(), SQL: fmt.Sprintf(`CREATE OR REPLACE FUNCTION emergency_dispatch_clock() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT '%s'::timestamptz $$`, at.UTC().Format(time.RFC3339Nano))}}))
}

func TestRevokedClinicalReviewIsNotOfferedOrExposedToAssignedCrew(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false)).Code)
	snapshot, err := dispatchProcessor(t, f).Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	require.Equal(t, "reconciliation_required", snapshot.State)
	require.Nil(t, snapshot.AcknowledgementDeadline)
	w := testutil.Do(f.r, "GET", "/v1/emergency/crew/bases/"+f.base+"/dispatches/"+id, "driver", "")
	require.Equal(t, 404, w.Code, w.Body.String())
}

func TestCrewCannotAcknowledgeAnOfferWhoseClinicalAuthorityWasRevoked(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	processor := dispatchProcessor(t, f)
	_, err := processor.Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false)).Code)
	path := "/v1/emergency/crew/bases/" + f.base + "/dispatches/" + id + "/commands/" + newID()
	w := testutil.Do(f.r, "PUT", path, "driver", `{"version":2,"operation":"acknowledge","evidence_reference":"SYN-CREW-ACK"}`)
	require.Equal(t, 409, w.Code, w.Body.String())
	snapshot, err := processor.Snapshot(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	require.Equal(t, "offered", snapshot.State)
	require.Equal(t, int64(2), snapshot.Version)
}

func TestEligibilityPurposeCannotElevateApplicationOrUnassignedSubjects(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	_, err := dispatchProcessor(t, f).Offer(t.Context(), emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.NoError(t, err)
	for _, subject := range []string{"user-admin", "user-unassigned", "service:emergency-dispatch-worker"} {
		require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec(`SELECT set_config('app.emergency_subject',?,true),set_config('app.emergency_base',?,true),set_config('app.emergency_dispatch',?,true),set_config('app.emergency_eligibility','true',true)`, subject, f.base, id).Error)
			var allowed bool
			require.NoError(t, tx.Raw(`SELECT emergency_dispatch_offer_eligible(?::uuid,?::uuid,?::uuid)`, testutil.TenantA, f.base, id).Scan(&allowed).Error)
			require.False(t, allowed, subject)
			var count int64
			require.NoError(t, tx.Table("emergency_cases").Where("tenant_id=? AND base_id=?", testutil.TenantA, f.base).Count(&count).Error)
			require.Zero(t, count, subject)
			return nil
		}))
	}
}

func TestDispatchWorkerCannotReadPrivatePayloadOrAssumeApplicationRole(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+id, "dispatcher", dispatchBody(c, a, v, d)).Code)
	var database string
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	db, err := tenantdb.OpenRuntime(t.Context(), testinfra.EmergencyWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, statement := range []string{
		`SELECT contact_ciphertext,incident_ciphertext FROM emergency_cases`,
		`SELECT evidence_reference FROM emergency_crew`,
		`SET ROLE hms_app`,
		`SET ROLE hms_owner`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.emergency_subject','service:emergency-dispatch-worker',true),set_config('app.emergency_base',?,true),set_config('app.emergency_dispatch',?,true)`, f.base, id).Error; err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
		require.ErrorContains(t, err, "permission denied", statement)
	}
}
