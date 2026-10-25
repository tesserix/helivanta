package config_test

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/config"
	"testing"
)

func TestPrivacyWorkerRequiresDedicatedIdentityAndVerifiedProductionTLS(t *testing.T) {
	good := config.Config{Env: "production", PrivacyWorkerDatabaseURL: "postgres://helivanta_privacy_worker:synthetic@localhost/fixture?sslmode=verify-full&sslrootcert=/fixture/ca.crt", TemporalAddress: "localhost:7233", TemporalNamespace: "privacy-fixture"}
	require.NoError(t, good.RequirePrivacyWorker())
	for _, change := range []func(*config.Config){
		func(c *config.Config) {
			c.PrivacyWorkerDatabaseURL = ""
			c.AppDatabaseURL = "postgres://hms_app:synthetic@localhost/fixture"
		},
		func(c *config.Config) {
			c.PrivacyWorkerDatabaseURL = "postgres://hms_app:synthetic@localhost/fixture?sslmode=verify-full&sslrootcert=/fixture/ca.crt"
		},
		func(c *config.Config) {
			c.PrivacyWorkerDatabaseURL = "postgres://helivanta_privacy_worker:synthetic@localhost/fixture?sslmode=require"
		},
		func(c *config.Config) { c.TemporalAddress = "" }, func(c *config.Config) { c.TemporalNamespace = "" },
	} {
		c := good
		change(&c)
		require.Error(t, c.RequirePrivacyWorker())
	}
}
func TestPrivacyDeadlinesConfigurationIsIndependentOfEmergencyAndDocuments(t *testing.T) {
	t.Setenv("PRIVACY_DEADLINES_ENABLED", "true")
	t.Setenv("PRIVACY_WORKER_DATABASE_URL", "postgres://helivanta_privacy_worker:synthetic@localhost/fixture")
	t.Setenv("TEMPORAL_ADDRESS", "localhost:7233")
	t.Setenv("TEMPORAL_NAMESPACE", "privacy-fixture")
	c := config.Load()
	require.True(t, c.PrivacyDeadlinesEnabled)
	require.NotEmpty(t, c.PrivacyWorkerDatabaseURL)
	require.NoError(t, c.RequirePrivacyDeadlines())
	c.TemporalAddress = ""
	require.Error(t, c.RequirePrivacyDeadlines())
	t.Setenv("PRIVACY_DEADLINES_ENABLED", "")
	require.False(t, config.Load().PrivacyDeadlinesEnabled)
}
