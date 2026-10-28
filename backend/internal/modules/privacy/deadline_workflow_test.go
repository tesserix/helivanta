package privacy_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"testing"
	"time"
)

func TestCaseDeadlineWorkflowWaitsDurablyAndNeverResolvesTheCase(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	in := privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: uuid.Must(uuid.NewV7()).String()}
	snapshot := privacy.DeadlineSnapshot{Version: 1, ObservedAt: start, AcknowledgeBy: start.Add(time.Hour), ResolveBy: start.Add(2 * time.Hour)}
	calls := []string{}
	env.RegisterActivityWithOptions(func(context.Context, privacycontract.DeadlineWorkflowInput) (privacy.DeadlineSnapshot, error) {
		return snapshot, nil
	}, activity.RegisterOptions{Name: privacy.DeadlineRefreshActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, expired privacy.DeadlineExpiryInput) (privacy.DeadlineSnapshot, error) {
		calls = append(calls, expired.Kind)
		if expired.Kind == "acknowledgement" {
			snapshot.AcknowledgementEscalated = true
		} else {
			snapshot.ResolutionEscalated = true
		}
		snapshot.ObservedAt = env.Now()
		snapshot.Version++
		return snapshot, nil
	}, activity.RegisterOptions{Name: privacy.DeadlineExpireActivity})
	env.ExecuteWorkflow(privacy.DeadlineWorkflow, in)
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"acknowledgement", "resolution"}, calls)
	require.False(t, snapshot.Terminal, "escalation cannot resolve patient casework")
	require.Equal(t, start.Add(2*time.Hour), env.Now().UTC())
}

func TestDeadlineWorkflowRecoversDatabaseOutageBeyondTwoMinutes(t *testing.T) {
	r, db, _, _ := deadlineFixture(t, true)
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	setDeadlineClock(t, db, start)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"Synthetic request"}`).Code)
	processor := deadlineProcessor(t, db)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_privacy_clock_outage", SQL: `CREATE OR REPLACE FUNCTION privacy_deadline_clock() RETURNS timestamptz LANGUAGE plpgsql VOLATILE AS $$ BEGIN RAISE EXCEPTION 'synthetic database outage'; END $$`}}))
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(start)
	env.SetTestTimeout(30 * time.Second)
	env.RegisterActivityWithOptions(processor.Refresh, activity.RegisterOptions{Name: privacy.DeadlineRefreshActivity})
	env.RegisterActivityWithOptions(func(ctx context.Context, in privacy.DeadlineExpiryInput) (privacy.DeadlineSnapshot, error) {
		setDeadlineClock(t, db, env.Now())
		return processor.Expire(ctx, in)
	}, activity.RegisterOptions{Name: privacy.DeadlineExpireActivity})
	env.RegisterDelayedCallback(func() { setDeadlineClock(t, db, env.Now()) }, 3*time.Minute)
	env.ExecuteWorkflow(privacy.DeadlineWorkflow, privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: id})
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "escalated", result)
	require.True(t, env.Now().Equal(start.Add(2*time.Hour)))
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"received"`)
}
