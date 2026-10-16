//go:build acceptance

package supply_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // external test imports its own module
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract"
	"github.com/tesserix/helivanta/internal/platform"
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

func evidenceTemporalClient(t *testing.T) client.Client {
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

func evidenceScanEvent(t *testing.T, s evidenceScenario) events.Event {
	t.Helper()
	data, err := json.Marshal(supplycontract.EvidenceScanRequestedData{PartyID: evidenceScanInput(s).PartyID, LicenseID: evidenceScanInput(s).LicenseID, FileID: evidenceScanInput(s).FileID, ScanAttempt: evidenceScanInput(s).ScanAttempt})
	require.NoError(t, err)
	return events.Event{Type: supplycontract.SubjectEvidenceScanRequested, Version: 1, TenantID: evidenceScanInput(s).TenantID, Data: data}
}

func TestEvidenceJetStreamStartsOneActualTemporalExecution(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	c := evidenceTemporalClient(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), evidenceWorker(t, s), s.files, scanner)
	require.NoError(t, err)
	w := worker.New(c, supply.EvidenceTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2})
	w.RegisterWorkflowWithOptions(supply.EvidenceWorkflow, workflow.RegisterOptions{Name: supply.EvidenceWorkflowName})
	w.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: supply.EvidenceActivityName})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	consumers := supply.New().Consumers(platform.Deps{Workflows: c})
	require.Len(t, consumers, 1, "production module must register the durable evidence dispatcher")
	consumer := consumers[0]
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, s.bus.StartConsumers(ctx, s.db, []events.Consumer{consumer}))
	workflowID, err := supply.EvidenceWorkflowID(evidenceScanInput(s))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	var status string
	require.NoError(t, c.GetWorkflow(ctx, workflowID, "").Get(ctx, &status))
	require.Equal(t, "available", status)
	evt := evidenceScanEvent(t, s)
	require.NoError(t, s.db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, supplycontract.SubjectEvidenceScanRequested, evt)
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
	listed := testutil.Do(s.r, "GET", s.path[:len(s.path)-36-1], "owner", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"available"`)
	first, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	second, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.Equal(t, first.WorkflowExecutionInfo.Execution.RunId, second.WorkflowExecutionInfo.Execution.RunId)
}

func TestEvidenceConsumerRefusesConflictingTemporalReceipt(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	c := evidenceTemporalClient(t)
	workflowID, err := supply.EvidenceWorkflowID(evidenceScanInput(s))
	require.NoError(t, err)
	wrong := evidenceScanInput(s)
	wrong.ScanAttempt++
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: supply.EvidenceTaskQueue}, supply.EvidenceWorkflowName, wrong)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.TerminateWorkflow(cleanup, workflowID, run.GetRunID(), "synthetic acceptance cleanup"))
	})
	consumer := supply.EvidenceScanConsumer(c)
	err = consumer.AfterCommit(ctx, evidenceScanEvent(t, s))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipt mismatch")
	require.Zero(t, s.cloud.CopyCount())
}
