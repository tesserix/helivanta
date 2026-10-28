package pharmacy_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"                           //nolint:depguard // external test importing its own module
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func runImageWorkflow(t *testing.T, s imageScenario, reply string) (*testsuite.TestWorkflowEnvironment, int) {
	t.Helper()
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewPrescriptionImageProcessor(t.Context(), s.worker(t), s.files, scanner)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(processor.ProcessActivity, activity.RegisterOptions{Name: pharmacy.PrescriptionImageActivityName})
	attempts := 0
	env.SetOnActivityStartedListener(func(_ *activity.Info, _ context.Context, _ converter.EncodedValues) { attempts++ })
	env.ExecuteWorkflow(pharmacy.PrescriptionImageWorkflow, s.input())
	require.True(t, env.IsWorkflowCompleted())
	return env, attempts
}

func TestThePrescriptionImageWorkflowReleasesAScopedPage(t *testing.T) {
	s := newImageScenario(t)
	env, attempts := runImageWorkflow(t, s, "stream: OK\x00")
	require.NoError(t, env.GetWorkflowError())
	var status string
	require.NoError(t, env.GetWorkflowResult(&status))
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.Equal(t, 1, attempts)
}

func TestThePrescriptionImageWorkflowStopsAfterBoundedRetriesWithAVisibleFailure(t *testing.T) {
	s := newImageScenario(t)
	env, attempts := runImageWorkflow(t, s, "stream: ERROR\x00")
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "evidence processing unavailable")
	require.NotContains(t, env.GetWorkflowError().Error(), "stream:", "scanner replies never enter workflow history")
	require.Zero(t, s.cloud.CopyCount())
	require.Equal(t, 5, attempts)
	listed := do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "pharmacist", "")
	require.Contains(t, listed.Body.String(), `"status":"scan_error"`)
	require.Len(t, s.outbox(t, pharmacycontract.SubjectPrescriptionImageChanged), 1)
}

func TestThePrescriptionImageWorkflowRefusesMalformedInputWithoutRetrying(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(pharmacy.PrescriptionImageWorkflow, pharmacycontract.PrescriptionImageScanInput{TenantID: "not-a-uuid", ScanAttempt: 1})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	id, err := pharmacy.PrescriptionImageWorkflowID(pharmacycontract.PrescriptionImageScanInput{TenantID: "not-a-uuid", ScanAttempt: 1})
	require.Error(t, err)
	require.Empty(t, id)
}
