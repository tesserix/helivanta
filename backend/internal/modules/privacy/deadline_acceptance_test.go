//go:build acceptance

package privacy_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"os"
	"testing"
	"time"
)

func TestDeadlineJetStreamDeliverySurvivesWorkerRestartAndTerminalReplay(t *testing.T) {
	address := os.Getenv("HELIVANTA_TEST_TEMPORAL_ADDR")
	require.NotEmpty(t, address, "acceptance requires an explicitly selected local Temporal server")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	temporalClient, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: "default"})
	require.NoError(t, err)
	t.Cleanup(temporalClient.Close)
	r, db, _, _ := deadlineFixtureWithClient(t, true, temporalClient)
	processor := deadlineProcessor(t, db)
	startWorker := func() worker.Worker {
		w := worker.New(temporalClient, privacy.DeadlineTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2, WorkerStopTimeout: 5 * time.Second})
		w.RegisterWorkflowWithOptions(privacy.DeadlineWorkflow, workflow.RegisterOptions{Name: privacy.DeadlineWorkflowName})
		w.RegisterActivityWithOptions(processor.Refresh, activity.RegisterOptions{Name: privacy.DeadlineRefreshActivity})
		w.RegisterActivityWithOptions(processor.Expire, activity.RegisterOptions{Name: privacy.DeadlineExpireActivity})
		require.NoError(t, w.Start())
		return w
	}
	firstWorker := startWorker()
	firstStopped := false
	t.Cleanup(func() {
		if !firstStopped {
			firstWorker.Stop()
		}
	})
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/privacy/requests/" + id
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "patient", `{"kind":"access","description":"private synthetic case"}`).Code)
	input := privacycontract.DeadlineWorkflowInput{TenantID: testutil.TenantA, RequestID: id}
	workflowID, err := privacy.DeadlineWorkflowID(input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		result := testutil.Do(r, "GET", path+"/deadline", "patient", "")
		var body struct{ Monitoring string }
		return result.Code == 200 && json.Unmarshal(result.Body.Bytes(), &body) == nil && body.Monitoring == "observed"
	}, 15*time.Second, 20*time.Millisecond)
	before, err := temporalClient.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	firstWorker.Stop()
	firstStopped = true
	require.Equal(t, 201, testutil.Do(r, "PUT", path+"/commands/"+uuid.Must(uuid.NewV7()).String(), "patient", `{"expected_version":1,"action":"withdraw","message":"Synthetic withdrawal"}`).Code)
	secondWorker := startWorker()
	t.Cleanup(secondWorker.Stop)
	var result string
	require.NoError(t, temporalClient.GetWorkflow(ctx, workflowID, "").Get(ctx, &result))
	require.Equal(t, "terminal", result)
	payload, err := json.Marshal(privacycontract.DeadlineChangedData{RequestID: id, Version: 2})
	require.NoError(t, err)
	evt := events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: privacycontract.SubjectDeadlineChanged, Version: 1, TenantID: testutil.TenantA, Data: payload}
	require.NoError(t, privacy.DeadlineConsumer(temporalClient).AfterCommit(ctx, evt))
	after, err := temporalClient.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.Equal(t, before.GetWorkflowExecutionInfo().GetExecution().GetRunId(), after.GetWorkflowExecutionInfo().GetExecution().GetRunId())
	require.Contains(t, testutil.Do(r, "GET", path, "patient", "").Body.String(), `"status":"withdrawn"`)
}
