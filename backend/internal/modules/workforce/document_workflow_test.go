package workforce_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/gorm"
)

func TestDocumentWorkflowProcessesRealScopedDocument(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: workforce.DocumentActivityName})
	attempts := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { attempts++ })
	env.ExecuteWorkflow(workforce.DocumentWorkflow, s.input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var status string
	require.NoError(t, env.GetWorkflowResult(&status))
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.Equal(t, 1, attempts)
}

func TestDocumentWorkflowExhaustsBoundedRetriesAndLeavesVisibleFailure(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: workforce.DocumentActivityName})
	attempts := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { attempts++ })
	env.ExecuteWorkflow(workforce.DocumentWorkflow, s.input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "document processing unavailable")
	require.NotContains(t, env.GetWorkflowError().Error(), "stream:")
	require.Zero(t, s.cloud.CopyCount())
	require.Equal(t, 5, attempts)
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"scan_error"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}
