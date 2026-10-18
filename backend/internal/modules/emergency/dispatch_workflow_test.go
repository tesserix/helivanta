package emergency_test

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/emergency" //nolint:depguard // module under test
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"testing"
	"time"
)

func TestDispatchWorkflowDurablyExpiresAnOfferAndWaitsForRealReconciliation(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	c, a, v, d := prepareDispatch(t, f)
	id := newID()
	path := f.path() + "/dispatches/" + id
	require.Equal(t, 202, testutil.Do(f.r, "PUT", path, "dispatcher", dispatchBody(c, a, v, d)).Code)
	processor := dispatchProcessor(t, f)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Now().UTC()
	env.SetStartTime(start)
	setDispatchClock(t, f, start)
	env.RegisterActivityWithOptions(processor.OfferActivity, activity.RegisterOptions{Name: emergency.DispatchOfferActivity})
	env.RegisterActivityWithOptions(processor.RefreshActivity, activity.RegisterOptions{Name: emergency.DispatchRefreshActivity})
	env.RegisterActivityWithOptions(func(ctx context.Context, in emergency.DispatchExpiryInput) (emergency.DispatchSnapshot, error) {
		setDispatchClock(t, f, env.Now())
		return processor.ExpireActivity(ctx, in)
	}, activity.RegisterOptions{Name: emergency.DispatchExpireActivity})
	env.RegisterDelayedCallback(func() {
		w := testutil.Do(f.r, "GET", path, "dispatcher", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "reconciliation_required", decodeBody(t, w.Body.Bytes())["state"])
		w = testutil.Do(f.r, "PUT", path+"/commands/"+newID(), "supervisor", `{"version":3,"operation":"reconcile_cancelled","evidence_reference":"SYN-VOICE-CONTACT-CONFIRMED"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		env.SignalWorkflow(emergency.DispatchVersionSignal, int64(4))
	}, 3*time.Minute)
	env.ExecuteWorkflow(emergency.DispatchWorkflow, emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: id})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var state string
	require.NoError(t, env.GetWorkflowResult(&state))
	require.Equal(t, "cancelled", state)
	require.True(t, fleetRows(t, f)[0]["dispatchable"].(bool))
}

func TestDispatchWorkflowDurablyEscalatesReceivingExpiryWithoutLosingCapacity(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	a := prepareAcceptedReceivingTrip(t, f)
	processor := dispatchProcessor(t, f)
	in := emergencycontract.DispatchWorkflowInput{TenantID: testutil.TenantA, BaseID: f.base, DispatchID: a.dispatch}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetTestTimeout(10 * time.Second)
	env.SetStartTime(time.Now().UTC())
	setDispatchClock(t, f, env.Now())
	env.RegisterActivityWithOptions(processor.OfferActivity, activity.RegisterOptions{Name: emergency.DispatchOfferActivity})
	env.RegisterActivityWithOptions(processor.RefreshActivity, activity.RegisterOptions{Name: emergency.DispatchRefreshActivity})
	env.RegisterActivityWithOptions(func(ctx context.Context, input emergency.DispatchReceivingExpiryInput) (emergency.DispatchSnapshot, error) {
		setDispatchClock(t, f, env.Now())
		return processor.ExpireReceiving(ctx, input)
	}, activity.RegisterOptions{Name: "emergency-dispatch-receiving-expire-v1"})
	env.RegisterDelayedCallback(func() {
		setDispatchClock(t, f, env.Now())
		current, err := processor.Snapshot(t.Context(), in)
		require.NoError(t, err)
		if !current.ReceivingTimedOut {
			t.Error("receiving deadline did not produce its durable escalation")
		}
		require.Equal(t, "on_scene", current.State)
		require.Equal(t, int64(6), current.Version)
		w := testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+a.dispatch+"/commands/"+newID(), "dispatcher", `{"version":6,"operation":"request_cancel","evidence_reference":"SYN-VOICE-STANDDOWN"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/dispatches/"+a.dispatch+"/commands/"+newID(), "supervisor", `{"version":7,"operation":"reconcile_cancelled","evidence_reference":"SYN-CONFIRMED-STANDDOWN"}`).Code)
		env.SignalWorkflow(emergency.DispatchVersionSignal, int64(8))
	}, 31*time.Minute)
	env.ExecuteWorkflow(emergency.DispatchWorkflow, in)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "cancelled", result)
}
