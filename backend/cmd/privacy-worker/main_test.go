package main

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrivacyWorkerRejectsMissingDedicatedIdentityBeforeConnecting(t *testing.T) {
	t.Setenv("PRIVACY_WORKER_DATABASE_URL", "")
	t.Setenv("APP_DATABASE_URL", "postgres://hms_app:synthetic@localhost/fixture")
	err := run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "privacy worker requires")
	require.NotContains(t, err.Error(), "synthetic")
}
