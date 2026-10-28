package main

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPharmacyWorkerRequiresIndependentCredentialsBeforeNetwork(t *testing.T) {
	for _, key := range []string{"DOCUMENT_QUARANTINE_BUCKET", "DOCUMENT_RELEASED_BUCKET", "PHARMACY_EVIDENCE_WORKER_DATABASE_URL", "CLAMAV_ADDRESS", "TEMPORAL_ADDRESS", "TEMPORAL_NAMESPACE"} {
		t.Setenv(key, "")
	}
	t.Setenv("DOCUMENT_WORKER_DATABASE_URL", "postgres://clinical-worker:TEST-only@localhost/TEST")
	err := run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "pharmacy evidence worker requires")
	require.NotContains(t, err.Error(), "postgres://")
}
