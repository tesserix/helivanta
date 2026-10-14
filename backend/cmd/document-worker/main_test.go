package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentWorkerRefusesBootBeforeNetworkWithoutPrivatePipeline(t *testing.T) {
	for _, key := range []string{"DOCUMENT_QUARANTINE_BUCKET", "DOCUMENT_RELEASED_BUCKET", "DOCUMENT_WORKER_DATABASE_URL", "CLAMAV_ADDRESS", "TEMPORAL_ADDRESS", "TEMPORAL_NAMESPACE"} {
		t.Setenv(key, "")
	}
	err := run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "document worker requires")
}
