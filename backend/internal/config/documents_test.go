package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/config"
)

func TestDocumentStorageRequiresCompletePrivatePipelineConfiguration(t *testing.T) {
	valid := config.Config{DocumentQuarantineBucket: "helivanta-test-quarantine", DocumentReleasedBucket: "helivanta-test-released", TemporalAddress: "127.0.0.1:18233", TemporalNamespace: "default"}
	for _, tc := range []struct {
		name             string
		cfg              config.Config
		enabled, invalid bool
	}{
		{"disabled", config.Config{}, false, false},
		{"quarantine_only", config.Config{DocumentQuarantineBucket: valid.DocumentQuarantineBucket}, false, true},
		{"release_only", config.Config{DocumentReleasedBucket: valid.DocumentReleasedBucket}, false, true},
		{"no_workflows", config.Config{DocumentQuarantineBucket: valid.DocumentQuarantineBucket, DocumentReleasedBucket: valid.DocumentReleasedBucket}, false, true},
		{"complete", valid, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, err := tc.cfg.RequireDocumentStorage()
			require.Equal(t, tc.enabled, enabled)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDocumentWorkerRefusesMissingRuntimeIdentityAndScanner(t *testing.T) {
	cfg := config.Config{DocumentQuarantineBucket: "helivanta-test-quarantine", DocumentReleasedBucket: "helivanta-test-released", TemporalAddress: "127.0.0.1:18233", TemporalNamespace: "default"}
	require.Error(t, cfg.RequireDocumentWorker())
	cfg.DocumentWorkerDatabaseURL = "postgres://test-user:never-print-this@localhost/test-db"
	require.Error(t, cfg.RequireDocumentWorker())
	cfg.ClamAVAddress = "127.0.0.1:13310"
	require.NoError(t, cfg.RequireDocumentWorker())
	cfg.DocumentWorkerDatabaseURL = ""
	err := cfg.RequireDocumentWorker()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "never-print-this")
	require.Error(t, (config.Config{}).RequireDocumentWorker())
}

func TestEmergencyWorkflowsDoNotRequirePrivateDocumentBuckets(t *testing.T) {
	cfg := config.Config{EmergencyDispatchEnabled: true, TemporalAddress: "temporal:7233", TemporalNamespace: "helivanta-fixture"}
	enabled, err := cfg.RequireDocumentStorage()
	require.NoError(t, err)
	require.False(t, enabled)
	require.NoError(t, cfg.RequireEmergencyWorkflows())
	cfg.DocumentQuarantineBucket = "one-incomplete-bucket"
	_, err = cfg.RequireDocumentStorage()
	require.Error(t, err)
	cfg.DocumentQuarantineBucket = ""
	cfg.TemporalNamespace = ""
	require.Error(t, cfg.RequireEmergencyWorkflows())
}

func TestEmergencyProcessingCanContinueWithoutAcceptingNewTrips(t *testing.T) {
	t.Setenv("EMERGENCY_DISPATCH_ENABLED", "false")
	t.Setenv("EMERGENCY_WORKFLOWS_ENABLED", "true")
	cfg := config.Load()
	require.False(t, cfg.EmergencyDispatchEnabled)
	require.True(t, cfg.EmergencyWorkflowsEnabled)
	t.Setenv("EMERGENCY_DISPATCH_ENABLED", "true")
	t.Setenv("EMERGENCY_WORKFLOWS_ENABLED", "")
	require.True(t, config.Load().EmergencyWorkflowsEnabled)
	t.Setenv("EMERGENCY_DISPATCH_ENABLED", "false")
	require.False(t, config.Load().EmergencyWorkflowsEnabled)
}
