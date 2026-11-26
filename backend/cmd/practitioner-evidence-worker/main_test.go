package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func professionalWorkerEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DOCUMENT_QUARANTINE_BUCKET", "qa-professional-quarantine")
	t.Setenv("DOCUMENT_RELEASED_BUCKET", "qa-professional-released")
	t.Setenv("CLAMAV_ADDRESS", "scanner.example.invalid:3310")
	t.Setenv("TEMPORAL_ADDRESS", "temporal.example.invalid:7233")
	t.Setenv("TEMPORAL_NAMESPACE", "qa-professional")
	t.Setenv("DOCUMENT_WORKER_DATABASE_URL", "postgres://other-worker:TEST-only@example.invalid/TEST")
	t.Setenv("PHARMACY_EVIDENCE_WORKER_DATABASE_URL", "postgres://pharmacy-worker:TEST-only@example.invalid/TEST")
	t.Setenv("SUPPLY_EVIDENCE_WORKER_DATABASE_URL", "postgres://supply-worker:TEST-only@example.invalid/TEST")
}

func TestProfessionalWorkerRejectsOtherWorkerCredentialsBeforeNetwork(t *testing.T) {
	professionalWorkerEnvironment(t)
	t.Setenv("PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL", "")
	err := run()
	require.ErrorContains(t, err, "professional evidence worker requires independent")
	require.NotContains(t, err.Error(), "postgres://")
	require.NotContains(t, err.Error(), "TEST-only")
}

func TestProfessionalWorkerRejectsBlankCredentials(t *testing.T) {
	professionalWorkerEnvironment(t)
	t.Setenv("PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL", " \t\n")
	_, err := loadWorkerConfig()
	require.ErrorContains(t, err, "professional evidence worker requires independent")
}

func TestProfessionalWorkerUsesOnlyItsConfiguredCredential(t *testing.T) {
	professionalWorkerEnvironment(t)
	const own = "postgres://helivanta_practitioner_evidence_worker:TEST%20only@example.invalid/TEST?sslmode=verify-full"
	t.Setenv("PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL", own)
	cfg, err := loadWorkerConfig()
	require.NoError(t, err)
	require.Equal(t, own, cfg.DocumentWorkerDatabaseURL)
	require.Equal(t, "qa-professional-quarantine", cfg.DocumentQuarantineBucket)
	require.Equal(t, "qa-professional-released", cfg.DocumentReleasedBucket)
	require.Equal(t, "qa-professional", cfg.TemporalNamespace)
}

func TestProfessionalWorkerRejectsIncompletePrivatePipeline(t *testing.T) {
	for _, key := range []string{"DOCUMENT_QUARANTINE_BUCKET", "DOCUMENT_RELEASED_BUCKET", "CLAMAV_ADDRESS", "TEMPORAL_ADDRESS", "TEMPORAL_NAMESPACE"} {
		t.Run(key, func(t *testing.T) {
			professionalWorkerEnvironment(t)
			t.Setenv("PRACTITIONER_EVIDENCE_WORKER_DATABASE_URL", "postgres://own:TEST-only@example.invalid/TEST")
			t.Setenv(key, "")
			_, err := loadWorkerConfig()
			require.ErrorContains(t, err, "professional evidence worker requires independent")
		})
	}
}
