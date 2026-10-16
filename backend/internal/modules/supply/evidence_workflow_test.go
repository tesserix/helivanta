package supply_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"                         //nolint:depguard // external test imports its own module
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/gorm"
)

func TestEvidenceWorkflowProcessesRealScopedEvidence(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	input := evidenceScanInput(s)
	worker := evidenceWorker(t, s)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: supply.EvidenceActivityName})
	attempts := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { attempts++ })
	env.ExecuteWorkflow(supply.EvidenceWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var status string
	require.NoError(t, env.GetWorkflowResult(&status))
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.Equal(t, 1, attempts)
}

func TestEvidenceWorkflowExhaustsBoundedRetriesAndLeavesVisibleFailure(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	input := evidenceScanInput(s)
	worker := evidenceWorker(t, s)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: supply.EvidenceActivityName})
	attempts := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { attempts++ })
	env.ExecuteWorkflow(supply.EvidenceWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "evidence processing unavailable")
	require.NotContains(t, env.GetWorkflowError().Error(), "stream:")
	require.Zero(t, s.cloud.CopyCount())
	require.Equal(t, 5, attempts)
	listed := testutil.Do(s.r, "GET", s.path[:len(s.path)-36-1], "owner", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"scan_error"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", supplycontract.SubjectEvidenceChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}
