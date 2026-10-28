//go:build acceptance

package pharmacy_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"                           //nolint:depguard // external test importing its own module
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
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

func imageTemporalClient(t *testing.T) client.Client {
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

func imageScanEvent(t *testing.T, s imageScenario) events.Event {
	t.Helper()
	data, err := json.Marshal(pharmacycontract.PrescriptionImageScanRequestedData{OutletID: s.input().OutletID, PrescriptionID: s.input().PrescriptionID, FileID: s.input().FileID, ScanAttempt: s.input().ScanAttempt})
	require.NoError(t, err)
	return events.Event{Type: pharmacycontract.SubjectPrescriptionImageScanRequested, Version: 1, TenantID: s.input().TenantID, Data: data}
}

func TestPrescriptionImageJetStreamStartsOneActualTemporalExecution(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, 201, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, 202, s.upload("pharmacist", "image/png", s.data).Code)
	c := imageTemporalClient(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewPrescriptionImageProcessor(t.Context(), s.worker(t), s.files, scanner)
	require.NoError(t, err)
	w := worker.New(c, pharmacy.PrescriptionImageTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2})
	w.RegisterWorkflowWithOptions(pharmacy.PrescriptionImageWorkflow, workflow.RegisterOptions{Name: pharmacy.PrescriptionImageWorkflowName})
	w.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: pharmacy.PrescriptionImageActivityName})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	var consumer events.Consumer
	for _, registered := range pharmacy.New().Consumers(platform.Deps{Workflows: c}) {
		if registered.Subject == pharmacycontract.SubjectPrescriptionImageScanRequested {
			consumer = registered
		}
	}
	require.Equal(t, "pharmacy-prescription-image-scan", consumer.Name, "the production module registers the durable scan dispatcher")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, s.bus.StartConsumers(ctx, s.db, []events.Consumer{consumer}))
	workflowID, err := pharmacy.PrescriptionImageWorkflowID(s.input())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	var status string
	require.NoError(t, c.GetWorkflow(ctx, workflowID, "").Get(ctx, &status))
	require.Equal(t, "available", status)
	evt := imageScanEvent(t, s)
	require.NoError(t, s.db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, pharmacycontract.SubjectPrescriptionImageScanRequested, evt)
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
	listed := do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "pharmacist", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"available"`)
	first, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	second, err := c.DescribeWorkflowExecution(ctx, workflowID, "")
	require.NoError(t, err)
	require.Equal(t, first.WorkflowExecutionInfo.Execution.RunId, second.WorkflowExecutionInfo.Execution.RunId)
}

func TestPrescriptionImageConsumerRefusesAConflictingTemporalReceipt(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, 201, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, 202, s.upload("pharmacist", "image/png", s.data).Code)
	c := imageTemporalClient(t)
	workflowID, err := pharmacy.PrescriptionImageWorkflowID(s.input())
	require.NoError(t, err)
	wrong := s.input()
	wrong.ScanAttempt++
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: pharmacy.PrescriptionImageTaskQueue}, pharmacy.PrescriptionImageWorkflowName, wrong)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.TerminateWorkflow(cleanup, workflowID, run.GetRunID(), "synthetic acceptance cleanup"))
	})
	consumer := pharmacy.PrescriptionImageScanConsumer(c)
	err = consumer.AfterCommit(ctx, imageScanEvent(t, s))
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipt mismatch")
	require.Zero(t, s.cloud.CopyCount())
}
