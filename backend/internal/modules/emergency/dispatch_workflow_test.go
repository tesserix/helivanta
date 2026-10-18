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
