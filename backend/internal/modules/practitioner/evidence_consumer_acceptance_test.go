//go:build acceptance

package practitioner_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"gorm.io/gorm"
)

func professionalTemporalClient(t *testing.T) client.Client {
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

func professionalCommittedScanEvent(t *testing.T, s professionalEvidenceScenario) events.Event {
	t.Helper()
	var rows []struct{ Payload json.RawMessage }
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Select("payload").Where("subject=?", practitionercontract.EvidenceScanRequested).Find(&rows).Error
	}))
	require.Len(t, rows, 1)
	var evt events.Event
	require.NoError(t, json.Unmarshal(rows[0].Payload, &evt))
	return evt
}

func TestProfessionalCommittedScanStartsOneRealTemporalExecution(t *testing.T) {
	c := professionalTemporalClient(t)
	s := newProfessionalEvidenceScenarioWithRuntime(t, nil, c)
	p := cleanProfessionalEvidenceProcessor(t, s)
	w := worker.New(c, practitioner.EvidenceTaskQueue, worker.Options{MaxConcurrentActivityExecutionSize: 2})
	w.RegisterWorkflowWithOptions(practitioner.EvidenceWorkflow, workflow.RegisterOptions{Name: practitioner.EvidenceWorkflowName})
	w.RegisterActivityWithOptions(p.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	consumer := practitioner.New().Consumers(platform.Deps{Workflows: c})[0]
	evt := professionalCommittedScanEvent(t, s)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, consumer.Handle(ctx, nil, evt))
	id, err := practitioner.EvidenceWorkflowID(s.input)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := c.DescribeWorkflowExecution(ctx, id, "")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "committed outbox must dispatch through JetStream")
	var status string
	run := c.GetWorkflow(ctx, id, "")
	require.NoError(t, run.Get(ctx, &status))
	require.Equal(t, "available", status)
	evt.ID = uuid.NewString()
	require.NoError(t, s.db.WithTenant(ctx, s.input.TenantID, func(tx *gorm.DB) error { return events.Enqueue(tx, practitionercontract.EvidenceScanRequested, evt) }))
	require.Eventually(t, func() bool {
		var count int64
		err := s.db.WithTenant(ctx, s.input.TenantID, func(tx *gorm.DB) error {
			return tx.Table("processed_events").Where("consumer=?", consumer.Name).Count(&count).Error
		})
		return err == nil && count == 2
	}, 10*time.Second, 20*time.Millisecond, "duplicate delivery claims share one execution")
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	require.NoError(t, consumer.AfterCommit(ctx, evt))
	description, err := c.DescribeWorkflowExecution(ctx, id, "")
	require.NoError(t, err)
	require.Equal(t, run.GetRunID(), description.WorkflowExecutionInfo.Execution.RunId)
	require.Equal(t, 1, s.cloud.CopyCount())
	content := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
	require.Equal(t, 200, content.Code, content.Body.String())
	require.Equal(t, s.content, content.Body.Bytes())
}

func TestProfessionalDispatcherRejectsForeignTemporalReceipt(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	c := professionalTemporalClient(t)
	id, err := practitioner.EvidenceWorkflowID(s.input)
	require.NoError(t, err)
	wrong := s.input
	wrong.RevisionID = uuid.Must(uuid.NewV7()).String()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: practitioner.EvidenceTaskQueue}, practitioner.EvidenceWorkflowName, wrong)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.TerminateWorkflow(cleanup, id, run.GetRunID(), "synthetic acceptance cleanup"))
	})
	consumer := practitioner.New().Consumers(platform.Deps{Workflows: c})[0]
	require.EqualError(t, consumer.AfterCommit(ctx, professionalCommittedScanEvent(t, s)), "evidence workflow receipt mismatch")
	require.Zero(t, s.cloud.CopyCount())
}
