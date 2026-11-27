package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
)

func TestManualPersonSourceDefaultsToDocumentOnlyValidation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	in := iam.ManualIdentitySourceImport{TenantID: uuid.New(), SourceProfileID: uuid.Must(uuid.NewV7()), PolicyReference: uuid.Must(uuid.NewV7()), ReviewReference: uuid.Must(uuid.NewV7()), WitnessReference: uuid.Must(uuid.NewV7()), DisplayName: "Synthetic source", ObservedAt: now.Add(-time.Hour), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), IndependentRootReviewConfirmed: true, Reason: "PRIVATE-SYNTHETIC-FINDING"}
	for _, subject := range []string{"synthetic-examiner", "synthetic-reviewer"} {
		in.Roots = append(in.Roots, iam.ManualIdentityRootPerson{PersonID: uuid.Must(uuid.NewV7()), SourcePersonReference: uuid.Must(uuid.NewV7()), FindingReference: uuid.Must(uuid.NewV7()), Accounts: []iam.ManualIdentityRootAccount{{Subject: subject, ExpectedMembershipEpoch: 1}}})
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "source.json")
	require.NoError(t, os.WriteFile(file, raw, 0600))
	t.Setenv("APP_DATABASE_URL", "invalid")
	t.Setenv("ADMIN_DATABASE_URL", "invalid")
	var output bytes.Buffer
	require.NoError(t, run([]string{"--file", file}, &output))
	require.JSONEq(t, `{"status":"valid_document_only","membership_checked":false,"clinical_authority":"not_evaluated"}`, output.String())
	require.NotContains(t, output.String(), in.Reason)
	require.NotContains(t, output.String(), in.Roots[0].Accounts[0].Subject)
}

func TestManualPersonSourceRejectsUnsafeOrAmbiguousArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--file"}, {"--unknown"}, {"--file", "missing", "extra"}} {
		var output bytes.Buffer
		require.Error(t, run(args, &output))
		require.Empty(t, output.String())
	}
	file := filepath.Join(t.TempDir(), "invalid.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"subject":"PRIVATE-IDENTITY"}`), 0600))
	var output bytes.Buffer
	err := run([]string{"--file", file}, &output)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PRIVATE-IDENTITY")
	require.Empty(t, output.String())
}
