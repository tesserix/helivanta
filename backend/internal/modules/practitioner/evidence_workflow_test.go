package practitioner_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/gorm"
)

func TestProfessionalEvidenceWorkflowUsesRealScopedGenerationProcessing(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(p.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	calls := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { calls++ })
	env.ExecuteWorkflow(practitioner.EvidenceWorkflow, s.input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var status string
	require.NoError(t, env.GetWorkflowResult(&status))
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.Equal(t, 1, calls)
}
func TestProfessionalEvidenceWorkflowBoundsRetriesAndRetainsSafeVisibleFailure(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(p.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	calls := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { calls++ })
	env.ExecuteWorkflow(practitioner.EvidenceWorkflow, s.input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 5, calls)
	require.Zero(t, s.cloud.CopyCount())
	require.NotContains(t, env.GetWorkflowError().Error(), "stream:")
	require.NotContains(t, env.GetWorkflowError().Error(), "SELECT")
	metadata := testutil.Do(s.router, "GET", s.path, "steward", "")
	require.Equal(t, 200, metadata.Code)
	require.Contains(t, metadata.Body.String(), `"status":"scan_error"`)
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.practitioner.evidence_changed.v1").Count(&n).Error
		require.EqualValues(t, 1, n, "retrying the same visible failure does not duplicate events")
		return err
	}))
}
func TestProfessionalEvidenceWorkflowDoesNotRetryMalformedOrMissingObjects(t *testing.T) {
	for _, malformed := range []bool{true, false} {
		t.Run(map[bool]string{true: "invalid", false: "missing"}[malformed], func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			p := cleanProfessionalEvidenceProcessor(t, s)
			in := s.input
			in.FileID = uuid.Must(uuid.NewV7()).String()
			if malformed {
				in.RevisionID = "not-a-revision"
			}
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(p.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
			calls := 0
			env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { calls++ })
			env.ExecuteWorkflow(practitioner.EvidenceWorkflow, in)
			require.True(t, env.IsWorkflowCompleted())
			require.Error(t, env.GetWorkflowError())
			want := 1
			if malformed {
				want = 0
			}
			require.Equal(t, want, calls)
			require.Zero(t, s.cloud.CopyCount())
			require.NotContains(t, env.GetWorkflowError().Error(), "SELECT")
		})
	}
}

func TestProfessionalEvidenceManualRetryRecoversExhaustedWorkflowWithNewIdentity(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	failed := suite.NewTestWorkflowEnvironment()
	failed.RegisterActivityWithOptions(p.ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	calls := 0
	failed.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { calls++ })
	failed.ExecuteWorkflow(practitioner.EvidenceWorkflow, s.input)
	require.True(t, failed.IsWorkflowCompleted())
	require.Error(t, failed.GetWorkflowError())
	require.Equal(t, 5, calls)
	require.Zero(t, s.cloud.CopyCount())
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	result := testutil.Do(s.router, "PUT", s.path+"/scan-retries/"+uuid.Must(uuid.NewV7()).String(), "steward", body)
	require.Equal(t, 201, result.Code, result.Body.String())
	next := s.input
	next.ScanAttempt = 2
	firstID, err := practitioner.EvidenceWorkflowID(s.input)
	require.NoError(t, err)
	nextID, err := practitioner.EvidenceWorkflowID(next)
	require.NoError(t, err)
	require.NotEqual(t, firstID, nextID)
	recovered := suite.NewTestWorkflowEnvironment()
	recovered.RegisterActivityWithOptions(cleanProfessionalEvidenceProcessor(t, s).ProcessActivity, activity.RegisterOptions{Name: practitioner.EvidenceActivityName})
	recovered.ExecuteWorkflow(practitioner.EvidenceWorkflow, next)
	require.True(t, recovered.IsWorkflowCompleted())
	require.NoError(t, recovered.GetWorkflowError())
	var status string
	require.NoError(t, recovered.GetWorkflowResult(&status))
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
}
