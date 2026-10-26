package practitioner_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestProfessionalEvidenceDispatcherValidatesBeforeClaimAndRequiresTemporal(t *testing.T) {
	consumers := practitioner.New().Consumers(platform.Deps{})
	require.Len(t, consumers, 1)
	c := consumers[0]
	require.Equal(t, "practitioner-evidence-scan", c.Name)
	require.Equal(t, practitionercontract.EvidenceScanRequested, c.Subject)
	data, err := json.Marshal(practitionercontract.EvidenceScanRequestedData{PractitionerID: uuid.Must(uuid.NewV7()).String(), RegistrationID: uuid.Must(uuid.NewV7()).String(), RevisionID: uuid.Must(uuid.NewV7()).String(), FileID: uuid.Must(uuid.NewV7()).String(), ScanAttempt: 1})
	require.NoError(t, err)
	event := events.Event{Type: c.Subject, Version: 1, TenantID: uuid.NewString(), Data: data}
	require.NoError(t, c.Handle(t.Context(), nil, event))
	require.EqualError(t, c.AfterCommit(t.Context(), event), "evidence workflow client unavailable")
	for _, mutate := range []func(*events.Event){
		func(e *events.Event) { e.Version = 2 },
		func(e *events.Event) { e.Type = "other" },
		func(e *events.Event) { e.DestinationTenantID = uuid.NewString() },
		func(e *events.Event) { e.TenantID = "invalid" },
		func(e *events.Event) { e.Data = json.RawMessage(`null`) },
		func(e *events.Event) { e.Data = json.RawMessage(`{"file_id":"invalid"}`) },
		func(e *events.Event) { e.Data = append(append([]byte{}, data...), []byte(` {}`)...) },
		func(e *events.Event) {
			e.Data = append(append([]byte{}, data[:len(data)-1]...), []byte(`,"tenant_id":"forged"}`)...)
		},
	} {
		invalid := event
		mutate(&invalid)
		require.Error(t, c.Handle(t.Context(), nil, invalid))
		require.Error(t, c.AfterCommit(t.Context(), invalid))
	}
}

func TestProfessionalEvidenceExecutionIdentityIncludesEveryScopeComponent(t *testing.T) {
	in := practitionercontract.EvidenceScanInput{TenantID: uuid.NewString(), PractitionerID: uuid.Must(uuid.NewV7()).String(), RegistrationID: uuid.Must(uuid.NewV7()).String(), RevisionID: uuid.Must(uuid.NewV7()).String(), FileID: uuid.Must(uuid.NewV7()).String(), ScanAttempt: 1}
	id, err := practitioner.EvidenceWorkflowID(in)
	require.NoError(t, err)
	attempt := in
	attempt.ScanAttempt = 2
	nextID, err := practitioner.EvidenceWorkflowID(attempt)
	require.NoError(t, err)
	require.NotEqual(t, id, nextID)
	for _, invalid := range []int64{0, -1, 9007199254740992} {
		attempt.ScanAttempt = invalid
		_, err = practitioner.EvidenceWorkflowID(attempt)
		require.Error(t, err)
	}
	seen := map[string]bool{id: true}
	for i := range 5 {
		changed := in
		fields := []*string{&changed.TenantID, &changed.PractitionerID, &changed.RegistrationID, &changed.RevisionID, &changed.FileID}
		*fields[i] = uuid.Must(uuid.NewV7()).String()
		different, err := practitioner.EvidenceWorkflowID(changed)
		require.NoError(t, err)
		require.False(t, seen[different])
		seen[different] = true
		*fields[i] = "invalid"
		_, err = practitioner.EvidenceWorkflowID(changed)
		require.Error(t, err)
	}
}
