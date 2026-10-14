//go:build acceptance

package workforce_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
)

func documentTemporalClient(t *testing.T) client.Client {
	t.Helper()
	address := os.Getenv("HELIVANTA_TEST_TEMPORAL_ADDR")
	require.NotEmpty(t, address, "acceptance requires an explicitly selected local Temporal server")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: "default"})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func documentScanEvent(t *testing.T, s documentScenario) events.Event {
	t.Helper()
	data, err := json.Marshal(workforcecontract.DocumentScanRequestedData{HospitalID: s.input.HospitalID, StaffID: s.input.StaffID, FileID: s.input.FileID, ScanAttempt: s.input.ScanAttempt})
	require.NoError(t, err)
	return events.Event{Type: workforce.DocumentScanRequested, Version: 1, TenantID: s.input.TenantID, Data: data}
}

func TestDocumentJetStreamStartsOneActualTemporalExecution(t *testing.T) {
	s := newDocumentScenario(t)
	c := documentTemporalClient(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	w := worker.New(c, workforce.DocumentTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2})
	w.RegisterWorkflowWithOptions(workforce.DocumentWorkflow, workflow.RegisterOptions{Name: workforce.DocumentWorkflowName})
	w.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: workforce.DocumentActivityName})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	consumer := workforce.DocumentScanConsumer(c)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, s.bus.StartConsumers(ctx, s.db, []events.Consumer{consumer}))
	workflowID, err := workforce.DocumentWorkflowID(s.input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	var status string
	require.NoError(t, c.GetWorkflow(ctx, workflowID, "").Get(ctx, &status))
	require.Equal(t, "available", status)
	evt := documentScanEvent(t, s)
	require.NoError(t, s.db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, workforce.DocumentScanRequested, evt)
	}))
	// Direct duplicate after completion exercises the retained receipt check too.
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	require.Eventually(t, func() bool {
		var count int64
		err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Table("processed_events").Where("consumer=?", consumer.Name).Count(&count).Error
		})
		return err == nil && count == 2
	}, 10*time.Second, 20*time.Millisecond, "distinct JetStream delivery claims share one business execution")
	require.Equal(t, 1, s.cloud.CopyCount())
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"available"`)
	first, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	second, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.Equal(t, first.WorkflowExecutionInfo.Execution.RunId, second.WorkflowExecutionInfo.Execution.RunId)
}

func TestDocumentConsumerRefusesConflictingTemporalReceipt(t *testing.T) {
	s := newDocumentScenario(t)
	c := documentTemporalClient(t)
	workflowID, err := workforce.DocumentWorkflowID(s.input)
	require.NoError(t, err)
	wrong := s.input
	wrong.ScanAttempt++
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: workforce.DocumentTaskQueue}, workforce.DocumentWorkflowName, wrong)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.TerminateWorkflow(cleanup, workflowID, run.GetRunID(), "synthetic acceptance cleanup"))
	})
	consumer := workforce.DocumentScanConsumer(c)
	err = consumer.AfterCommit(ctx, documentScanEvent(t, s))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipt mismatch")
	require.Zero(t, s.cloud.CopyCount())
}
